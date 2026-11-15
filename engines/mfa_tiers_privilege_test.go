package engines

import "testing"

// 49.3.6: a role-permission save and its rollback belong to the privilege-change tier.
func TestStage4936PrivilegeChangeIsAnMFATier(t *testing.T) {
	for _, p := range []string{"POST /api/v1/admin/role-permissions", "POST /api/v1/admin/role-permissions/rollback"} {
		found := false
		for _, tier := range MFATiersForAction(p, "admin.roles", false) {
			found = found || tier == MFATierPrivilegeChange
		}
		if !found {
			t.Errorf("%s is not in the privilege_change tier", p)
		}
	}
	for _, tier := range MFATiersForAction("GET /api/v1/admin/role-permissions", "admin.roles", false) {
		if tier == MFATierPrivilegeChange {
			t.Error("reading role permissions must not need step-up")
		}
	}
}
