package engines

import (
	"context"
	"custom_erp/db"
	"testing"
)

func linkedUserStatus(t *testing.T, id string) string {
	t.Helper()
	var s string
	db.DB.QueryRow(`SELECT status FROM tenant_default.users WHERE id = $1`, id).Scan(&s)
	return s
}

// Stage 49.2.1: saving an Active Employee must not undo an administrator's
// disable of the linked user.
func TestStage4921EmployeeSaveDoesNotReactivateDisabledUser(t *testing.T) {
	db.InitDB(testConnStr())
	const id = "t4921-emp-a"
	seedLifecycleUser(t, id, RoleCashier, "Inactive") // disabled by an administrator
	err := SyncEmployeeAccessLink(context.Background(), "default", EmployeeAccessChange{
		UserID: id, Status: "Active", PriorStatus: "Active", Actor: "hr",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := linkedUserStatus(t, id); got != "Inactive" {
		t.Fatalf("an Active Employee save re-activated the disabled user: %s", got)
	}
}
