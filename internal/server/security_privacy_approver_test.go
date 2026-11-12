package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// 49.12.5 (Privacy Approver): a user holding the role can be named as, and can
// act as, the approver of a notification decision without being an Admin. Not
// yourself, an active user, a stale decision and a closed record stay refused;
// an Admin can still approve; the role reaches nothing else.
func TestStage49125PrivacyApproverThroughTheDoor(t *testing.T) {
	db.InitDB(testConnStr())
	aID, _, aClean := seedSecurityOpsUser(t, "Super Admin") // decider
	defer aClean()
	pID, _, pClean := seedSecurityOpsUser(t, engines.RolePrivacyApprover)
	defer pClean()
	p2ID, _, p2Clean := seedSecurityOpsUser(t, engines.RolePrivacyApprover) // never named
	defer p2Clean()
	offID, _, offClean := seedSecurityOpsUser(t, engines.RolePrivacyApprover) // switched off
	defer offClean()
	nID, _, nClean := seedSecurityOpsUser(t, "Cashier")
	defer nClean()
	admID, _, admClean := seedSecurityOpsUser(t, "Admin")
	defer admClean()
	defer db.DB.Exec(`DELETE FROM tenant_default.notification_decisions WHERE incident_title LIKE 'T49125P%'`)
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action LIKE 'NOTIFY_%' AND user_id IN ($1, $2, $3, $4, $5, $6)`, aID, pID, p2ID, offID, nID, admID)
	db.DB.Exec(`UPDATE tenant_default.users SET status = 'Inactive' WHERE id = $1`, offID)
	engines.ResetLiveUserStateCache()
	tok := func(id, role string) string { return engines.SignToken(id, id, role, "default", "HO", 1) }
	aTok, pTok, p2Tok, nTok, admTok := tok(aID, "Super Admin"), tok(pID, engines.RolePrivacyApprover), tok(p2ID, engines.RolePrivacyApprover), tok(nID, "Cashier"), tok(admID, "Admin")

	const base = "/api/v1/admin/security/notify/"
	call := func(h http.HandlerFunc, method, path, token string, body interface{}) (int, map[string]interface{}) {
		rec := doRequest(t, apiMiddleware(h), method, base+path, token, body)
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	open := func(title string) string {
		if code, out := call(handleNotifyIncidentOpen, http.MethodPost, "incidents", aTok, map[string]interface{}{
			"title": title, "discovered_at": time.Now().Add(-time.Hour).UTC(), "keys": []string{"cert-in"}}); code != http.StatusOK {
			t.Fatalf("open: %d %v", code, out)
		}
		_, out := call(handleNotifyDecisions, http.MethodGet, "decisions", aTok, nil)
		for _, d := range out["decisions"].([]interface{}) {
			if m := d.(map[string]interface{}); m["incident_title"] == title {
				return m["id"].(string)
			}
		}
		t.Fatal("decision not listed")
		return ""
	}
	decidedAt := func(id string) string {
		_, out := call(handleNotifyDecisions, http.MethodGet, "decisions", aTok, nil)
		for _, d := range out["decisions"].([]interface{}) {
			if m := d.(map[string]interface{}); m["id"] == id {
				s, _ := m["decided_at"].(string)
				return s
			}
		}
		return ""
	}
	decide := func(id, approver string) int {
		code, _ := call(handleNotifyDecide, http.MethodPost, "decide", aTok, map[string]interface{}{
			"id": id, "decision": "notify", "reason": "Counsel read the facts", "evidence_ref": "DOC-1", "approver": approver})
		return code
	}
	review := func(token, id string) int {
		code, _ := call(handleNotifyReview, http.MethodPost, "review", token, map[string]interface{}{"id": id, "approve": true, "decided_at": decidedAt(id)})
		return code
	}

	id := open("T49125P lost laptop")
	// Who can be named: not a Cashier, not an inactive Privacy Approver, not the decider.
	if c := decide(id, nID); c != http.StatusUnprocessableEntity {
		t.Fatalf("a user without the role and not Admin was named approver: %d", c)
	}
	if c := decide(id, offID); c != http.StatusUnprocessableEntity {
		t.Fatalf("an inactive Privacy Approver was named approver: %d", c)
	}
	if c := decide(id, aID); c != http.StatusUnprocessableEntity {
		t.Fatalf("the decider named themself approver: %d", c)
	}
	// The picker offers the Privacy Approver and the Admin, and no one it should not.
	_, ap := call(handleNotifyApprovers, http.MethodGet, "approvers", aTok, nil)
	offered := map[string]bool{}
	for _, n := range ap["approvers"].([]interface{}) {
		offered[n.(string)] = true
	}
	if !offered[pID] || !offered[admID] || offered[nID] || offered[offID] || offered[aID] {
		t.Fatalf("picker offered %v", offered)
	}
	if code, _ := call(handleNotifyApprovers, http.MethodGet, "approvers", pTok, nil); code != http.StatusForbidden {
		t.Fatalf("a Privacy Approver may not read the picker: %d", code)
	}
	if c := decide(id, pID); c != http.StatusOK {
		t.Fatalf("naming a Privacy Approver: %d", c)
	}

	// The role reaches the decision it is named on, and nothing else.
	_, out := call(handleNotifyDecisions, http.MethodGet, "decisions", pTok, nil)
	if list := out["decisions"].([]interface{}); len(list) != 1 || list[0].(map[string]interface{})["id"] != id {
		t.Fatalf("the approver must see exactly the decision naming them, got %d", len(list))
	}
	if _, out := call(handleNotifyDecisions, http.MethodGet, "decisions", p2Tok, nil); len(out["decisions"].([]interface{})) != 0 {
		t.Fatal("a Privacy Approver not named must see no decisions")
	}
	for _, c := range []struct {
		h            http.HandlerFunc
		method, path string
		body         interface{}
		what         string
	}{
		{handleNotifyMatrix, http.MethodGet, "matrix", nil, "matrix"},
		{handleNotifyMatrixSave, http.MethodPost, "matrix", map[string]interface{}{"entries": []interface{}{}}, "matrix save"},
		{handleNotifyIncidentOpen, http.MethodPost, "incidents", map[string]interface{}{"title": "T49125P x", "keys": []string{"cert-in"}}, "incident"},
		{handleNotifyDecide, http.MethodPost, "decide", map[string]interface{}{"id": id, "decision": "notify", "reason": "Counsel read the facts", "evidence_ref": "DOC-1", "approver": admID}, "decide"},
	} {
		if code, _ := call(c.h, c.method, c.path, pTok, c.body); code != http.StatusForbidden {
			t.Errorf("Privacy Approver reached %s: %d", c.what, code)
		}
	}
	if rec := doRequest(t, apiMiddleware(handleSecurityExportStatus), http.MethodGet, "/api/v1/admin/security/export", pTok, nil); rec.Code != http.StatusForbidden {
		t.Errorf("Privacy Approver reached the event export: %d", rec.Code)
	}

	// Who may approve: the decider no; a user without the role no; an unnamed
	// Privacy Approver no; the named one yes.
	if c := review(aTok, id); c != http.StatusUnprocessableEntity {
		t.Fatalf("the decider approved their own decision: %d", c)
	}
	if c := review(nTok, id); c != http.StatusForbidden {
		t.Fatalf("a user without the role approved: %d", c)
	}
	if c := review(p2Tok, id); c != http.StatusUnprocessableEntity {
		t.Fatalf("a Privacy Approver who is not the named one approved: %d", c)
	}
	// Stale: the decision was replaced after the approver read it.
	stale := decidedAt(id)
	time.Sleep(10 * time.Millisecond)
	if c := decide(id, pID); c != http.StatusOK {
		t.Fatalf("re-decide: %d", c)
	}
	if code, _ := call(handleNotifyReview, http.MethodPost, "review", pTok, map[string]interface{}{"id": id, "approve": true, "decided_at": stale}); code != http.StatusUnprocessableEntity {
		t.Fatalf("a stale decision was approved: %d", code)
	}
	// An account switched off after naming ends it, at the door and in the engine.
	db.DB.Exec(`UPDATE tenant_default.users SET status = 'Inactive' WHERE id = $1`, pID)
	engines.ResetLiveUserStateCache()
	if c := review(pTok, id); c == http.StatusOK {
		t.Fatal("an inactive Privacy Approver approved")
	}
	if err := engines.ReviewNotifyDecision("default", pID, id, true, "", decidedAt(id)); err == nil {
		t.Fatal("the engine accepted an inactive approver")
	}
	db.DB.Exec(`UPDATE tenant_default.users SET status = 'Active' WHERE id = $1`, pID)
	engines.ResetLiveUserStateCache()
	if c := review(pTok, id); c != http.StatusOK {
		t.Fatalf("the named, active Privacy Approver was refused: %d", c)
	}
	var status string
	db.DB.QueryRow(`SELECT status FROM tenant_default.notification_decisions WHERE id::text = $1`, id).Scan(&status)
	if status != "Closed" {
		t.Fatalf("status %q, want Closed after the Privacy Approver approved", status)
	}
	// Closed means closed.
	if c := decide(id, admID); c != http.StatusUnprocessableEntity {
		t.Fatalf("a closed decision changed: %d", c)
	}

	// An Admin still can.
	id2 := open("T49125P second")
	if c := decide(id2, admID); c != http.StatusOK {
		t.Fatalf("naming an Admin: %d", c)
	}
	if c := review(admTok, id2); c != http.StatusOK {
		t.Fatalf("an Admin approving: %d", c)
	}
}

// A Privacy Approver of another client is not a user of this one: they cannot be
// named here, and their own client's session cannot close this client's decision.
func TestStage49125PrivacyApproverOfAnotherTenantCannotApprove(t *testing.T) {
	db.InitDB(testConnStr())
	aID, _, aClean := seedSecurityOpsUser(t, "Super Admin")
	defer aClean()
	defer db.DB.Exec(`DELETE FROM tenant_default.notification_decisions WHERE incident_title LIKE 'T49125X%'`)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	tenantID, schema := "p49125"+suffix, "tenant_p49125"+suffix
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
		db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantID)
		engines.InvalidateTenantLifecycleCache(tenantID)
	}()
	if _, err := engines.ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	other := "pa-other-" + suffix
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.users (id, username, password_hash, email, role, status, location_code)
		VALUES ($1, $1, 'x', $2, $3, 'Active', 'HO')`, schema), other, other+"@x.invalid", engines.RolePrivacyApprover); err != nil {
		t.Fatalf("seed other tenant user: %v", err)
	}
	if _, err := engines.OpenNotifyIncident("default", aID, "T49125X cross", time.Now().Add(-time.Hour).UTC(), []string{"cert-in"}); err != nil {
		t.Fatal(err)
	}
	list, err := engines.ListNotifyDecisions("default")
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, d := range list {
		if d.IncidentTitle == "T49125X cross" {
			id = d.ID
		}
	}
	if id == "" {
		t.Fatal("decision not found")
	}
	if err := engines.RecordNotifyDecision("default", aID, id, "notify", "Counsel read the facts", "DOC-1", other); err == nil {
		t.Fatal("a Privacy Approver of another client was named")
	}
	if err := engines.ReviewNotifyDecision(tenantID, other, id, true, "", ""); err == nil {
		t.Fatal("another client's session closed this client's decision")
	}
}
