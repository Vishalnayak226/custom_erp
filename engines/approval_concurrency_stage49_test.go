package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 49.5.3 (concurrent stale approvals, duplicate transitions): two
// approvers deciding the same pending document at the same moment record one
// decision; a decision on an already decided document is refused.
func TestStage49OneDecisionPerPendingDocument(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE doctype = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV495%')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV495%'")
	}
	cleanup()
	defer cleanup()
	withSelfApproval(t, tenantID, SelfApprovalAnyone)

	voucherID, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV495 race", []JournalVoucherLine{{AccountCode: "5100", Debit: 49}, {AccountCode: "1100", Credit: 49}}, "admin")
	if err != nil {
		t.Fatalf("CreateJournalVoucher: %v", err)
	}
	if err := SubmitForApproval(tenantID, "JournalVoucher", voucherID, "admin", RoleSuperAdmin); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}
	results := make(chan error, 2)
	for _, who := range []string{"approver-a-495", "approver-b-495"} {
		go func(who string) {
			results <- DecideApproval(tenantID, "JournalVoucher", voucherID, who, RoleSuperAdmin, "", "Rejected", "race "+who)
		}(who)
	}
	won := 0
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			won++
		}
	}
	var decisions int
	db.DB.QueryRow("SELECT count(*) FROM "+schema+".approval_log WHERE doctype = 'JournalVoucher' AND document_id = $1 AND action IN ('Approved', 'Rejected')", voucherID).Scan(&decisions)
	if won != 1 || decisions != 1 {
		t.Fatalf("two simultaneous decisions: %d accepted, %d recorded; exactly one must be", won, decisions)
	}
	if err := DecideApproval(tenantID, "JournalVoucher", voucherID, "approver-c-495", RoleSuperAdmin, "", "Approved", "late"); err == nil {
		t.Fatal("a decision on an already decided document must be refused")
	}
}
