package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Stage 73.D.9: a scheduled report belongs to its creator - another store
// manager can neither list, open nor change it (its recipients and webhook URL
// with its token), its creator and a Super Admin can.
func TestStage73D9ScheduleIsItsCreatorsOnly(t *testing.T) {
	db.InitDB(testConnStr())
	owner, cleanupA := seedStage47User(t, "Store Manager", "HO")
	defer cleanupA()
	other, cleanupB := seedStage47User(t, "Store Manager", "HO")
	defer cleanupB()
	const id = "T73D9-SCHED"
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'ScheduledReport', '{"report_id":"gl-journal","frequency":"Daily","recipients":"boss@example.com","webhook_url":"https://hooks.example.com/T/secret-token"}', 'Active', $2)`, id, owner); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	handler := mux.ServeHTTP
	tokenOwner := stage47Token(owner, "Store Manager", "HO")
	tokenOther := stage47Token(other, "Store Manager", "HO")
	tokenAdmin := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "", currentCredentialVersion("admin"))

	listHas := func(token string) bool {
		rec := doRequest(t, handler, http.MethodGet, "/api/v1/doc/ScheduledReport?recipients=boss@example.com", token, nil)
		var rows []map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &rows)
		for _, row := range rows {
			if row["id"] == id {
				return true
			}
		}
		return false
	}
	if !listHas(tokenOwner) {
		t.Error("the creator does not see their own schedule in the list")
	}
	if listHas(tokenOther) {
		t.Error("another store manager sees the schedule in the list")
	}
	if !listHas(tokenAdmin) {
		t.Error("a Super Admin does not see the schedule in the list")
	}

	if rec := doRequest(t, handler, http.MethodGet, "/api/v1/doc/ScheduledReport/"+id, tokenOther, nil); rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "secret-token") {
		t.Errorf("another store manager opened the schedule: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, handler, http.MethodGet, "/api/v1/doc/ScheduledReport/"+id, tokenOwner, nil); rec.Code != http.StatusOK {
		t.Errorf("the creator could not open their schedule: %d %s", rec.Code, rec.Body.String())
	}
	edit := map[string]interface{}{"report_id": "gl-journal", "frequency": "Daily", "requested_role": "Store Manager", "recipients": "attacker@example.com", "status": "Active"}
	if rec := doRequest(t, handler, http.MethodPost, "/api/v1/doc/ScheduledReport/"+id, tokenOther, edit); rec.Code != http.StatusForbidden {
		t.Errorf("another store manager changed the schedule: %d %s", rec.Code, rec.Body.String())
	}
	var recipients string
	db.DB.QueryRow(`SELECT data->>'recipients' FROM tenant_default.documents WHERE id = $1`, id).Scan(&recipients)
	if recipients != "boss@example.com" {
		t.Errorf("recipients changed to %q", recipients)
	}
}
