package engines

import (
	"context"
	"custom_erp/db"
	"testing"
	"time"
)

func linkedAuditCount(t *testing.T, like string) int {
	t.Helper()
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE details LIKE $1`, like).Scan(&n)
	return n
}

func TestStage4921EmployeeReturnRestoresAndLeaverDisables(t *testing.T) {
	db.InitDB(testConnStr())
	const id = "t4921-emp-b"
	seedLifecycleUser(t, id, RoleCashier, "Active")
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE details LIKE $1`, "%"+id+"%") })
	ctx := context.Background()

	// Status Inactive: disabled at once, one audit row.
	if err := SyncEmployeeAccessLink(ctx, "default", EmployeeAccessChange{UserID: id, Status: "Inactive", PriorStatus: "Active", Actor: "hr"}); err != nil {
		t.Fatal(err)
	}
	if got := linkedUserStatus(t, id); got != "Inactive" {
		t.Fatalf("leaver not disabled: %s", got)
	}
	if n := linkedAuditCount(t, "Set user "+id+" status to Inactive - Employee left%"); n != 1 {
		t.Fatalf("audit rows = %d", n)
	}
	// Saving the leaver again changes nothing and adds no second row.
	if err := SyncEmployeeAccessLink(ctx, "default", EmployeeAccessChange{UserID: id, Status: "Inactive", PriorStatus: "Inactive"}); err != nil {
		t.Fatal(err)
	}
	if n := linkedAuditCount(t, "Set user "+id+" status to Inactive - Employee left%"); n != 1 {
		t.Fatalf("a repeat save wrote another row: %d", n)
	}
	// A real return restores the account.
	if err := SyncEmployeeAccessLink(ctx, "default", EmployeeAccessChange{UserID: id, Status: "Active", PriorStatus: "Inactive"}); err != nil {
		t.Fatal(err)
	}
	if got := linkedUserStatus(t, id); got != "Active" {
		t.Fatalf("returning employee not restored: %s", got)
	}
	// An exit date that has come disables; one still ahead does not.
	future := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	past := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	_ = SyncEmployeeAccessLink(ctx, "default", EmployeeAccessChange{UserID: id, Status: "Active", ExitDate: future, PriorStatus: "Active"})
	if got := linkedUserStatus(t, id); got != "Active" {
		t.Fatalf("a future exit date disabled the user: %s", got)
	}
	_ = SyncEmployeeAccessLink(ctx, "default", EmployeeAccessChange{UserID: id, Status: "Active", ExitDate: past, PriorStatus: "Active"})
	if got := linkedUserStatus(t, id); got != "Inactive" {
		t.Fatalf("a past exit date did not disable the user: %s", got)
	}
}

func TestStage4921LeaverNeverDisablesSuperAdmin(t *testing.T) {
	db.InitDB(testConnStr())
	const id = "t4921-emp-sa"
	seedLifecycleUser(t, id, RoleSuperAdmin, "Active")
	err := SyncEmployeeAccessLink(context.Background(), "default", EmployeeAccessChange{UserID: id, Status: "Exited", PriorStatus: "Active"})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if got := linkedUserStatus(t, id); got != "Active" {
		t.Fatalf("a Super Admin was disabled by HR data: %s", got)
	}
}

func TestStage4921JoinerMoverLeaverApproval(t *testing.T) {
	db.InitDB(testConnStr())
	seedLifecycleUser(t, "t4921-maker", RoleSuperAdmin, "Active")
	seedLifecycleUser(t, "t4921-checker", RoleSuperAdmin, "Active")
	seedLifecycleUser(t, "t4921-new", RoleCashier, "Inactive")
	seedLifecycleUser(t, "t4921-move", RoleCashier, "Active")
	seedLifecycleUser(t, "t4921-left", RoleCashier, "Active")
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'AccessChangeRequest' AND data->>'target_user_id' LIKE 't4921-%'`)
		db.DB.Exec(`DELETE FROM tenant_default.system_settings WHERE key = $1`, SettingIdentityChangeApproval)
	})

	// Off by default: no request is filed for a leaver.
	if IdentityChangeApprovalOn("default") {
		t.Fatal("the setting must be Off by default")
	}
	_ = SyncEmployeeAccessLink(context.Background(), "default", EmployeeAccessChange{UserID: "t4921-left", Status: "Inactive", PriorStatus: "Active", Actor: "t4921-maker"})
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'AccessChangeRequest' AND data->>'target_user_id' = 't4921-left'`).Scan(&n)
	if n != 0 {
		t.Fatalf("a request was filed with the setting Off: %d", n)
	}
	db.DB.Exec(`UPDATE tenant_default.users SET status = 'Active' WHERE id = 't4921-left'`)

	if err := SetSetting("default", SettingIdentityChangeApproval, "true", "test"); err != nil {
		t.Fatal(err)
	}

	// Joiner: stays Inactive until a different Super Admin approves.
	jid, err := RequestAccessChange("default", AccessChangeJoiner, "t4921-new", "new Cashier account", "", "t4921-maker", RoleSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval("default", AccessChangeRequestDoctype, jid, "t4921-maker", RoleSuperAdmin, "", "Approved", ""); err == nil {
		t.Fatal("the requester decided their own request")
	}
	if got := linkedUserStatus(t, "t4921-new"); got != "Inactive" {
		t.Fatalf("joiner active before approval: %s", got)
	}
	if err := DecideApproval("default", AccessChangeRequestDoctype, jid, "t4921-checker", RoleSuperAdmin, "", "Approved", ""); err != nil {
		t.Fatal(err)
	}
	if got := linkedUserStatus(t, "t4921-new"); got != "Active" {
		t.Fatalf("approved joiner not activated: %s", got)
	}

	// Mover: the store changes only on approval.
	mid, err := RequestAccessChange("default", AccessChangeMover, "t4921-move", "store HO to ST9", "ST9", "t4921-maker", RoleSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	loc := func() (s string) {
		db.DB.QueryRow(`SELECT location_code FROM tenant_default.users WHERE id = 't4921-move'`).Scan(&s)
		return
	}
	if loc() != "HO" {
		t.Fatalf("store moved before approval: %s", loc())
	}
	if err := DecideApproval("default", AccessChangeRequestDoctype, mid, "t4921-checker", RoleSuperAdmin, "", "Rejected", "no"); err != nil {
		t.Fatal(err)
	}
	if loc() != "HO" {
		t.Fatalf("a rejected move changed the store: %s", loc())
	}

	// Leaver: disabled at once; the request only records it, and rejecting it
	// does not turn the account back on.
	if err := SyncEmployeeAccessLink(context.Background(), "default", EmployeeAccessChange{UserID: "t4921-left", Status: "Exited", PriorStatus: "Active", Actor: "t4921-maker"}); err != nil {
		t.Fatal(err)
	}
	if got := linkedUserStatus(t, "t4921-left"); got != "Inactive" {
		t.Fatalf("leaver delayed: %s", got)
	}
	var lid, lst string
	db.DB.QueryRow(`SELECT id FROM tenant_default.documents WHERE doctype = 'AccessChangeRequest' AND data->>'target_user_id' = 't4921-left'`).Scan(&lid)
	if lid == "" {
		t.Fatal("no review request for the leaver")
	}
	if err := DecideApproval("default", AccessChangeRequestDoctype, lid, "t4921-checker", RoleSuperAdmin, "", "Rejected", "check"); err != nil {
		t.Fatal(err)
	}
	if got := linkedUserStatus(t, "t4921-left"); got != "Inactive" {
		t.Fatalf("a rejected review reinstated the leaver: %s", got)
	}
	db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = $1`, lid).Scan(&lst)
	if lst != "Rejected" {
		t.Fatalf("review status %s", lst)
	}
}

func TestStage4921AccessReview(t *testing.T) {
	db.InitDB(testConnStr())
	seedLifecycleUser(t, "t4921-rv-a", RoleCashier, "Active")
	seedLifecycleUser(t, "t4921-rv-sa", RoleSuperAdmin, "Active")
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.access_reviews WHERE user_id LIKE 't4921-rv-%'`)
		db.DB.Exec(`DELETE FROM tenant_default.system_settings WHERE key = $1`, SettingAccessReviewDays)
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE details LIKE '%t4921-rv-%'`)
	})
	ctx := context.Background()
	find := func(l *AccessReviewList, id string) *AccessReviewRow {
		for i := range l.Rows {
			if l.Rows[i].ID == id {
				return &l.Rows[i]
			}
		}
		return nil
	}

	// No schedule: nobody is overdue.
	l, err := ListAccessReviews("default")
	if err != nil || find(l, "t4921-rv-a") == nil || find(l, "t4921-rv-a").Overdue {
		t.Fatalf("no schedule: %v", err)
	}
	if err := SetSetting("default", SettingAccessReviewDays, "90", "test"); err != nil {
		t.Fatal(err)
	}
	l, _ = ListAccessReviews("default")
	if r := find(l, "t4921-rv-a"); r == nil || !r.Overdue || l.Overdue == 0 {
		t.Fatalf("an unreviewed user is overdue on a schedule: %+v", r)
	}
	// Confirm records who/when/what and clears overdue.
	if err := RecordAccessReview(ctx, "default", "t4921-rv-a", AccessReviewConfirmed, "", "t4921-rv-sa", "t4921-rv-sa"); err != nil {
		t.Fatal(err)
	}
	l, _ = ListAccessReviews("default")
	r := find(l, "t4921-rv-a")
	if r.Overdue || r.LastDecision != AccessReviewConfirmed || r.LastBy != "t4921-rv-sa" || r.LastAt == nil {
		t.Fatalf("after confirm: %+v", r)
	}
	var role string
	db.DB.QueryRow(`SELECT role FROM tenant_default.access_reviews WHERE user_id = 't4921-rv-a'`).Scan(&role)
	if role != RoleCashier {
		t.Fatalf("the review must keep the role it looked at: %q", role)
	}
	// Aged past the period it is overdue again.
	db.DB.Exec(`UPDATE tenant_default.access_reviews SET decided_at = NOW() - INTERVAL '91 days' WHERE user_id = 't4921-rv-a'`)
	l, _ = ListAccessReviews("default")
	if !find(l, "t4921-rv-a").Overdue {
		t.Fatal("a 91-day-old review is overdue on a 90-day schedule")
	}
	// Guards: a reason for a revoke, never yourself, never a Super Admin.
	if err := RecordAccessReview(ctx, "default", "t4921-rv-a", AccessReviewRevoked, " ", "t4921-rv-sa", "t4921-rv-sa"); err == nil {
		t.Fatal("a revoke needs a reason")
	}
	if err := RecordAccessReview(ctx, "default", "t4921-rv-sa", AccessReviewRevoked, "x", "t4921-rv-sa", "t4921-rv-sa"); err == nil {
		t.Fatal("revoked own access")
	}
	if err := RecordAccessReview(ctx, "default", "t4921-rv-sa", AccessReviewRevoked, "x", "someone-else", "someone-else"); err == nil {
		t.Fatal("revoked a Super Admin")
	}
	// Revoke disables with an audit row and leaves the list.
	if err := RecordAccessReview(ctx, "default", "t4921-rv-a", AccessReviewRevoked, "left the team", "t4921-rv-sa", "t4921-rv-sa"); err != nil {
		t.Fatal(err)
	}
	if got := linkedUserStatus(t, "t4921-rv-a"); got != "Inactive" {
		t.Fatalf("revoked user: %s", got)
	}
	if linkedAuditCount(t, "Set user t4921-rv-a status to Inactive - access revoked in review%") != 1 {
		t.Fatal("no audit row for the revoke")
	}
	l, _ = ListAccessReviews("default")
	if find(l, "t4921-rv-a") != nil {
		t.Fatal("a revoked user is still on the list")
	}
}
