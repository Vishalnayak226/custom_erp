package engines

import (
	"custom_erp/db"
	"errors"
	"testing"
)

// Stage 84.3: the person who submitted a document deciding it themselves is
// refused with its own code (APPROV-0840), not as an uncoded error that the
// API sends out as GLOBAL-0002 "Correct the field format".
func TestStage84SelfApprovalIsCoded(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE doctype = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV84%')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV84%'")
	}
	cleanup()
	defer cleanup()
	withSelfApproval(t, tenantID, SelfApprovalNobody) // Stage 92: the submitter may not decide

	voucherID, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV84 self decision", []JournalVoucherLine{{AccountCode: "5100", Debit: 84}, {AccountCode: "1100", Credit: 84}}, "manager1")
	if err != nil {
		t.Fatalf("CreateJournalVoucher: %v", err)
	}
	if err := SubmitForApproval(tenantID, "JournalVoucher", voucherID, "manager1", "Store Manager"); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}

	for _, decision := range []string{"Approved", "Rejected"} {
		err := DecideApproval(tenantID, "JournalVoucher", voucherID, "manager1", "Store Manager", "HO", decision, "deciding my own")
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "APPROV-0840" {
			t.Fatalf("%s by the submitter: want APPROV-0840, got %v", decision, err)
		}
	}

	var status string
	if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", voucherID).Scan(&status); err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != "Pending Approval" {
		t.Fatalf("status after refused self-decisions = %q, want Pending Approval", status)
	}

	// Bulk approval reports the same refusal per document, as text.
	_, failed, err := BulkDecideApproval(tenantID, "JournalVoucher", []string{voucherID}, "manager1", "Store Manager", "HO", "Approved", "bulk")
	if err != nil {
		t.Fatalf("BulkDecideApproval: %v", err)
	}
	if failed[voucherID] == "" {
		t.Fatalf("bulk self-approval was not refused: %v", failed)
	}
}
