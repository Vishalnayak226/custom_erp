package engines

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 49.3.6 (c): a store-access change (a role's, or one user's) goes
// through the same evidence and preview as a role's record-type grants. The
// settings stay where every reader looks (role_store_access, users); this adds
// the before/after row, its SHA-256 in the signed audit chain, and a preview
// that says whether the change widens or narrows where people can work.
// Narrowing bites at the next click already (the live state is dropped).

// StoreAccessChangeRequest is one change. Kind is "role" or "user"; Subject is
// the role name or the user id.
type StoreAccessChangeRequest struct {
	Kind    string
	Subject string
	Access  string
	Stores  []string
	Actor   string
	Reason  string
}

// StoreAccessState is one setting, as stored.
type StoreAccessState struct {
	Access string   `json:"access"`
	Stores []string `json:"stores"`
}

// StoreAccessChangePreview is what a change does, computed without saving.
type StoreAccessChangePreview struct {
	Kind     string   `json:"kind"`
	Subject  string   `json:"subject"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	FromList []string `json:"from_stores"`
	ToList   []string `json:"to_stores"`
	Widened  bool     `json:"widened"`
	Narrowed bool     `json:"narrowed"`
	Changed  bool     `json:"changed"`
}

// StoreAccessChangeResult is a saved change; ChangeID is "" for a no-op.
type StoreAccessChangeResult struct {
	ChangeID string                   `json:"change_id,omitempty"`
	Preview  StoreAccessChangePreview `json:"preview"`
}

type storeReach struct {
	all bool
	set map[string]bool
}

func reachOf(mode string, stores []string, role StoreAccessState) storeReach {
	r := storeReach{set: map[string]bool{}}
	switch mode {
	case StoreAccessAll:
		r.all = true
	case StoreAccessInherit:
		r = reachOf(role.Access, role.Stores, StoreAccessState{})
		for _, s := range stores {
			r.set[s] = true
		}
	case StoreAccessSelected:
		for _, s := range stores {
			r.set[s] = true
		}
	}
	return r
}

// widerThan: a reaches somewhere b does not.
func (a storeReach) widerThan(b storeReach) bool {
	if b.all {
		return false
	}
	if a.all {
		return true
	}
	for s := range a.set {
		if !b.set[s] {
			return true
		}
	}
	return false
}

type storeChangePlan struct {
	schema string
	before StoreAccessState
	after  StoreAccessState
}

func planStoreAccessChange(tenantID string, req StoreAccessChangeRequest) (StoreAccessChangePreview, storeChangePlan, error) {
	var c storeChangePlan
	p := StoreAccessChangePreview{Kind: req.Kind, Subject: strings.TrimSpace(req.Subject)}
	if p.Kind != "role" && p.Kind != "user" {
		return p, c, &ValidationError{Code: "STORE-0851", Message: "Choose a role or a user."}
	}
	if p.Subject == "" {
		return p, c, &ValidationError{Code: "STORE-0851", Message: "Choose the role."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return p, c, err
	}
	c.schema = schema
	mode, err := normaliseStoreMode(req.Access, p.Kind == "user")
	if err != nil {
		return p, c, err
	}
	stores := req.Stores
	if mode == StoreAccessAll || mode == StoreAccessHome {
		stores = nil
	}
	clean, err := cleanStoreList(schema, stores)
	if err != nil {
		return p, c, err
	}
	c.after = StoreAccessState{Access: mode, Stores: clean}
	var roleState StoreAccessState
	if p.Kind == "role" {
		cur, err := GetRoleStoreAccess(tenantID, p.Subject)
		if err != nil {
			return p, c, err
		}
		c.before = StoreAccessState{Access: cur.Access, Stores: cur.Stores}
	} else {
		cur, err := GetUserStoreAccess(tenantID, p.Subject)
		if err != nil {
			return p, c, &ValidationError{Code: "STORE-0851", Message: "No such user."}
		}
		c.before = StoreAccessState{Access: cur.Access, Stores: cur.Stores}
		roleState = StoreAccessState{Access: cur.RoleAccess, Stores: cur.RoleStores}
	}
	if c.before.Stores == nil {
		c.before.Stores = []string{}
	}
	sort.Strings(c.before.Stores)
	p.From, p.To = c.before.Access, c.after.Access
	p.FromList, p.ToList = c.before.Stores, c.after.Stores
	b := reachOf(c.before.Access, c.before.Stores, roleState)
	a := reachOf(c.after.Access, c.after.Stores, roleState)
	p.Widened, p.Narrowed = a.widerThan(b), b.widerThan(a)
	p.Changed = c.before.Access != c.after.Access || strings.Join(c.before.Stores, ",") != strings.Join(c.after.Stores, ",")
	return p, c, nil
}

// PreviewStoreAccessChange answers what a save would do, and saves nothing.
func PreviewStoreAccessChange(tenantID string, req StoreAccessChangeRequest) (StoreAccessChangePreview, error) {
	p, _, err := planStoreAccessChange(tenantID, req)
	return p, err
}

type storeAccessEvidence struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind"`
	Subject   string           `json:"subject"`
	ChangedBy string           `json:"changed_by"`
	Reason    string           `json:"reason"`
	Before    StoreAccessState `json:"before"`
	After     StoreAccessState `json:"after"`
}

func storeEvidenceDigest(e storeAccessEvidence) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ChangeStoreAccess saves a role's or a user's store access with its evidence.
func ChangeStoreAccess(ctx context.Context, tenantID string, req StoreAccessChangeRequest) (StoreAccessChangeResult, error) {
	p, c, err := planStoreAccessChange(tenantID, req)
	if err != nil {
		return StoreAccessChangeResult{}, err
	}
	if strings.TrimSpace(req.Actor) == "" {
		return StoreAccessChangeResult{}, &ValidationError{Code: "GLOBAL-0002", Message: "a store change must record who made it"}
	}
	if req.Kind == "role" {
		err = SetRoleStoreAccess(tenantID, p.Subject, c.after.Access, c.after.Stores, req.Actor)
	} else {
		err = SetUserStoreAccess(tenantID, p.Subject, c.after.Access, c.after.Stores, req.Actor)
	}
	if err != nil {
		return StoreAccessChangeResult{}, err
	}
	res := StoreAccessChangeResult{Preview: p}
	if !p.Changed {
		return res, nil
	}
	e := storeAccessEvidence{ID: NewDocID("STORECHG"), Kind: p.Kind, Subject: p.Subject, ChangedBy: req.Actor,
		Reason: redactAndBoundLogField(strings.TrimSpace(req.Reason), 500), Before: c.before, After: c.after}
	digest, err := storeEvidenceDigest(e)
	if err != nil {
		return res, err
	}
	beforeJSON, _ := json.Marshal(e.Before)
	afterJSON, _ := json.Marshal(e.After)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.store_access_changes
		(id, changed_by, kind, subject, reason, before_state, after_state, widened, narrowed, evidence_sha256)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, c.schema),
		e.ID, e.ChangedBy, e.Kind, e.Subject, e.Reason, string(beforeJSON), string(afterJSON), p.Widened, p.Narrowed, digest); err != nil {
		return res, err
	}
	reason := e.Reason
	if reason == "" {
		reason = "(none given)"
	}
	LogAuditEventCtx(ctx, tenantID, req.Actor, "ROLE_PERMISSIONS", "STORE_ACCESS_CHANGED", fmt.Sprintf(
		"%s %s store access %s %v -> %s %v (widened %t, narrowed %t). Reason: %s. Change %s evidence sha256 %s",
		p.Kind, p.Subject, p.From, p.FromList, p.To, p.ToList, p.Widened, p.Narrowed, reason, e.ID, digest))
	res.ChangeID = e.ID
	return res, nil
}

// VerifyStoreAccessChangeEvidence recomputes a change's SHA-256 from its row
// and checks it against the digest written into the signed audit chain.
func VerifyStoreAccessChangeEvidence(tenantID, changeID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var e storeAccessEvidence
	var beforeJSON, afterJSON, stored string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, kind, subject, changed_by, reason, before_state::text, after_state::text,
		evidence_sha256 FROM %s.store_access_changes WHERE id = $1`, schema), changeID).
		Scan(&e.ID, &e.Kind, &e.Subject, &e.ChangedBy, &e.Reason, &beforeJSON, &afterJSON, &stored); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(beforeJSON), &e.Before); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(afterJSON), &e.After); err != nil {
		return err
	}
	digest, err := storeEvidenceDigest(e)
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
		return fmt.Errorf("store change %s: its evidence no longer matches the audit log - it was edited after it was recorded", changeID)
	}
	return nil
}
