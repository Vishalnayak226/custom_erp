package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// Stage 130.4 (73.D.2, user decision 2026-10-22): an employee reads their own
// grievance back through the document API - description and resolution -
// while other roles still see the case without its content.
func TestStage130EmployeeReadsOwnGrievance(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() { db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T130G-%'") }
	cleanup()
	defer cleanup()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, string(b), status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert("T130G-EMP-A", "Employee", "Active", map[string]interface{}{"code": "T130G-EMP-A", "name": "A", "user_id": "t130g-a", "status": "Active"})
	insert("T130G-EMP-B", "Employee", "Active", map[string]interface{}{"code": "T130G-EMP-B", "name": "B", "user_id": "t130g-b", "status": "Active"})
	insert("T130G-GRV-A", "Grievance", "Resolved", map[string]interface{}{"employee_id": "T130G-EMP-A", "category": "Other",
		"description": "secret A", "resolution_notes": "settled", "status": "Resolved"})

	get := func(role, user string) (int, map[string]interface{}) {
		r := httptest.NewRequest("GET", "/api/v1/doc/Grievance/T130G-GRV-A", nil)
		r.SetPathValue("doctype", "Grievance")
		r.SetPathValue("id", "T130G-GRV-A")
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-Role", role)
		r.Header.Set("Resolved-User-ID", user)
		r.Header.Set("Resolved-Location", "HO")
		w := httptest.NewRecorder()
		handleGenericDoc(w, r)
		var out map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	if code, doc := get(engines.RoleCashier, "t130g-a"); code != 200 || doc["description"] != "secret A" || doc["resolution_notes"] != "settled" {
		t.Fatalf("own grievance: %d %v", code, doc)
	}
	if code, doc := get(engines.RoleCashier, "t130g-b"); code == 200 && doc["description"] != nil {
		t.Fatalf("another employee read the grievance: %v", doc)
	}
	if code, doc := get(engines.RoleStoreManager, "manager1"); code == 200 && doc["description"] != nil {
		t.Fatalf("store manager read the grievance's content: %v", doc)
	}
}

// Stage 130.9 (72.D e): an edit records its editor and reason - on the record,
// in one signed audit event, and in tenant_default's field-change trigger.
func TestStage130AuditRecordsEditorAndReason(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const id = "T130-AUD-BRAND"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
	}
	cleanup()
	defer cleanup()
	// audit_logs is append-only: count only this run's rows.
	var since int64
	_ = db.DB.QueryRow("SELECT COALESCE(MAX(seq), 0) FROM " + schema + ".audit_logs").Scan(&since)
	save := func(path, user string, body map[string]interface{}) int {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
		r.SetPathValue("doctype", "Brand")
		if strings.HasSuffix(path, id) {
			r.SetPathValue("id", id)
		}
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
		r.Header.Set("Resolved-User-ID", user)
		r.Header.Set("Resolved-Username", user)
		r.Header.Set("Resolved-Location", "HO")
		w := httptest.NewRecorder()
		handleGenericDoc(w, r)
		if w.Code >= 300 {
			t.Logf("%s: %s", path, w.Body.String())
		}
		return w.Code
	}
	if code := save("/api/v1/doc/Brand", "admin", map[string]interface{}{"code": id, "name": "Red", "status": "Active"}); code >= 300 {
		t.Fatalf("create: %d", code)
	}
	if code := save("/api/v1/doc/Brand/"+id, "manager1", map[string]interface{}{"code": id, "name": "Crimson", "status": "Active", "edit_reason": "brand name"}); code >= 300 {
		t.Fatalf("edit: %d", code)
	}
	var by, reason string
	_ = db.DB.QueryRow("SELECT COALESCE(updated_by, ''), COALESCE(edit_reason, '') FROM "+schema+".documents WHERE id = $1", id).Scan(&by, &reason)
	if by != "manager1" || reason != "brand name" {
		t.Fatalf("record: updated_by %q, edit_reason %q", by, reason)
	}
	var events, triggerRows int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".audit_logs WHERE seq > $1 AND action = 'EDIT_Brand' AND user_id = 'manager1' AND details LIKE '%"+id+"%name%reason: brand name%'", since).Scan(&events)
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".audit_logs WHERE seq > $1 AND action = 'UPDATE_Brand' AND user_id = 'manager1' AND details LIKE '%Crimson%"+id+" (reason: brand name)%'", since).Scan(&triggerRows)
	if events != 1 || triggerRows < 1 {
		t.Fatalf("audit: %d signed edit events, %d trigger rows naming the editor", events, triggerRows)
	}
}
