package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Item 193.2: a journal voucher takes paise. The balance is checked in whole
// paise, a third decimal is refused, whole-rupee vouchers stored before this
// read and post exactly as before, and the GL total equals the voucher.

func TestJournalVoucherPaiseValidation(t *testing.T) {
	total, err := validateJournalVoucherLines([]JournalVoucherLine{
		{AccountCode: "5100", Debit: 15.00}, {AccountCode: "5100", Debit: 2.70}, {AccountCode: "1100", Credit: 17.70},
	})
	if err != nil || total != 17.70 {
		t.Fatalf("15.00 + 2.70 against 17.70 must balance to the paisa: total=%v err=%v", total, err)
	}
	// 0.1 + 0.2 is the float trap; it must still balance against 0.30.
	if _, err := validateJournalVoucherLines([]JournalVoucherLine{
		{AccountCode: "5100", Debit: 0.1}, {AccountCode: "5110", Debit: 0.2}, {AccountCode: "1100", Credit: 0.3},
	}); err != nil {
		t.Fatalf("0.10 + 0.20 against 0.30 must balance: %v", err)
	}
	if _, err := validateJournalVoucherLines([]JournalVoucherLine{
		{AccountCode: "5100", Debit: 17.71}, {AccountCode: "1100", Credit: 17.70},
	}); err == nil {
		t.Fatal("one paisa out must be refused")
	}
	_, err = validateJournalVoucherLines([]JournalVoucherLine{
		{AccountCode: "5100", Debit: 10.005}, {AccountCode: "1100", Credit: 10.005},
	})
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != "FIN-0843" {
		t.Fatalf("a third decimal must be refused with FIN-0843, got %v", err)
	}
	// The generic save path (and import) is held to the same rule.
	payload := map[string]interface{}{"lines": `[{"account_code":"5100","debit":10.005},{"account_code":"1100","credit":10.005}]`}
	if err := validateJournalVoucherSave(payload); err == nil {
		t.Fatal("a generic save with a third decimal must be refused")
	}
	// A whole-rupee voucher stored before paise decodes and re-encodes unchanged.
	var old []JournalVoucherLine
	if err := json.Unmarshal([]byte(`[{"account_code":"5100","debit":777},{"account_code":"1100","credit":777}]`), &old); err != nil {
		t.Fatal(err)
	}
	if total, err := validateJournalVoucherLines(old); err != nil || total != 777 {
		t.Fatalf("old whole-rupee voucher: total=%v err=%v", total, err)
	}
	if b, _ := json.Marshal(old); string(b) != `[{"account_code":"5100","debit":777},{"account_code":"1100","credit":777}]` {
		t.Fatalf("old voucher must re-encode unchanged, got %s", b)
	}
}

func TestJournalVoucherPaisePostsAndReversesToZero(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		mine := "SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND (data->>'narration' LIKE 'TEST-JVP%' OR data->>'reversed_from' IN (SELECT id FROM " + schema + ".documents WHERE data->>'narration' LIKE 'TEST-JVP%'))"
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'JournalVoucher' AND document_id IN (" + mine + ")")
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE doctype = 'JournalVoucher' AND document_id IN (" + mine + ")")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id IN (" + mine + ")")
	}
	cleanup()
	defer cleanup()

	post := func(id string) {
		t.Helper()
		if err := SubmitForApproval(tenantID, "JournalVoucher", id, "manager1", "Store Manager"); err != nil {
			t.Fatalf("SubmitForApproval: %v", err)
		}
		if err := DecideApproval(tenantID, "JournalVoucher", id, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
			t.Fatalf("DecideApproval: %v", err)
		}
	}
	gl := func(id string) (d, c int64) {
		t.Helper()
		if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_type='JournalVoucher' AND document_id=$1", id).Scan(&d, &c); err != nil {
			t.Fatalf("gl_postings: %v", err)
		}
		return
	}

	id, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JVP paise", []JournalVoucherLine{
		{AccountCode: "5100", Debit: 15.00}, {AccountCode: "5100", Debit: 2.70}, {AccountCode: "1100", Credit: 17.70},
	}, "manager1")
	if err != nil {
		t.Fatalf("CreateJournalVoucher: %v", err)
	}
	data, _, err := fetchJournalVoucher(schema, id)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := parityNumber(data["total_amount"]); got != 17.70 {
		t.Fatalf("total_amount (what approval slabs read) = %v, want 17.70", data["total_amount"])
	}
	post(id)
	if d, c := gl(id); d != 1770 || c != 1770 {
		t.Fatalf("GL for 17.70 must be 1770 paise each side, got debit=%d credit=%d", d, c)
	}

	revID, err := ReverseJournalVoucher(tenantID, id, "manager1")
	if err != nil {
		t.Fatalf("ReverseJournalVoucher: %v", err)
	}
	post(revID)
	var netD, netC int64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_type='JournalVoucher' AND document_id IN ($1,$2) AND account_code='1100'", id, revID).Scan(&netD, &netC); err != nil {
		t.Fatal(err)
	}
	if netD != netC || netD != 1770 {
		t.Fatalf("original and reversal must offset on 1100: debit=%d credit=%d", netD, netC)
	}
	if rd, rc := gl(revID); rd != 1770 || rc != 1770 {
		t.Fatalf("reversal GL = %d/%d, want 1770/1770", rd, rc)
	}
}
