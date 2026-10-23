package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// Stage 55.14: the approvals inbox follows the document API's rules - another
// employee's pending grievance does not reach a role without HR's self-scope
// exemption, and its content reaches only a role that may read it.
func TestPendingApprovalsAreScopedLikeTheDocumentAPI(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() { db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T5514P-%'") }
	cleanup()
	defer cleanup()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, string(b), status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert("T5514P-EMP-A", "Employee", "Active", map[string]interface{}{"code": "T5514P-EMP-A", "name": "A", "user_id": "t5514p-a", "status": "Active"})
	insert("T5514P-EMP-B", "Employee", "Active", map[string]interface{}{"code": "T5514P-EMP-B", "name": "B", "user_id": "t5514p-b", "status": "Active"})
	insert("T5514P-GRV-A", "Grievance", "Pending Approval", map[string]interface{}{"employee_id": "T5514P-EMP-A", "category": "Other", "description": "secret A", "status": "Pending Approval"})

	pending := func() []map[string]interface{} {
		return []map[string]interface{}{{"id": "T5514P-GRV-A", "doctype": "Grievance", "employee_id": "T5514P-EMP-A", "category": "Other", "description": "secret A"}}
	}
	visible := func(role, user string) []map[string]interface{} {
		r := httptest.NewRequest("GET", "/api/v1/approval/pending", nil)
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-Role", role)
		r.Header.Set("Resolved-User-ID", user)
		r.Header.Set("Resolved-Location", "HO")
		out, err := scopePendingApprovals(r, pending())
		if err != nil {
			t.Fatalf("%s/%s: %v", role, user, err)
		}
		return out
	}
	if got := visible(engines.RoleCashier, "t5514p-b"); len(got) != 0 {
		t.Fatalf("another employee saw the grievance: %v", got)
	}
	if got := visible(engines.RoleCashier, "t5514p-nobody"); len(got) != 0 {
		t.Fatalf("an unlinked login saw the grievance: %v", got)
	}
	// 73.D.2 (user decision 2026-10-22): an employee reads their own back.
	own := visible("Employee Self-Service", "t5514p-a")
	if len(own) != 1 || own[0]["description"] != "secret A" {
		t.Fatalf("own grievance should show, with its content: %v", own)
	}
	manager := visible(engines.RoleStoreManager, "manager1")
	if len(manager) != 1 || manager[0]["description"] != nil {
		t.Fatalf("store manager sees the case, not its content: %v", manager)
	}
	admin := visible(engines.RoleSuperAdmin, "admin")
	if len(admin) != 1 || admin[0]["description"] != "secret A" {
		t.Fatalf("super admin reads the case: %v", admin)
	}
}
