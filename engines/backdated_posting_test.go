package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestBackdatedPostingApproval(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	today := time.Now().UTC()
	start := today.AddDate(0, 0, -3).Format("2006-01-02")
	end := today.AddDate(0, 0, 3).Format("2006-01-02")
	voucherDate := today.Format("2006-01-02")

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-BACKDATED%')")
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype IN ('JournalVoucher', 'BackdatedPostingRequest') AND (data->>'narration' LIKE 'TEST-BACKDATED%' OR data->>'reason' LIKE 'TEST-BACKDATED%'))")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-BACKDATED%'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'BackdatedPostingRequest' AND data->>'reason' LIKE 'TEST-BACKDATED%'")
		db.DB.Exec("DELETE FROM " + schema + ".accounting_periods WHERE period_name = 'TEST-PERIOD-BACKDATED'")
	}
	cleanup()
	defer cleanup()

	periodID, err := CreateAccountingPeriod(tenantID, "TEST-PERIOD-BACKDATED", start, end, "system")
	if err != nil {
		t.Fatalf("CreateAccountingPeriod: %v", err)
	}
	if err := CloseAccountingPeriod(tenantID, periodID, "system"); err != nil {
		t.Fatalf("CloseAccountingPeriod: %v", err)
	}

	voucherID, err := CreateJournalVoucher(tenantID, voucherDate, "TEST-BACKDATED voucher", []JournalVoucherLine{{AccountCode: "5100", Debit: 250}, {AccountCode: "1100", Credit: 250}}, "manager1")
	if err != nil {
		t.Fatalf("CreateJournalVoucher: %v", err)
	}
	if err := SubmitForApproval(tenantID, "JournalVoucher", voucherID, "manager1", "Store Manager"); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}
	if err := DecideApproval(tenantID, "JournalVoucher", voucherID, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}

	// The voucher's own approval succeeded, but the automatic post inside
	// it should be blocked by the closed period - Approved, not Posted.
	_, status, err := fetchJournalVoucher(schema, voucherID)
	if err != nil {
		t.Fatalf("fetchJournalVoucher: %v", err)
	}
	if status != "Approved" {
		t.Fatalf("expected voucher to remain Approved (blocked by closed period), got %s", status)
	}
	if err := RetryPostApprovedJournalVoucher(tenantID, voucherID); err == nil {
		t.Fatalf("expected retry to still fail with no BackdatedPostingRequest approved yet")
	}
	var verr *ValidationError
	if postErr := PostDoubleEntry(tenantID, "JournalVoucher", voucherID, map[string]int64{"5100": 250}, map[string]int64{"1100": 250}, voucherDate, ""); postErr == nil {
		t.Fatalf("expected a direct PostDoubleEntry call to also be blocked")
	} else if ve, ok := postErr.(*ValidationError); !ok || ve.Code != "FIN-0260" {
		t.Fatalf("expected a FIN-0260 ValidationError, got %v (%T)", postErr, postErr)
	} else {
		verr = ve
	}
	_ = verr

	// Request + approve a backdated-posting override for this exact voucher.
	reqID := "TEST-BACKDATED-BPR-1"
	reqData := map[string]interface{}{
		"id": reqID, "target_doctype": "JournalVoucher", "target_document_id": voucherID,
		"transaction_date": voucherDate, "reason": "TEST-BACKDATED correction", "status": "Draft",
	}
	bytes, err := json.Marshal(reqData)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'BackdatedPostingRequest', $2, 'Draft', 'manager1')", reqID, bytes); err != nil {
		t.Fatalf("seed BackdatedPostingRequest: %v", err)
	}
	if err := SubmitForApproval(tenantID, "BackdatedPostingRequest", reqID, "manager1", "Store Manager"); err != nil {
		t.Fatalf("SubmitForApproval(BackdatedPostingRequest): %v", err)
	}
	if err := DecideApproval(tenantID, "BackdatedPostingRequest", reqID, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatalf("DecideApproval(BackdatedPostingRequest): %v", err)
	}

	// Retry now succeeds.
	if err := RetryPostApprovedJournalVoucher(tenantID, voucherID); err != nil {
		t.Fatalf("RetryPostApprovedJournalVoucher (after backdated approval): %v", err)
	}
	_, status, err = fetchJournalVoucher(schema, voucherID)
	if err != nil {
		t.Fatalf("fetchJournalVoucher (after retry): %v", err)
	}
	if status != "Posted" {
		t.Fatalf("expected Posted after backdated approval + retry, got %s", status)
	}
	// gl_postings stores paise (Stage 45): the voucher's 250-rupee line is
	// 25000 paise.
	var debit int64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit),0) FROM "+schema+".gl_postings WHERE document_type='JournalVoucher' AND document_id=$1", voucherID).Scan(&debit); err != nil {
		t.Fatalf("query gl_postings: %v", err)
	}
	if debit != 25000 {
		t.Fatalf("expected gl_postings debit=25000, got %d", debit)
	}
}
