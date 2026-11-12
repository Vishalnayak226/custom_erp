package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 49.12.5 through the real door: the matrix is versioned and flagged for
// counsel, a decision keeps the version it was opened under, the decider cannot
// approve, only the named approver can, and a closed decision does not change.
func TestStage49125NotifyDecisionWorkflowThroughTheDoor(t *testing.T) {
	db.InitDB(testConnStr())
	aID, _, aClean := seedSecurityOpsUser(t, "Super Admin")
	defer aClean()
	bID, _, bClean := seedSecurityOpsUser(t, "Super Admin")
	defer bClean()
	cID, _, cClean := seedSecurityOpsUser(t, "Cashier")
	defer cClean()
	defer db.DB.Exec(`DELETE FROM tenant_default.notification_decisions WHERE incident_title LIKE 'T49125%'`)
	defer db.DB.Exec(`DELETE FROM tenant_default.notification_matrix_versions WHERE note LIKE 'T49125%'`)
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action LIKE 'NOTIFY_%' AND user_id IN ($1, $2, $3)`, aID, bID, cID)
	engines.ResetLiveUserStateCache()
	tok := func(id, role string) string { return engines.SignToken(id, id, role, "default", "HO", 1) }
	aTok, bTok, cTok := tok(aID, "Super Admin"), tok(bID, "Super Admin"), tok(cID, "Cashier")

	const base = "/api/v1/admin/security/notify/"
	call := func(h http.HandlerFunc, method, path, token string, body interface{}) (int, map[string]interface{}) {
		rec := doRequest(t, apiMiddleware(h), method, base+path, token, body)
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	if code, _ := call(handleNotifyMatrix, http.MethodGet, "matrix", cTok, nil); code != http.StatusForbidden && code != http.StatusUnauthorized {
		t.Fatalf("a cashier must be refused at the door, got %d", code)
	}
	code, out := call(handleNotifyMatrix, http.MethodGet, "matrix", aTok, nil)
	if code != http.StatusOK {
		t.Fatalf("matrix: %d %v", code, out)
	}
	cur := out["current"].(map[string]interface{})
	rows := cur["entries"].([]interface{})
	for _, r := range rows {
		row := r.(map[string]interface{})
		if row["needs_counsel"] != true {
			t.Fatalf("seed row %v must be flagged for counsel", row["key"])
		}
		if row["key"] == "dpdp" && row["target_hours"].(float64) != 0 {
			t.Fatal("no DPDP time limit may be stated without a recorded source")
		}
	}
	v1 := int(cur["version"].(float64))

	// An edit is always a new version.
	if code, _ := call(handleNotifyMatrixSave, http.MethodPost, "matrix", aTok, map[string]interface{}{"entries": rows, "note": "T49125 first"}); code != http.StatusOK {
		t.Fatalf("save matrix: %d", code)
	}
	_, out = call(handleNotifyMatrix, http.MethodGet, "matrix", aTok, nil)
	v2 := int(out["current"].(map[string]interface{})["version"].(float64))
	if v2 != v1+1 {
		t.Fatalf("version %d then %d, want a new version", v1, v2)
	}

	// Open an incident; the decision carries the version it was opened under.
	code, out = call(handleNotifyIncidentOpen, http.MethodPost, "incidents", aTok, map[string]interface{}{
		"title": "T49125 lost laptop", "discovered_at": time.Now().Add(-time.Hour).UTC(), "keys": []string{"cert-in"}})
	if code != http.StatusOK {
		t.Fatalf("open: %d %v", code, out)
	}
	rows2 := append([]interface{}{}, rows...)
	rows2[0].(map[string]interface{})["target_hours"] = 24
	if code, _ := call(handleNotifyMatrixSave, http.MethodPost, "matrix", aTok, map[string]interface{}{"entries": rows2, "note": "T49125 second"}); code != http.StatusOK {
		t.Fatalf("save matrix 2: %d", code)
	}
	_, out = call(handleNotifyDecisions, http.MethodGet, "decisions", aTok, nil)
	var id string
	for _, d := range out["decisions"].([]interface{}) {
		m := d.(map[string]interface{})
		if m["incident_title"] == "T49125 lost laptop" {
			id = m["id"].(string)
			if int(m["matrix_version"].(float64)) != v2 || m["target_at"] == nil {
				t.Fatalf("decision must keep version %d and have a target: %v", v2, m)
			}
			if m["row"].(map[string]interface{})["target_hours"].(float64) != 6 {
				t.Fatalf("a later matrix version changed an old decision: %v", m["row"])
			}
		}
	}
	if id == "" {
		t.Fatal("decision not listed")
	}

	decide := map[string]interface{}{"id": id, "decision": "notify", "reason": "Counsel read the facts", "evidence_ref": "DOC-1", "approver": bID}
	// The decider cannot name themself approver.
	self := map[string]interface{}{"id": id, "decision": "notify", "reason": "Counsel read the facts", "evidence_ref": "DOC-1", "approver": aID}
	if code, _ := call(handleNotifyDecide, http.MethodPost, "decide", aTok, self); code != http.StatusUnprocessableEntity {
		t.Fatalf("self approver must be refused, got %d", code)
	}
	if code, out := call(handleNotifyDecide, http.MethodPost, "decide", aTok, decide); code != http.StatusOK {
		t.Fatalf("decide: %d %v", code, out)
	}
	decidedAt := func() string {
		_, list := call(handleNotifyDecisions, http.MethodGet, "decisions", aTok, nil)
		for _, d := range list["decisions"].([]interface{}) {
			m := d.(map[string]interface{})
			if m["id"] == id {
				s, _ := m["decided_at"].(string)
				return s
			}
		}
		t.Fatal("decision not listed")
		return ""
	}
	// A Cashier cannot be the approver: the review door is admin only.
	cashier := map[string]interface{}{"id": id, "decision": "notify", "reason": "Counsel read the facts", "evidence_ref": "DOC-1", "approver": cID}
	if code, _ := call(handleNotifyDecide, http.MethodPost, "decide", aTok, cashier); code != http.StatusUnprocessableEntity {
		t.Fatalf("a Cashier approver must be refused, got %d", code)
	}
	// The decider cannot approve; the named approver can.
	if code, _ := call(handleNotifyReview, http.MethodPost, "review", aTok, map[string]interface{}{"id": id, "approve": true, "decided_at": decidedAt()}); code != http.StatusUnprocessableEntity {
		t.Fatalf("the decider approving must be refused, got %d", code)
	}
	// A new decision replaced the one the approver read: the old read cannot close it.
	stale := decidedAt()
	replaced := map[string]interface{}{"id": id, "decision": "do_not_notify", "reason": "Counsel changed the advice", "evidence_ref": "DOC-2", "approver": bID}
	if code, out := call(handleNotifyDecide, http.MethodPost, "decide", aTok, replaced); code != http.StatusOK {
		t.Fatalf("re-decide: %d %v", code, out)
	}
	if code, _ := call(handleNotifyReview, http.MethodPost, "review", bTok, map[string]interface{}{"id": id, "approve": true, "decided_at": stale}); code != http.StatusUnprocessableEntity {
		t.Fatalf("closing a decision the approver never read must be refused, got %d", code)
	}
	if code, _ := call(handleNotifyReview, http.MethodPost, "review", bTok, map[string]interface{}{"id": id, "approve": true}); code != http.StatusUnprocessableEntity {
		t.Fatalf("a review that names no decision must be refused, got %d", code)
	}
	if code, out := call(handleNotifyReview, http.MethodPost, "review", bTok, map[string]interface{}{"id": id, "approve": true, "decided_at": decidedAt()}); code != http.StatusOK {
		t.Fatalf("approve: %d %v", code, out)
	}
	// Closed means closed.
	if code, _ := call(handleNotifyDecide, http.MethodPost, "decide", aTok, decide); code != http.StatusUnprocessableEntity {
		t.Fatalf("a closed decision must not change, got %d", code)
	}
	var status string
	db.DB.QueryRow(`SELECT status FROM tenant_default.notification_decisions WHERE id::text = $1`, id).Scan(&status)
	if status != "Closed" {
		t.Fatalf("status %q, want Closed", status)
	}
}

// 49.11.6 through the real door: saving the destination needs the admin's own
// password, a cashier is refused, and the status never carries the secret.
func TestStage49116ExportSaveNeedsPasswordAndHidesSecret(t *testing.T) {
	db.InitDB(testConnStr())
	aID, aPW, aClean := seedSecurityOpsUser(t, "Super Admin")
	defer aClean()
	cID, _, cClean := seedSecurityOpsUser(t, "Cashier")
	defer cClean()
	defer db.DB.Exec(`DELETE FROM tenant_default.security_export_config`)
	engines.ResetLiveUserStateCache()
	aTok := engines.SignToken(aID, aID, "Super Admin", "default", "HO", 1)
	cTok := engines.SignToken(cID, cID, "Cashier", "default", "HO", 1)
	const path = "/api/v1/admin/security/export"
	save := apiMiddleware(handleSecurityExportSave)
	body := func(pw string) map[string]interface{} {
		return map[string]interface{}{"url": "https://1.1.1.1/ingest", "secret": "0123456789abcdefXYZ", "enabled": false, "password": pw}
	}
	if rec := doRequest(t, save, http.MethodPost, path, cTok, body(aPW)); rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
		t.Fatalf("cashier save: %d", rec.Code)
	}
	if rec := doRequest(t, save, http.MethodPost, path, aTok, body("wrong-password")); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong password must refuse: %d", rec.Code)
	}
	if rec := doRequest(t, save, http.MethodPost, path, aTok, body(aPW)); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	} else if bytesContains(rec.Body.String(), "0123456789abcdefXYZ") {
		t.Fatal("the secret came back in the response")
	}
	if rec := doRequest(t, apiMiddleware(handleSecurityExportStatus), http.MethodGet, path, aTok, nil); rec.Code != http.StatusOK || bytesContains(rec.Body.String(), "0123456789abcdefXYZ") {
		t.Fatalf("status: %d", rec.Code)
	}
}

func bytesContains(s, sub string) bool { return strings.Contains(s, sub) }
