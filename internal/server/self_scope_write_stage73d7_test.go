package server

import (
	"custom_erp/db"
	"net/http"
	"testing"
)

// Stage 73.D.7: an Employee Self-Service user writes only their own leave -
// not one filed under a colleague's code, not an edit of a colleague's, not a
// submit of a colleague's for approval.
func TestStage73D7SelfServiceWritesOnlyItsOwn(t *testing.T) {
	db.InitDB(testConnStr())
	ess, cleanup := seedStage47User(t, "Employee Self-Service", "HO")
	defer cleanup()
	ids := []string{"T73D7-EMP", "T73D7-LEAVE-OTHER", "T73D7-LEAVE-OWN"}
	clean := func() {
		for _, id := range ids {
			db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
		}
	}
	clean()
	defer clean()
	// The role's template grants Operate on Leave (role_templates.go); the
	// shared dev seed has no rows for this role, so the test grants exactly that.
	var had int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.role_permissions WHERE role = 'Employee Self-Service' AND doctype_name = 'Leave'`).Scan(&had)
	if had == 0 {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES ('Employee Self-Service', 'Leave', TRUE, TRUE, TRUE, FALSE)`); err != nil {
			t.Fatalf("grant: %v", err)
		}
		defer db.DB.Exec(`DELETE FROM tenant_default.role_permissions WHERE role = 'Employee Self-Service' AND doctype_name = 'Leave'`)
	}
	seed := func(id, doctype, data, status string) {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'admin')`, id, doctype, data, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("T73D7-EMP", "Employee", `{"code":"T73D7-EMP","name":"T73D7 Self","user_id":"`+ess+`","location":"HO"}`, "Active")
	seed("T73D7-LEAVE-OTHER", "Leave", `{"employee_id":"T73D7-COLLEAGUE","leave_type":"Casual","from_date":"2031-01-01","to_date":"2031-01-01"}`, "Draft")
	seed("T73D7-LEAVE-OWN", "Leave", `{"employee_id":"T73D7-EMP","leave_type":"Casual","from_date":"2031-01-02","to_date":"2031-01-02"}`, "Draft")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	doc := mux.ServeHTTP
	submit := apiMiddleware(handleSubmitApproval)
	token := stage47Token(ess, "Employee Self-Service", "HO")

	if rec := doRequest(t, doc, http.MethodPost, "/api/v1/doc/Leave", token,
		map[string]interface{}{"employee_id": "T73D7-COLLEAGUE", "leave_type": "Casual", "from_date": "2031-02-01", "to_date": "2031-02-01"}); rec.Code != http.StatusForbidden {
		t.Errorf("leave filed under a colleague's code: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, doc, http.MethodPost, "/api/v1/doc/Leave/T73D7-LEAVE-OTHER", token,
		map[string]interface{}{"employee_id": "T73D7-EMP", "leave_type": "Casual", "from_date": "2031-01-01", "to_date": "2031-01-01"}); rec.Code != http.StatusNotFound { // Stage 49.4.7: "not found", like a read
		t.Errorf("a colleague's leave edited (re-filed under their own code): %d %s", rec.Code, rec.Body.String())
	}
	var stillTheirs string
	db.DB.QueryRow(`SELECT data->>'employee_id' FROM tenant_default.documents WHERE id = 'T73D7-LEAVE-OTHER'`).Scan(&stillTheirs)
	if stillTheirs != "T73D7-COLLEAGUE" {
		t.Errorf("the colleague's leave now belongs to %q", stillTheirs)
	}
	if rec := doRequest(t, submit, http.MethodPost, "/api/v1/approval/submit", token,
		map[string]string{"doctype": "Leave", "document_id": "T73D7-LEAVE-OTHER"}); rec.Code != http.StatusForbidden {
		t.Errorf("a colleague's leave submitted for approval: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, submit, http.MethodPost, "/api/v1/approval/submit", token,
		map[string]string{"doctype": "Leave", "document_id": "T73D7-LEAVE-OWN"}); rec.Code == http.StatusForbidden {
		t.Errorf("their own leave could not be submitted: %d %s", rec.Code, rec.Body.String())
	}
}
