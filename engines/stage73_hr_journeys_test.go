package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func insertHRJourneyDoc(t *testing.T, schema, id, doctype, status string, data map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, string(b), status); err != nil {
		t.Fatalf("insert %s %s: %v", doctype, id, err)
	}
}

func hrJourneyCode(t *testing.T, err error) string {
	t.Helper()
	if verr, ok := err.(*ValidationError); ok {
		return verr.Code
	}
	t.Fatalf("expected a coded ValidationError, got %v", err)
	return ""
}

// Stage 55.14: a step is ticked with who and when stamped by the server, the
// checklist completes when every step is done, and nobody outside the
// checklist's scope can tick it.
func TestOnboardingStepIsStampedAndScoped(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T5514J-%'")
	}
	cleanup()
	defer cleanup()
	insertHRJourneyDoc(t, schema, "T5514J-EMP-A", "Employee", "Active", map[string]interface{}{"code": "T5514J-EMP-A", "name": "Asha", "user_id": "t5514j-asha", "status": "Active"})
	insertHRJourneyDoc(t, schema, "T5514J-EMP-B", "Employee", "Active", map[string]interface{}{"code": "T5514J-EMP-B", "name": "Bala", "user_id": "t5514j-bala", "status": "Active"})
	insertHRJourneyDoc(t, schema, "T5514J-ONB-A", "OnboardingChecklist", "In Progress", map[string]interface{}{
		"code": "T5514J-ONB-A", "employee_id": "T5514J-EMP-A", "checklist_type": "Onboarding", "status": "In Progress",
		"items": `[{"task":"Offer letter signed","done":false,"note":"kept"},{"task":"ID proof collected","done":false}]`})
	insertHRJourneyDoc(t, schema, "T5514J-ONB-BAD", "OnboardingChecklist", "In Progress", map[string]interface{}{
		"code": "T5514J-ONB-BAD", "employee_id": "T5514J-EMP-A", "checklist_type": "Onboarding", "status": "In Progress",
		"items": "laptop, badge"})

	manager := SessionScope{Role: RoleStoreManager, UserID: "manager1"}
	p, err := SetOnboardingStep(tenantID, "T5514J-ONB-A", 0, true, "manager1", manager)
	if err != nil {
		t.Fatalf("tick step 1: %v", err)
	}
	if p.Done != 1 || p.Total != 2 || p.Status != "In Progress" {
		t.Fatalf("after one tick: %+v", p)
	}
	if p.Items[0]["done_by"] != "manager1" || p.Items[0]["done_at"] == nil || p.Items[0]["note"] != "kept" {
		t.Fatalf("step 1 not stamped, or a key was lost: %+v", p.Items[0])
	}
	if p, err = SetOnboardingStep(tenantID, "T5514J-ONB-A", 1, true, "manager1", manager); err != nil || p.Status != "Completed" {
		t.Fatalf("second tick should complete the checklist: %+v, %v", p, err)
	}
	var status, itemsJSON string
	db.DB.QueryRow("SELECT status, data->>'items' FROM "+schema+".documents WHERE id = 'T5514J-ONB-A'").Scan(&status, &itemsJSON)
	if status != "Completed" {
		t.Fatalf("stored status %q", status)
	}
	if steps, err := ParseOnboardingSteps(itemsJSON); err != nil || len(steps) != 2 {
		t.Fatalf("stored items are not a step list: %q (%v)", itemsJSON, err)
	}
	// Unticking clears the stamp and reopens the checklist.
	if p, err = SetOnboardingStep(tenantID, "T5514J-ONB-A", 0, false, "manager1", manager); err != nil || p.Status != "In Progress" || p.Items[0]["done_by"] != nil {
		t.Fatalf("untick: %+v, %v", p, err)
	}

	// The employee's own checklist is in their scope; another employee's is not.
	if _, err := SetOnboardingStep(tenantID, "T5514J-ONB-A", 0, true, "t5514j-asha", SessionScope{Role: "Employee Self-Service", UserID: "t5514j-asha"}); err != nil {
		t.Fatalf("own checklist: %v", err)
	}
	_, err = SetOnboardingStep(tenantID, "T5514J-ONB-A", 1, true, "t5514j-bala", SessionScope{Role: "Employee Self-Service", UserID: "t5514j-bala"})
	if code := hrJourneyCode(t, err); code != "GLOBAL-0011" {
		t.Fatalf("another employee ticked the checklist: %s", code)
	}
	// A Cashier login with no Employee record fails closed.
	_, err = SetOnboardingStep(tenantID, "T5514J-ONB-A", 1, true, "cashier1", SessionScope{Role: RoleCashier, UserID: "t5514j-nobody"})
	if code := hrJourneyCode(t, err); code != "GLOBAL-0011" {
		t.Fatalf("an unlinked login ticked the checklist: %s", code)
	}

	_, err = SetOnboardingStep(tenantID, "T5514J-ONB-A", 5, true, "manager1", manager)
	if code := hrJourneyCode(t, err); code != "HR-0533" {
		t.Fatalf("step out of range: %s", code)
	}
	_, err = SetOnboardingStep(tenantID, "T5514J-NOPE", 0, true, "manager1", manager)
	if code := hrJourneyCode(t, err); code != "HR-0531" {
		t.Fatalf("missing checklist: %s", code)
	}
	_, err = SetOnboardingStep(tenantID, "T5514J-ONB-BAD", 0, true, "manager1", manager)
	if code := hrJourneyCode(t, err); code != "HR-0532" {
		t.Fatalf("unreadable items: %s", code)
	}
}

// Stage 55.14: the grievance visibility statement is the enforced policy:
// content to Super Admin only, the case list also to Store Manager, everyone
// else their own cases without the content, decided by the approval rule.
func TestGrievanceAccessMatchesThePolicy(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	admin, err := GetGrievanceAccess(tenantID, RoleSuperAdmin)
	if err != nil {
		t.Fatalf("admin access: %v", err)
	}
	// Stage 125: a client's Admin reads grievances as Super Admin does.
	if len(admin.ContentReaders) != 2 || admin.ContentReaders[0] != RoleSuperAdmin || admin.ContentReaders[1] != RoleAdmin {
		t.Fatalf("content readers %v", admin.ContentReaders)
	}
	if clientAdmin, _ := GetGrievanceAccess(tenantID, RoleAdmin); clientAdmin == nil || !clientAdmin.You.ReadContent || !clientAdmin.You.AllCases {
		t.Fatalf("client Admin: %+v", clientAdmin)
	}
	if !containsRole(admin.CaseListRoles, RoleStoreManager) || containsRole(admin.CaseListRoles, RoleCashier) {
		t.Fatalf("case list roles %v", admin.CaseListRoles)
	}
	if admin.ReviewerRole != RoleSuperAdmin {
		t.Fatalf("reviewer %q (the seeded Grievance rule routes to HR/Admin)", admin.ReviewerRole)
	}
	if y := admin.You; !y.AllCases || !y.ReadContent || !y.WriteNotes || !y.Decide {
		t.Fatalf("super admin: %+v", y)
	}
	manager, _ := GetGrievanceAccess(tenantID, RoleStoreManager)
	if y := manager.You; !y.AllCases || y.ReadContent || y.WriteNotes || y.Decide {
		t.Fatalf("store manager: %+v", y)
	}
	for _, role := range []string{RoleCashier, "Employee Self-Service"} {
		a, _ := GetGrievanceAccess(tenantID, role)
		if y := a.You; y.AllCases || y.ReadContent || y.WriteNotes || y.Decide {
			t.Fatalf("%s: %+v", role, y)
		}
	}
}
