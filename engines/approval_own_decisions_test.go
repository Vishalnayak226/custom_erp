package engines

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"custom_erp/db"
)

// 47.12.5 (ask 185): above the amount the owner set, an own decision needs a
// written reason (APPROV-0842); below it, or with the rule off, it does not.
// Every own decision lands in the Own approval decisions report with its
// reason and amount.
func TestOwnDecisionReasonAndReport(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE doctype = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV47125%')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV47125%'")
	}
	cleanup()
	defer cleanup()
	withSelfApproval(t, tenantID, SelfApprovalAnyone)
	if err := SetSetting(tenantID, SettingSelfApprovalReasonAbove, "50", "test"); err != nil {
		t.Fatalf("set reason-above: %v", err)
	}
	t.Cleanup(func() { _ = SetSetting(tenantID, SettingSelfApprovalReasonAbove, "0", "test") })

	submit := func(amount int) string {
		t.Helper()
		id, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV47125 "+strconv.Itoa(amount), []JournalVoucherLine{{AccountCode: "5100", Debit: float64(amount)}, {AccountCode: "1100", Credit: float64(amount)}}, "admin")
		if err != nil {
			t.Fatalf("CreateJournalVoucher: %v", err)
		}
		if err := SubmitForApproval(tenantID, "JournalVoucher", id, "admin", RoleSuperAdmin); err != nil {
			t.Fatalf("SubmitForApproval: %v", err)
		}
		return id
	}

	above := submit(92)
	for _, comment := range []string{"  ok  ", "too short"} {
		err := DecideApproval(tenantID, "JournalVoucher", above, "admin", RoleSuperAdmin, "", "Rejected", comment)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Code != "APPROV-0842" {
			t.Fatalf("own decision above the limit with comment %q: got %v, want APPROV-0842", comment, err)
		}
	}
	if err := DecideApproval(tenantID, "JournalVoucher", above, "admin", RoleSuperAdmin, "", "Rejected", "posted to the wrong account"); err != nil {
		t.Fatalf("own decision with a reason refused: %v", err)
	}

	below := submit(40)
	if err := DecideApproval(tenantID, "JournalVoucher", below, "admin", RoleSuperAdmin, "", "Rejected", "dup"); err != nil {
		t.Fatalf("own decision below the limit must not need a full reason: %v", err)
	}

	// A wide day window either side of today, so the client's day offset never matters.
	from := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	to := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	rows, err := ListOwnApprovalDecisions(tenantID, from, to)
	if err != nil {
		t.Fatalf("ListOwnApprovalDecisions: %v", err)
	}
	found := map[string]OwnApprovalDecision{}
	for _, r := range rows {
		if r.DocumentID == above || r.DocumentID == below {
			found[r.DocumentID] = r
		}
	}
	if r, ok := found[above]; !ok || r.Reason != "posted to the wrong account" || r.Amount != 92 || r.Decision != "Rejected" || r.DecidedBy != "admin" {
		t.Fatalf("report row for the decision above the limit = %+v (found %v), want reason, amount 92, Rejected by admin", r, ok)
	}
	if _, ok := found[below]; !ok {
		t.Fatal("the report must list the own decision below the limit too")
	}
	old, err := ListOwnApprovalDecisions(tenantID, "2001-01-01", "2001-01-31")
	if err != nil {
		t.Fatalf("ListOwnApprovalDecisions (old month): %v", err)
	}
	for _, r := range old {
		if r.DocumentID == above || r.DocumentID == below {
			t.Fatal("the month filter must leave out today's decisions")
		}
	}
}
