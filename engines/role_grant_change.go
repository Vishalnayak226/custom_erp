package engines

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Stage 49.3.6 - privilege-change safety for a role's record-type grants.
//
// Before this, a save on the Roles screen upserted role_permissions and wrote
// one "Set X permissions on Y" audit line per record type: no record of what
// the role held before, no way back, and nothing that set a payroll or
// ledger grant apart from a colour-scheme one. Now every save goes through
// ChangeRoleGrants, which
//
//   - previews the change (PreviewRoleGrantChange - the same answer, nothing
//     written): what widens, what narrows, which dangerous grants
//     (privilege_risk.go) the role gains or loses, the SoD conflicts after it
//     and how many people hold the role;
//   - records the role's exact grants before and after, with who and why, in
//     role_grant_changes, and that row's SHA-256 in the signed audit chain -
//     so the evidence cannot be edited without the audit verifier noticing
//     (VerifyRoleGrantChangeEvidence);
//   - ends the sessions of everyone holding the role when the change takes
//     access to a dangerous record type away (payroll, posting, account
//     takeover, connector secrets) - what such a person already has on screen
//     must not outlive the decision. Every other change already applies at
//     the user's next request: permissions are read live (checkPermission).
//
// RollbackRoleGrantChange undoes one change without ever restoring obsolete
// access: a record type changed again since keeps the later decision, a
// record type that no longer exists is skipped, and a rollback never hands a
// dangerous grant back - that needs a fresh change with its own reason.
// RevertRoleTemplateMigration (47.1.7) follows the same rules.

// RoleGrantInput is one record type's four rights, as a caller asks for them.
type RoleGrantInput struct {
	Doctype string `json:"doctype_name"`
	Read    bool   `json:"allow_read"`
	Create  bool   `json:"allow_create"`
	Update  bool   `json:"allow_update"`
	Delete  bool   `json:"allow_delete"`
}

// RoleGrantChangeRequest is one save of a role's grants.
type RoleGrantChangeRequest struct {
	Role   string
	Grants []RoleGrantInput
	// Actor is the username recorded as having made the change; ActorUserID
	// keeps the administrator's own session alive when sessions are ended.
	Actor       string
	ActorUserID string
	Reason      string
	// EndSessions ends the role's sessions even when the rule above would not.
	EndSessions bool
}

// RoleGrantChangePreview is what a change does, computed without saving it.
type RoleGrantChangePreview struct {
	Role string `json:"role"`
	// Changed lists every record type whose rights differ, before -> after.
	Changed []RoleGrantFlagChange `json:"changed"`
	// Widened / Reduced name the record types where any right was added /
	// taken away (one change can be both: read off, delete on).
	Widened          []string         `json:"widened"`
	Reduced          []string         `json:"reduced"`
	DangerousGained  []DangerousGrant `json:"dangerous_gained"`
	DangerousRemoved []DangerousGrant `json:"dangerous_removed"`
	// HighRisk is true when the change gives or takes away a dangerous grant.
	HighRisk        bool         `json:"high_risk"`
	Conflicts       []SoDFinding `json:"sod_conflicts_after"`
	UsersWithRole   int          `json:"users_with_role"`
	SessionsWillEnd bool         `json:"sessions_will_end"`
}

// RoleGrantFlagChange is one record type's rights before and after.
type RoleGrantFlagChange struct {
	Doctype string `json:"doctype"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// RoleGrantChangeResult is a saved change.
type RoleGrantChangeResult struct {
	// ChangeID is "" when the save changed nothing (no evidence row for a no-op).
	ChangeID      string                 `json:"change_id,omitempty"`
	Preview       RoleGrantChangePreview `json:"preview"`
	SessionsEnded int                    `json:"sessions_ended"`
}

// RollbackOutcome says what a rollback put back and what it deliberately did not.
type RollbackOutcome struct {
	Restored []RoleGrantFlagChange `json:"restored"`
	// Superseded record types were changed again after the change being
	// undone; the later decision stands.
	Superseded []string `json:"superseded"`
	// Withheld are dangerous grants the rollback would have handed back.
	Withheld []DangerousGrant `json:"withheld"`
	// Gone are record types that no longer exist.
	Gone []string `json:"gone"`
}

// ErrRoleChangeReasonRequired: a rollback is always explained.
var errRoleChangeReasonRequired = &ValidationError{Code: "GLOBAL-0002", Message: "write a reason (at least 5 characters) - it goes into the audit log"}

func grantKey(role, doctype string) string { return role + "\x00" + doctype }

func rowLevel(r storedRolePermissionRow) AccessLevel {
	return levelFor(r.Read, r.Create, r.Update, r.Delete)
}

func rowRights(r storedRolePermissionRow) string {
	var parts []string
	for _, p := range []struct {
		on   bool
		name string
	}{{r.Read, "read"}, {r.Create, "create"}, {r.Update, "update"}, {r.Delete, "delete"}} {
		if p.on {
			parts = append(parts, p.name)
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "+")
}

func sameRights(a, b storedRolePermissionRow) bool {
	return a.Read == b.Read && a.Create == b.Create && a.Update == b.Update && a.Delete == b.Delete
}

// widens / narrows: any right switched on / off from a to b.
func widens(a, b storedRolePermissionRow) bool {
	return (!a.Read && b.Read) || (!a.Create && b.Create) || (!a.Update && b.Update) || (!a.Delete && b.Delete)
}

func narrows(a, b storedRolePermissionRow) bool { return widens(b, a) }

// normaliseGrantInput trims, drops blanks and keeps the last entry for a
// record type named twice.
func normaliseGrantInput(role string, in []RoleGrantInput) []storedRolePermissionRow {
	byDoctype := map[string]storedRolePermissionRow{}
	for _, g := range in {
		doctype := strings.TrimSpace(g.Doctype)
		if doctype == "" {
			continue
		}
		byDoctype[doctype] = storedRolePermissionRow{Role: role, Doctype: doctype, Read: g.Read, Create: g.Create, Update: g.Update, Delete: g.Delete}
	}
	out := make([]storedRolePermissionRow, 0, len(byDoctype))
	for _, r := range byDoctype {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Doctype < out[j].Doctype })
	return out
}

// currentGrantRows reads role's rows for the named record types; forUpdate
// locks them inside a transaction. A record type with no row is absent.
func currentGrantRows(q queryer, schema, role string, doctypes []string, forUpdate bool) (map[string]storedRolePermissionRow, error) {
	stmt := fmt.Sprintf(`SELECT role, doctype_name, COALESCE(allow_read,false), COALESCE(allow_create,false),
		COALESCE(allow_update,false), COALESCE(allow_delete,false)
		FROM %s.role_permissions WHERE role = $1 AND doctype_name = ANY($2)`, schema)
	if forUpdate {
		stmt += " FOR UPDATE"
	}
	rows, err := q.Query(stmt, role, pq.Array(doctypes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]storedRolePermissionRow{}
	for rows.Next() {
		var r storedRolePermissionRow
		if err := rows.Scan(&r.Role, &r.Doctype, &r.Read, &r.Create, &r.Update, &r.Delete); err != nil {
			return nil, err
		}
		out[r.Doctype] = r
	}
	return out, rows.Err()
}

// buildPreview compares before (by record type) with the requested rows.
func buildPreview(tenantID, role string, before map[string]storedRolePermissionRow, after []storedRolePermissionRow) (RoleGrantChangePreview, error) {
	p := RoleGrantChangePreview{Role: role, Changed: []RoleGrantFlagChange{}, Widened: []string{}, Reduced: []string{},
		DangerousGained: []DangerousGrant{}, DangerousRemoved: []DangerousGrant{}}
	beforeLevels := map[string]AccessLevel{}
	afterLevels := map[string]AccessLevel{}
	for _, a := range after {
		b := before[a.Doctype] // zero value = no row = no rights
		if sameRights(a, b) {
			continue
		}
		p.Changed = append(p.Changed, RoleGrantFlagChange{Doctype: a.Doctype, From: rowRights(b), To: rowRights(a)})
		if widens(b, a) {
			p.Widened = append(p.Widened, a.Doctype)
		}
		if narrows(b, a) {
			p.Reduced = append(p.Reduced, a.Doctype)
			if _, sensitive := dangerousDoctypes[a.Doctype]; sensitive {
				p.SessionsWillEnd = true
			}
		}
		beforeLevels[a.Doctype], afterLevels[a.Doctype] = rowLevel(b), rowLevel(a)
	}
	p.DangerousGained = DangerousGained(role, beforeLevels, afterLevels)
	p.DangerousRemoved = DangerousGained(role, afterLevels, beforeLevels)
	p.HighRisk = len(p.DangerousGained)+len(p.DangerousRemoved) > 0

	// The SoD picture after the change: the role's whole stored access with
	// this change laid over it.
	whole, err := StoredRoleGrants(tenantID, role)
	if err != nil {
		return p, err
	}
	for doctype, level := range afterLevels {
		whole[doctype] = level
	}
	approver, err := ApproverDoctypesForRole(tenantID, role)
	if err != nil {
		return p, err
	}
	p.Conflicts = DetectSoDConflicts(EffectiveAccess{Role: role, Grants: whole, Capabilities: CapabilitiesForRole(role), ApproverFor: approver})
	if p.Conflicts == nil {
		p.Conflicts = []SoDFinding{}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return p, err
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.users WHERE role = $1 AND status = 'Active'`, schema), role).Scan(&p.UsersWithRole); err != nil {
		return p, err
	}
	return p, nil
}

func validateRoleGrantRequest(req RoleGrantChangeRequest) (string, []storedRolePermissionRow, error) {
	role := strings.TrimSpace(req.Role)
	rows := normaliseGrantInput(role, req.Grants)
	if role == "" || len(rows) == 0 {
		return "", nil, &ValidationError{Code: "GLOBAL-0002", Message: "Fields 'role' and 'doctype_name' (or 'doctype_names' or 'grants') are required"}
	}
	return role, rows, nil
}

// PreviewRoleGrantChange answers "what would this save do" and saves nothing.
func PreviewRoleGrantChange(tenantID string, req RoleGrantChangeRequest) (RoleGrantChangePreview, error) {
	role, rows, err := validateRoleGrantRequest(req)
	if err != nil {
		return RoleGrantChangePreview{}, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return RoleGrantChangePreview{}, err
	}
	before, err := currentGrantRows(db.DB, schema, role, rowDoctypes(rows), false)
	if err != nil {
		return RoleGrantChangePreview{}, err
	}
	p, err := buildPreview(tenantID, role, before, rows)
	if err == nil && req.EndSessions && len(p.Changed) > 0 {
		p.SessionsWillEnd = true
	}
	return p, err
}

func rowDoctypes(rows []storedRolePermissionRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Doctype
	}
	return out
}

// roleGrantEvidence is exactly what the SHA-256 covers.
type roleGrantEvidence struct {
	ID         string                    `json:"id"`
	Role       string                    `json:"role"`
	ChangedBy  string                    `json:"changed_by"`
	Reason     string                    `json:"reason"`
	Before     []storedRolePermissionRow `json:"before"`
	After      []storedRolePermissionRow `json:"after"`
	RollbackOf string                    `json:"rollback_of,omitempty"`
}

func evidenceDigest(e roleGrantEvidence) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// beforeRows lists the before-state for exactly the record types in after,
// an absent row recorded as all-false so a rollback can remove it again.
func beforeRows(role string, before map[string]storedRolePermissionRow, after []storedRolePermissionRow) []storedRolePermissionRow {
	out := make([]storedRolePermissionRow, 0, len(after))
	for _, a := range after {
		b, ok := before[a.Doctype]
		if !ok {
			b = storedRolePermissionRow{Role: role, Doctype: a.Doctype}
		}
		out = append(out, b)
	}
	return out
}

// ChangeRoleGrants saves one change to a role's grants with its evidence.
func ChangeRoleGrants(ctx context.Context, tenantID string, req RoleGrantChangeRequest) (RoleGrantChangeResult, error) {
	role, rows, err := validateRoleGrantRequest(req)
	if err != nil {
		return RoleGrantChangeResult{}, err
	}
	reason := redactAndBoundLogField(strings.TrimSpace(req.Reason), 500)
	return applyRoleGrantRows(ctx, tenantID, role, rows, req.Actor, req.ActorUserID, reason, req.EndSessions, "", nil)
}

// applyRoleGrantRows is the one write path ChangeRoleGrants and
// RollbackRoleGrantChange share. deletes names record types whose row is
// removed instead of written (a rollback of a row the change created).
func applyRoleGrantRows(ctx context.Context, tenantID, role string, rows []storedRolePermissionRow, actor, actorUserID, reason string, endSessions bool, rollbackOf string, deletes map[string]bool) (RoleGrantChangeResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return RoleGrantChangeResult{}, err
	}
	if strings.TrimSpace(actor) == "" {
		return RoleGrantChangeResult{}, &ValidationError{Code: "GLOBAL-0002", Message: "a role change must record who made it"}
	}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return RoleGrantChangeResult{}, err
	}
	defer tx.Rollback()

	before, err := currentGrantRows(tx, schema, role, rowDoctypes(rows), true)
	if err != nil {
		return RoleGrantChangeResult{}, err
	}
	for _, g := range rows {
		if deletes[g.Doctype] {
			if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.role_permissions WHERE role = $1 AND doctype_name = $2`, schema), role, g.Doctype); err != nil {
				return RoleGrantChangeResult{}, err
			}
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (role, doctype_name) DO UPDATE SET
				allow_read = EXCLUDED.allow_read, allow_create = EXCLUDED.allow_create,
				allow_update = EXCLUDED.allow_update, allow_delete = EXCLUDED.allow_delete`, schema),
			role, g.Doctype, g.Read, g.Create, g.Update, g.Delete); err != nil {
			return RoleGrantChangeResult{}, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("%s: %v", g.Doctype, err)}
		}
	}

	preview, err := buildPreview(tenantID, role, before, rows)
	if err != nil {
		return RoleGrantChangeResult{}, err
	}
	result := RoleGrantChangeResult{Preview: preview}
	if len(preview.Changed) == 0 {
		// A save that changes nothing leaves no evidence row behind.
		return result, tx.Commit()
	}
	if endSessions {
		preview.SessionsWillEnd = true
		result.Preview.SessionsWillEnd = true
	}

	id := NewDocID("ROLECHG")
	evidence := roleGrantEvidence{ID: id, Role: role, ChangedBy: actor, Reason: reason,
		Before: beforeRows(role, before, rows), After: rows, RollbackOf: rollbackOf}
	digest, err := evidenceDigest(evidence)
	if err != nil {
		return RoleGrantChangeResult{}, err
	}
	beforeJSON, _ := json.Marshal(evidence.Before)
	afterJSON, _ := json.Marshal(evidence.After)
	gainedJSON, _ := json.Marshal(preview.DangerousGained)
	removedJSON, _ := json.Marshal(preview.DangerousRemoved)

	if preview.SessionsWillEnd {
		res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.users SET credential_version = credential_version + 1
			WHERE role = $1 AND id::text <> $2`, schema), role, actorUserID)
		if err != nil {
			return RoleGrantChangeResult{}, err
		}
		n, _ := res.RowsAffected()
		result.SessionsEnded = int(n)
	}

	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.role_grant_changes
		(id, changed_by, role, reason, before_state, after_state, dangerous_gained, dangerous_removed,
		 sessions_ended, evidence_sha256, rollback_of)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''))`, schema),
		id, actor, role, reason, string(beforeJSON), string(afterJSON), string(gainedJSON), string(removedJSON),
		result.SessionsEnded, digest, rollbackOf); err != nil {
		return RoleGrantChangeResult{}, err
	}
	if rollbackOf != "" {
		res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.role_grant_changes SET rolled_back_by = $2
			WHERE id = $1 AND rolled_back_by IS NULL`, schema), rollbackOf, id)
		if err != nil {
			return RoleGrantChangeResult{}, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return RoleGrantChangeResult{}, &ValidationError{Code: "GLOBAL-0002", Message: "this role change was already rolled back"}
		}
	}
	if err := tx.Commit(); err != nil {
		return RoleGrantChangeResult{}, err
	}
	result.ChangeID = id
	if result.SessionsEnded > 0 {
		// Now, not after the ~30s live-state cache window.
		InvalidateLiveUserStatesForTenant(tenantID)
	}

	status := "GRANTS_CHANGED"
	if rollbackOf != "" {
		status = "GRANTS_ROLLED_BACK"
	}
	LogAuditEventCtx(ctx, tenantID, actor, "ROLE_PERMISSIONS", status, roleChangeAuditLine(evidence, preview, result, digest))
	return result, nil
}

// roleChangeAuditLine is the one audit row per change: readable on its own,
// and carrying the evidence digest that ties the ledger row to the chain.
func roleChangeAuditLine(e roleGrantEvidence, p RoleGrantChangePreview, r RoleGrantChangeResult, digest string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Role %s: %d record type(s) changed", e.Role, len(p.Changed))
	for i, c := range p.Changed {
		if i == 20 {
			fmt.Fprintf(&b, "; and %d more", len(p.Changed)-20)
			break
		}
		fmt.Fprintf(&b, "; %s %s -> %s", c.Doctype, c.From, c.To)
	}
	for _, g := range p.DangerousGained {
		fmt.Fprintf(&b, ". GAINS %s (%s)", g.Doctype, g.Category)
	}
	for _, g := range p.DangerousRemoved {
		fmt.Fprintf(&b, ". LOSES %s (%s)", g.Doctype, g.Category)
	}
	if r.SessionsEnded > 0 {
		fmt.Fprintf(&b, ". %d session(s) ended", r.SessionsEnded)
	}
	reason := e.Reason
	if reason == "" {
		reason = "(none given)"
	}
	if e.RollbackOf != "" {
		fmt.Fprintf(&b, ". Rollback of %s", e.RollbackOf)
	}
	fmt.Fprintf(&b, ". Reason: %s. Change %s evidence sha256 %s", reason, e.ID, digest)
	return b.String()
}

// planGrantRollback decides, record type by record type, what may be put
// back. current is what the role holds now, applied what the change being
// undone wrote, previous what was there before it. A record type is restored
// only while it still holds exactly what the change wrote, and never to a
// dangerous level it does not hold now.
func planGrantRollback(current map[string]storedRolePermissionRow, previous, applied []storedRolePermissionRow, exists map[string]bool) ([]storedRolePermissionRow, map[string]bool, RollbackOutcome) {
	out := RollbackOutcome{Restored: []RoleGrantFlagChange{}, Superseded: []string{}, Withheld: []DangerousGrant{}, Gone: []string{}}
	prevByKey := map[string]storedRolePermissionRow{}
	appliedByKey := map[string]storedRolePermissionRow{}
	keys := map[string]storedRolePermissionRow{}
	for _, r := range previous {
		prevByKey[grantKey(r.Role, r.Doctype)] = r
		keys[grantKey(r.Role, r.Doctype)] = r
	}
	for _, r := range applied {
		appliedByKey[grantKey(r.Role, r.Doctype)] = r
		keys[grantKey(r.Role, r.Doctype)] = r
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)

	var writes []storedRolePermissionRow
	deletes := map[string]bool{}
	for _, k := range ordered {
		ref := keys[k]
		role, doctype := ref.Role, ref.Doctype
		cur := current[k]
		if !sameRights(cur, appliedByKey[k]) {
			out.Superseded = append(out.Superseded, doctype)
			continue
		}
		target := prevByKey[k] // absent = the change created the row: no rights
		target.Role, target.Doctype = role, doctype
		if sameRights(cur, target) {
			continue
		}
		if !exists[doctype] {
			out.Gone = append(out.Gone, doctype)
			continue
		}
		if widens(cur, target) {
			if category, dangerous := IsDangerousGrant(role, doctype, rowLevel(target)); dangerous {
				out.Withheld = append(out.Withheld, DangerousGrant{Role: role, Doctype: doctype, Category: category, Level: rowLevel(target).String()})
				continue
			}
		}
		out.Restored = append(out.Restored, RoleGrantFlagChange{Doctype: doctype, From: rowRights(cur), To: rowRights(target)})
		writes = append(writes, target)
		if !target.Read && !target.Create && !target.Update && !target.Delete {
			// Back to no rights: remove the row rather than keep an empty one.
			deletes[grantKey(role, doctype)] = true
		}
	}
	return writes, deletes, out
}

func existingDoctypes(q queryer, schema string, doctypes []string) (map[string]bool, error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT name FROM %s.doctype_meta WHERE name = ANY($1)`, schema), pq.Array(doctypes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// RollbackRoleGrantChange undoes one saved change, as a change of its own
// with its own reason and evidence.
func RollbackRoleGrantChange(ctx context.Context, tenantID, changeID, actor, actorUserID, reason string) (RoleGrantChangeResult, RollbackOutcome, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) < 5 {
		return RoleGrantChangeResult{}, RollbackOutcome{}, errRoleChangeReasonRequired
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return RoleGrantChangeResult{}, RollbackOutcome{}, err
	}
	var role, beforeJSON, afterJSON string
	var rollbackOf, rolledBackBy sql.NullString
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT role, before_state::text, after_state::text, rollback_of, rolled_back_by
		FROM %s.role_grant_changes WHERE id = $1`, schema), changeID).Scan(&role, &beforeJSON, &afterJSON, &rollbackOf, &rolledBackBy); err != nil {
		if err == sql.ErrNoRows {
			return RoleGrantChangeResult{}, RollbackOutcome{}, &ValidationError{Code: "GLOBAL-0004", Message: "no role change with this id"}
		}
		return RoleGrantChangeResult{}, RollbackOutcome{}, err
	}
	if rolledBackBy.Valid {
		return RoleGrantChangeResult{}, RollbackOutcome{}, &ValidationError{Code: "GLOBAL-0002", Message: "this role change was already rolled back"}
	}
	if rollbackOf.Valid {
		return RoleGrantChangeResult{}, RollbackOutcome{}, &ValidationError{Code: "GLOBAL-0002", Message: "this is itself a rollback - make a new change instead"}
	}
	var previous, applied []storedRolePermissionRow
	if err := json.Unmarshal([]byte(beforeJSON), &previous); err != nil {
		return RoleGrantChangeResult{}, RollbackOutcome{}, err
	}
	if err := json.Unmarshal([]byte(afterJSON), &applied); err != nil {
		return RoleGrantChangeResult{}, RollbackOutcome{}, err
	}
	doctypes := rowDoctypes(applied)
	current, err := currentGrantRows(db.DB, schema, role, doctypes, false)
	if err != nil {
		return RoleGrantChangeResult{}, RollbackOutcome{}, err
	}
	currentByKey := map[string]storedRolePermissionRow{}
	for _, r := range current {
		currentByKey[grantKey(role, r.Doctype)] = r
	}
	exists, err := existingDoctypes(db.DB, schema, doctypes)
	if err != nil {
		return RoleGrantChangeResult{}, RollbackOutcome{}, err
	}
	writes, deletes, outcome := planGrantRollback(currentByKey, previous, applied, exists)
	if len(writes) == 0 {
		why := rollbackSummary(outcome)
		if why == "nothing withheld" {
			why = "the role already holds what it held before"
		}
		return RoleGrantChangeResult{}, outcome, &ValidationError{Code: "GLOBAL-0002", Message: "nothing to roll back: " + why}
	}
	byDoctype := map[string]bool{}
	for key := range deletes {
		byDoctype[strings.TrimPrefix(key, role+"\x00")] = true
	}
	result, err := applyRoleGrantRows(ctx, tenantID, role, writes, actor, actorUserID, redactAndBoundLogField(reason, 500), false, changeID, byDoctype)
	return result, outcome, err
}

func rollbackSummary(o RollbackOutcome) string {
	var parts []string
	if len(o.Superseded) > 0 {
		parts = append(parts, "changed again since: "+strings.Join(o.Superseded, ", "))
	}
	for _, w := range o.Withheld {
		parts = append(parts, fmt.Sprintf("%s (%s) is not handed back - grant it again with a reason", w.Doctype, w.Category))
	}
	if len(o.Gone) > 0 {
		parts = append(parts, "no longer exist: "+strings.Join(o.Gone, ", "))
	}
	if len(parts) == 0 {
		return "nothing withheld"
	}
	return strings.Join(parts, "; ")
}

// VerifyRoleGrantChangeEvidence recomputes a change's SHA-256 from its
// ledger row and checks it against the digest written into the signed audit
// chain when the change was made. A row edited afterwards - by anyone,
// including a database superuser - no longer matches.
func VerifyRoleGrantChangeEvidence(tenantID, changeID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var e roleGrantEvidence
	var beforeJSON, afterJSON, stored string
	var rollbackOf sql.NullString
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, role, changed_by, reason, before_state::text, after_state::text,
		rollback_of, evidence_sha256 FROM %s.role_grant_changes WHERE id = $1`, schema), changeID).
		Scan(&e.ID, &e.Role, &e.ChangedBy, &e.Reason, &beforeJSON, &afterJSON, &rollbackOf, &stored); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(beforeJSON), &e.Before); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(afterJSON), &e.After); err != nil {
		return err
	}
	e.RollbackOf = rollbackOf.String
	digest, err := evidenceDigest(e)
	if err != nil {
		return err
	}
	var audited int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.audit_logs
		WHERE action = 'ROLE_PERMISSIONS' AND details LIKE '%%' || $1`, schema),
		"Change "+changeID+" evidence sha256 "+digest).Scan(&audited); err != nil {
		return err
	}
	if digest != stored || audited == 0 {
		return fmt.Errorf("role change %s: its evidence no longer matches the audit log - it was edited after it was recorded", changeID)
	}
	return nil
}
