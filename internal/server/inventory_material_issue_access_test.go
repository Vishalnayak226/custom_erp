package server

import (
	"custom_erp/db"
	"net/http/httptest"
	"testing"
)

// TestMaterialIssueOnlyTheStoreIssues (162.2): Cashier may ask for material
// but not issue it; Store Manager and HR/Admin pass the permission check and
// reach the engine, which then refuses the unknown request id (422, not 403).
// Store Manager passes on 130.15's seed as it stands (MaterialReturn create,
// MaterialIssue update off): no migration rewrites its grant (49.3.7).
func TestMaterialIssueOnlyTheStoreIssues(t *testing.T) {
	db.InitDB(testConnStr())
	if ok, err := checkPermission("default", "Store Manager", "MaterialIssue", "update"); err != nil || ok {
		t.Fatalf("Store Manager holds update on MaterialIssue (%v %v): this test needs 130.15's seed as shipped", ok, err)
	}
	for role, want := range map[string]int{"Cashier": 403, "Store Manager": 422, "HR/Admin": 422} {
		r := httptest.NewRequest("POST", "/api/v1/inventory/material-requests/MI-162-2-NONE/issue", nil)
		r.SetPathValue("id", "MI-162-2-NONE")
		r.Header.Set("Resolved-Tenant-ID", "default")
		r.Header.Set("Resolved-Role", role)
		r.Header.Set("Resolved-User-ID", "t162_2")
		w := httptest.NewRecorder()
		handleIssueMaterial(w, r)
		if w.Code != want {
			t.Errorf("%s: %d %s, want %d", role, w.Code, w.Body.String(), want)
		}
	}
	// Cashier still asks: the create right is unchanged.
	if ok, err := checkPermission("default", "Cashier", "MaterialIssue", "create"); err != nil || !ok {
		t.Errorf("Cashier lost the right to ask for material: %v %v", ok, err)
	}
}
