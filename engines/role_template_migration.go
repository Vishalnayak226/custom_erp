package engines

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 47.1.7 - existing-tenant migration for the 47.1.5 role templates.
//
// The requirement is narrow and worth restating, because it is the part that
// usually gets skipped: "produce per-tenant before/after grant diff, owner
// approval and reversible migration. Do not silently reset legitimate custom
// roles."
//
// So this file deliberately does NOT ship a migration that rewrites
// role_permissions. A tenant's roles are live production access; changing
// them is a decision its owner makes, having seen exactly what changes.
// db/migrations_stage47_1_role_templates.sql therefore only creates the
// ledger, and every grant change goes:
//
//	PlanRoleTemplateMigration  -> the before/after diff, changing nothing
//	ApplyRoleTemplateMigration -> applies it, recording who approved and
//	                              the complete prior state
//	RevertRoleTemplateMigration-> restores that prior state exactly
//
// Roles with no template (a tenant's own "Night Shift Lead") are never
// touched and are reported separately, so an administrator can see that they
// were left alone rather than having to infer it from silence.

// GrantChange is one doctype's before/after for one role.
type GrantChange struct {
	Doctype string `json:"doctype"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// RoleTemplateDiff is the whole before/after for one role in one tenant.
type RoleTemplateDiff struct {
	Role     string `json:"role"`
	Template string `json:"template"`
	Summary  string `json:"summary"`
	// Added/Tightened/Loosened/Removed split the changes by direction,
	// because "12 changes" is not reviewable and "3 grants removed, 1
	// widened" is.
	Added     []GrantChange `json:"added"`
	Tightened []GrantChange `json:"tightened"`
	Loosened  []GrantChange `json:"loosened"`
	Removed   []GrantChange `json:"removed"`
	// Conflicts is the SoD picture AFTER the change - the point of showing
	// a diff at all is deciding whether to accept the result.
	Conflicts []SoDFinding `json:"sod_conflicts_after"`
}

// Changed reports whether anything at all would change for this role.
func (d RoleTemplateDiff) Changed() bool {
	return len(d.Added)+len(d.Tightened)+len(d.Loosened)+len(d.Removed) > 0
}

// RoleTemplateMigrationPlan is the tenant-wide preview.
type RoleTemplateMigrationPlan struct {
	TenantID string             `json:"tenant_id"`
	Diffs    []RoleTemplateDiff `json:"diffs"`
	// UntouchedCustomRoles are roles present in role_permissions that no
	// template governs. Named explicitly: "we did not touch these" is the
	// half of this item that a silent migration gets wrong.
	UntouchedCustomRoles []string `json:"untouched_custom_roles"`
}

// PlanRoleTemplateMigration computes the diff for every template against a
// tenant's current role_permissions. It changes nothing.
func PlanRoleTemplateMigration(tenantID string) (RoleTemplateMigrationPlan, error) {
	plan := RoleTemplateMigrationPlan{TenantID: tenantID, UntouchedCustomRoles: []string{}}

	storedRoles, err := rolesInPermissions(tenantID)
	if err != nil {
		return plan, err
	}
	governed := map[string]bool{}

	for _, template := range RoleTemplates() {
		// One diff per role STRING the template governs that already exists
		// in the tenant, plus the canonical name itself so a template with
		// no existing role still shows what it would create.
		names := []string{template.Name}
		for _, legacy := range template.LegacyNames {
			names = append(names, legacy)
		}
		for _, role := range names {
			if role != template.Name && !storedRoles[role] {
				continue // a legacy name this tenant never used
			}
			governed[role] = true
			diff, err := diffRoleAgainstTemplate(tenantID, role, template)
			if err != nil {
				return plan, err
			}
			plan.Diffs = append(plan.Diffs, diff)
		}
	}

	for role := range storedRoles {
		if !governed[role] {
			plan.UntouchedCustomRoles = append(plan.UntouchedCustomRoles, role)
		}
	}
	sort.Strings(plan.UntouchedCustomRoles)
	return plan, nil
}

func diffRoleAgainstTemplate(tenantID, role string, template RoleTemplate) (RoleTemplateDiff, error) {
	diff := RoleTemplateDiff{Role: role, Template: template.Name, Summary: template.Summary}

	stored, err := StoredRoleGrants(tenantID, role)
	if err != nil {
		return diff, err
	}
	wanted, err := TemplateGrants(tenantID, template)
	if err != nil {
		return diff, err
	}

	seen := map[string]bool{}
	for _, doctype := range sortedKeys(wanted) {
		seen[doctype] = true
		before, had := stored[doctype]
		after := wanted[doctype]
		change := GrantChange{Doctype: doctype, From: before.String(), To: after.String()}
		switch {
		case !had || before == AccessNone:
			diff.Added = append(diff.Added, change)
		case after < before:
			diff.Tightened = append(diff.Tightened, change)
		case after > before:
			diff.Loosened = append(diff.Loosened, change)
		}
	}
	for _, doctype := range sortedKeys(stored) {
		if seen[doctype] || stored[doctype] == AccessNone {
			continue
		}
		diff.Removed = append(diff.Removed, GrantChange{
			Doctype: doctype, From: stored[doctype].String(), To: AccessNone.String(),
		})
	}

	approver, err := ApproverDoctypesForRole(tenantID, role)
	if err != nil {
		return diff, err
	}
	diff.Conflicts = DetectSoDConflicts(EffectiveAccess{
		Role: role, Grants: wanted, Capabilities: template.Capabilities, ApproverFor: approver,
	})
	if diff.Conflicts == nil {
		diff.Conflicts = []SoDFinding{}
	}
	return diff, nil
}

func sortedKeys(m map[string]AccessLevel) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// RolesWithStoredGrants lists every role that has any row in
// role_permissions, sorted - template roles and a tenant's own custom roles
// alike, since an unnoticed SoD conflict is likeliest in the latter.
func RolesWithStoredGrants(tenantID string) ([]string, error) {
	roles, err := rolesInPermissions(tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(roles))
	for role := range roles {
		out = append(out, role)
	}
	sort.Strings(out)
	return out, nil
}

func rolesInPermissions(tenantID string) (map[string]bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT role FROM %s.role_permissions`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		out[role] = true
	}
	return out, rows.Err()
}

// storedRolePermissionRow is one role_permissions row, as snapshotted.
type storedRolePermissionRow struct {
	Role    string `json:"role"`
	Doctype string `json:"doctype"`
	Read    bool   `json:"read"`
	Create  bool   `json:"create"`
	Update  bool   `json:"update"`
	Delete  bool   `json:"delete"`
}

func snapshotRoles(tenantID string, roles []string) ([]storedRolePermissionRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var out []storedRolePermissionRow
	for _, role := range roles {
		rows, err := db.DB.Query(fmt.Sprintf(
			`SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
			 FROM %s.role_permissions WHERE role = $1`, schema), role)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r storedRolePermissionRow
			if err := rows.Scan(&r.Role, &r.Doctype, &r.Read, &r.Create, &r.Update, &r.Delete); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, r)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	if out == nil {
		out = []storedRolePermissionRow{}
	}
	return out, nil
}

// ApplyRoleTemplateMigration rewrites the named roles' grants to match their
// templates, in one transaction, recording the complete prior state so
// RevertRoleTemplateMigration can put it back exactly.
//
// approvedBy is required. A role change with no named approver is precisely
// the "silent reset" this item forbids, so it is refused rather than
// defaulted to "system".
func ApplyRoleTemplateMigration(tenantID string, roles []string, approvedBy string) (string, error) {
	if strings.TrimSpace(approvedBy) == "" {
		return "", &ValidationError{Code: "GLOBAL-0002", Message: "a role-template migration must record who approved it"}
	}
	if len(roles) == 0 {
		return "", &ValidationError{Code: "GLOBAL-0002", Message: "name at least one role to migrate"}
	}
	templates := map[string]RoleTemplate{}
	for _, role := range roles {
		template, ok := RoleTemplateFor(role)
		if !ok {
			return "", &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("role %q has no template - custom roles are never rewritten by this migration", role)}
		}
		templates[role] = template
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	previous, err := snapshotRoles(tenantID, roles)
	if err != nil {
		return "", err
	}
	previousJSON, err := json.Marshal(previous)
	if err != nil {
		return "", err
	}

	var applied []storedRolePermissionRow
	for _, role := range roles {
		grants, err := TemplateGrants(tenantID, templates[role])
		if err != nil {
			return "", err
		}
		for _, doctype := range sortedKeys(grants) {
			read, create, update, del := grants[doctype].Permissions()
			applied = append(applied, storedRolePermissionRow{
				Role: role, Doctype: doctype, Read: read, Create: create, Update: update, Delete: del,
			})
		}
	}
	if applied == nil {
		applied = []storedRolePermissionRow{}
	}
	appliedJSON, err := json.Marshal(applied)
	if err != nil {
		return "", err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	for _, role := range roles {
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.role_permissions WHERE role = $1`, schema), role); err != nil {
			return "", err
		}
	}
	for _, row := range applied {
		if _, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
			 VALUES ($1, $2, $3, $4, $5, $6)`, schema),
			row.Role, row.Doctype, row.Read, row.Create, row.Update, row.Delete); err != nil {
			return "", err
		}
	}

	migrationID := NewDocID("ROLEMIG")
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.role_template_migrations (id, approved_by, roles, previous_state, applied_state)
		 VALUES ($1, $2, $3, $4, $5)`, schema),
		migrationID, approvedBy, strings.Join(roles, ","), string(previousJSON), string(appliedJSON)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return migrationID, nil
}

// RevertRoleTemplateMigration restores the exact grants that existed before
// a migration, including removing rows it created.
func RevertRoleTemplateMigration(tenantID, migrationID, revertedBy string) error {
	if strings.TrimSpace(revertedBy) == "" {
		return &ValidationError{Code: "GLOBAL-0002", Message: "a revert must record who performed it"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var rolesCSV, previousJSON string
	var revertedAt *string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT roles, previous_state, reverted_at FROM %s.role_template_migrations WHERE id = $1`, schema),
		migrationID).Scan(&rolesCSV, &previousJSON, &revertedAt); err != nil {
		return err
	}
	if revertedAt != nil {
		return &ValidationError{Code: "GLOBAL-0002", Message: "this role-template migration was already reverted"}
	}
	var previous []storedRolePermissionRow
	if err := json.Unmarshal([]byte(previousJSON), &previous); err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, role := range strings.Split(rolesCSV, ",") {
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.role_permissions WHERE role = $1`, schema), role); err != nil {
			return err
		}
	}
	for _, row := range previous {
		if _, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
			 VALUES ($1, $2, $3, $4, $5, $6)`, schema),
			row.Role, row.Doctype, row.Read, row.Create, row.Update, row.Delete); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.role_template_migrations SET reverted_at = CURRENT_TIMESTAMP, reverted_by = $2 WHERE id = $1`, schema),
		migrationID, revertedBy); err != nil {
		return err
	}
	return tx.Commit()
}

// RoleTemplateMigrationRecord is one ledger row, for the admin list.
type RoleTemplateMigrationRecord struct {
	ID         string  `json:"id"`
	AppliedAt  string  `json:"applied_at"`
	ApprovedBy string  `json:"approved_by"`
	Roles      string  `json:"roles"`
	RevertedAt *string `json:"reverted_at,omitempty"`
	RevertedBy *string `json:"reverted_by,omitempty"`
}

// ListRoleTemplateMigrations returns the ledger, newest first.
func ListRoleTemplateMigrations(tenantID string) ([]RoleTemplateMigrationRecord, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, applied_at, approved_by, roles, reverted_at, reverted_by
		 FROM %s.role_template_migrations ORDER BY applied_at DESC LIMIT 200`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoleTemplateMigrationRecord{}
	for rows.Next() {
		var r RoleTemplateMigrationRecord
		if err := rows.Scan(&r.ID, &r.AppliedAt, &r.ApprovedBy, &r.Roles, &r.RevertedAt, &r.RevertedBy); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
