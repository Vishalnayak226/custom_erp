package engines

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

func partTestOpen(t *testing.T, schema, id string) float64 {
	t.Helper()
	var dataStr string
	if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id = $1", id).Scan(&dataStr); err != nil {
		t.Fatalf("line %s: %v", id, err)
	}
	var d map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &d)
	return numFromInterface(d["open_amount"])
}

// Stage 170.2: one book entry across several statement lines and a line
// matched for less than its amount; unmatch gives the open amounts back.
func TestBankLinePartMatchAndUnmatchRestoreOpenAmounts(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".bank_match_parts WHERE statement_line_id LIKE 'T1702-%'")
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'TestPart1702'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T1702-%'")
		db.DB.Exec("DELETE FROM " + schema + ".gl_accounts WHERE account_code = '1198'")
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".gl_accounts (account_code, account_name, account_type) VALUES ('1198', 'Test Bank 1702', 'Asset')"); err != nil {
		t.Fatalf("gl account: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	insertTestDoc(t, schema, "T1702-BANK", "BankAccount", map[string]interface{}{"bank_name": "Test", "gl_account_code": "1198"})
	// One book debit of 1,000 and one of 300.
	for id, amt := range map[string]int64{"T1702-A": 100000, "T1702-B": 30000} {
		if err := PostDoubleEntry(tenantID, "TestPart1702", id, map[string]int64{"1198": amt}, map[string]int64{"4100": amt}, "", ""); err != nil {
			t.Fatalf("post: %v", err)
		}
	}
	pA := glPostingID(t, schema, "TestPart1702", "T1702-A", "1198")
	pB := glPostingID(t, schema, "TestPart1702", "T1702-B", "1198")
	line := func(id string, amt float64) {
		insertTestDoc(t, schema, id, "BankStatementLine", map[string]interface{}{"bank_account": "T1702-BANK", "txn_date": today, "amount": amt, "dr_cr": "Credit", "match_status": "Unmatched"})
	}
	line("T1702-L1", 600)
	line("T1702-L2", 700)

	// L1 takes 600 of A; L2 takes the other 400 of A and 250 of B (50 left open).
	if err := MatchBankLineParts(tenantID, "T1702-L1", []BankPart{{PostingID: pA, Amount: 600}}, "", "manager1"); err != nil {
		t.Fatalf("part 1: %v", err)
	}
	if st, _, _ := bankLineStatus(t, schema, "T1702-L1"); st != "Matched" {
		t.Fatalf("L1 fully covered should be Matched, got %s", st)
	}
	var matched *string
	db.DB.QueryRow("SELECT matched_statement_line_id FROM "+schema+".gl_postings WHERE posting_id = $1", pA).Scan(&matched)
	if matched != nil {
		t.Fatalf("A has 400 open, it must not be marked matched yet")
	}
	if code := validationCode(MatchBankLineParts(tenantID, "T1702-L2", []BankPart{{PostingID: pA, Amount: 401}}, "", "manager1")); code != "FIN-1702" {
		t.Fatalf("over the entry's open part: %q", code)
	}
	if code := validationCode(MatchBankLineParts(tenantID, "T1702-L2", []BankPart{{PostingID: pA, Amount: 400}, {PostingID: pB, Amount: 301}}, "", "manager1")); code != "FIN-1702" {
		t.Fatalf("B only has 300: %q", code)
	}
	if err := MatchBankLineParts(tenantID, "T1702-L2", []BankPart{{PostingID: pA, Amount: 400}, {PostingID: pB, Amount: 250}}, "", "manager1"); err != nil {
		t.Fatalf("part 2: %v", err)
	}
	if st, _, _ := bankLineStatus(t, schema, "T1702-L2"); st != "Part Matched" {
		t.Fatalf("L2 should be Part Matched, got %s", st)
	}
	if got := partTestOpen(t, schema, "T1702-L2"); got != 50 {
		t.Fatalf("L2 open = %v, want 50", got)
	}
	if code := validationCode(MatchBankLineParts(tenantID, "T1702-L2", []BankPart{{PostingID: pB, Amount: 51}}, "", "manager1")); code != "FIN-1702" {
		t.Fatalf("B has 50 left: %q", code)
	}
	db.DB.QueryRow("SELECT matched_statement_line_id FROM "+schema+".gl_postings WHERE posting_id = $1", pA).Scan(&matched)
	if matched == nil || *matched != "T1702-L2" {
		t.Fatalf("A should be matched to the line that completed it, got %v", matched)
	}
	open, err := unmatchedBookPostings(schema, "1198", 50)
	if err != nil {
		t.Fatal(err)
	}
	var sawB bool
	for _, p := range open {
		if p.PostingID == pB {
			sawB = true
			if p.Debit != 50 || p.PartMatched != 250 {
				t.Fatalf("B open should show 50 with 250 taken, got %+v", p)
			}
		}
		if p.PostingID == pA {
			t.Fatalf("A is fully matched and must not be listed open")
		}
	}
	if !sawB {
		t.Fatalf("B's open 50 must stay listed")
	}
	// The auto-matcher must leave the part-matched entry alone.
	line("T1702-L3", 50)
	if _, err := ReconcileBankStatement(tenantID, "T1702-BANK", "manager1"); err != nil {
		t.Logf("auto reconcile: %v", err)
	}
	if st, _, _ := bankLineStatus(t, schema, "T1702-L3"); st == "Matched" {
		t.Fatalf("the auto-matcher took an entry that has parts")
	}
	if err := MatchBankLineParts(tenantID, "T1702-L3", []BankPart{{PostingID: pB, Amount: 50}}, "", "manager1"); err != nil {
		t.Fatalf("part 3: %v", err)
	}
	if err := UnmatchBankLine(tenantID, "T1702-L2", "test", "manager1"); err != nil {
		t.Fatalf("unmatch: %v", err)
	}
	if st, _, _ := bankLineStatus(t, schema, "T1702-L2"); st != "Unmatched" {
		t.Fatalf("L2 after unmatch: %s", st)
	}
	var used int64
	db.DB.QueryRow("SELECT COALESCE(SUM(amount_paise),0) FROM "+schema+".bank_match_parts WHERE posting_id = $1", pA).Scan(&used)
	if used != 60000 {
		t.Fatalf("A should be back to 600 taken (L1), got %d paise", used)
	}
	matched = nil
	db.DB.QueryRow("SELECT matched_statement_line_id FROM "+schema+".gl_postings WHERE posting_id = $1", pA).Scan(&matched)
	if matched != nil {
		t.Fatalf("A has 400 open again after the unmatch")
	}
	if code := validationCode(MatchBankLineParts(tenantID, "T1702-L1", []BankPart{{PostingID: pA, Amount: 1}}, "", "manager1")); code != "FIN-1703" {
		t.Fatalf("matched line: %q", code)
	}
}

// Stage 170.3: a statement file loads once; loading it again skips every line.
func TestBankStatementImportIsIdempotent(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'BankStatementLine' AND data->>'bank_account' = 'T1703-BANK'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = 'T1703-BANK'")
		db.DB.Exec("DELETE FROM " + schema + ".gl_accounts WHERE account_code = '1197'")
	}
	cleanup()
	defer cleanup()
	db.DB.Exec("INSERT INTO " + schema + ".gl_accounts (account_code, account_name, account_type) VALUES ('1197', 'Test Bank 1703', 'Asset')")
	insertTestDoc(t, schema, "T1703-BANK", "BankAccount", map[string]interface{}{"bank_name": "Test", "gl_account_code": "1197"})

	csvFile := "Date,Description,Reference,Debit,Credit,Balance\n" +
		"01/11/2026,Customer receipt,UTR111,,1500.00,11500.00\n" +
		"02/11/2026,Vendor payment,CHQ22,400.50,,11099.50\n" +
		"02/11/2026,Vendor payment,CHQ22,400.50,,10699.00\n" +
		"bad-date,x,,1,,\n"
	res, err := ImportBankStatement(tenantID, "T1703-BANK", "csv", csvFile, "manager1")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Loaded != 3 || res.Skipped != 0 || len(res.Errors) != 1 {
		t.Fatalf("first load: %+v", res)
	}
	again, err := ImportBankStatement(tenantID, "T1703-BANK", "csv", csvFile, "manager1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Loaded != 0 || again.Skipped != 3 {
		t.Fatalf("second load must skip everything: %+v", again)
	}
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'BankStatementLine' AND data->>'bank_account' = 'T1703-BANK'").Scan(&n)
	if n != 3 {
		t.Fatalf("expected 3 lines stored, got %d", n)
	}

	mt := ":20:STMT\n:25:123\n:60F:C261101INR10000,00\n" +
		":61:2611030103C250,75NTRFREF900//BANKREF\n:86:Receipt from ACME\n" +
		":61:2611040104D100,00NCHKCHQ77\n:86:Cheque paid\n:62F:C261104INR10150,75\n"
	m1, err := ImportBankStatement(tenantID, "T1703-BANK", "", mt, "manager1")
	if err != nil || m1.Loaded != 2 || m1.Format != "mt940" {
		t.Fatalf("mt940 load: %+v %v", m1, err)
	}
	if m1.Lines[0].Amount != 250.75 || m1.Lines[0].DrCr != "Credit" {
		t.Fatalf("mt940 credit line: %+v", m1.Lines[0])
	}
	if m1.Lines[1].DrCr != "Debit" || m1.Lines[1].Amount != 100 {
		t.Fatalf("mt940 debit line: %+v", m1.Lines[1])
	}
	m2, _ := ImportBankStatement(tenantID, "T1703-BANK", "mt940", mt, "manager1")
	if m2.Loaded != 0 || m2.Skipped != 2 {
		t.Fatalf("mt940 reload: %+v", m2)
	}
	_, e := ImportBankStatement(tenantID, "T1703-BANK", "csv", "", "manager1")
	if code := validationCode(e); code != "FIN-1704" {
		t.Fatalf("empty file: %q", code)
	}
	if !strings.HasPrefix(res.Lines[0].ID, "BSL-") {
		t.Fatalf("line id: %s", res.Lines[0].ID)
	}
}

// Stage 170.4: the bank's answer to a payment file records UTRs, raises an
// exception for a rejection without touching the ledger, lists unknown
// references and skips a replay.
func TestPaymentBankResponseRecordsUTRAndRaisesException(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".payment_bank_response WHERE proposal_id = 'T1704-PP'")
		db.DB.Exec("DELETE FROM " + schema + ".payment_utr_log WHERE proposal_id = 'T1704-PP'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T1704-%' OR id LIKE 'BSL-REJ-%' AND data->>'payment_proposal' = 'T1704-PP'")
		db.DB.Exec("DELETE FROM " + schema + ".gl_accounts WHERE account_code = '1196'")
	}
	cleanup()
	defer cleanup()
	db.DB.Exec("INSERT INTO " + schema + ".gl_accounts (account_code, account_name, account_type) VALUES ('1196', 'Test Bank 1704', 'Asset')")
	insertTestDoc(t, schema, "T1704-BANK", "BankAccount", map[string]interface{}{"bank_name": "Test", "gl_account_code": "1196"})
	insertTestDoc(t, schema, "T1704-PP", "PaymentProposal", map[string]interface{}{"execution_results": []map[string]interface{}{
		{"invoice_id": "T1704-INV1", "paid": true, "amount": 500, "amount_paise": 50000},
		{"invoice_id": "T1704-INV2", "paid": true, "amount": 300, "amount_paise": 30000},
	}})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Executed' WHERE id = 'T1704-PP'")

	file := "payment_reference,utr,status,reason\n" +
		"T1704-INV1,UTRT1704A,Success,\n" +
		"T1704-INV2,,Rejected,Account closed\n" +
		"T1704-NOPE,UTRT1704Z,Success,\n"
	var postingsBefore int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".gl_postings").Scan(&postingsBefore)
	res, err := ImportPaymentBankResponse(tenantID, "T1704-PP", "T1704-BANK", file, "manager1")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Paid != 1 || res.Rejected != 1 || res.Unknown != 1 || res.Errors != 0 {
		t.Fatalf("outcome: %+v", res)
	}
	var utr string
	if err := db.DB.QueryRow("SELECT utr FROM " + schema + ".payment_utr_log WHERE proposal_id = 'T1704-PP' AND invoice_id = 'T1704-INV1'").Scan(&utr); err != nil || utr != "UTRT1704A" {
		t.Fatalf("utr not recorded: %v %q", err, utr)
	}
	if res.Rows[1].Exception == "" {
		t.Fatalf("rejection should name its exception line: %+v", res.Rows[1])
	}
	if st, _, _ := bankLineStatus(t, schema, res.Rows[1].Exception); st != "Exception" {
		t.Fatalf("rejected payment line status: %s", st)
	}
	var postingsAfter int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".gl_postings").Scan(&postingsAfter)
	if postingsAfter != postingsBefore {
		t.Fatalf("a rejection must not post or reverse anything (%d -> %d)", postingsBefore, postingsAfter)
	}
	again, err := ImportPaymentBankResponse(tenantID, "T1704-PP", "T1704-BANK", file, "manager1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Skipped != 2 || again.Paid != 0 || again.Rejected != 0 {
		t.Fatalf("replay: %+v", again)
	}
	_, e := ImportPaymentBankResponse(tenantID, "T1704-PP", "T1704-BANK", "foo,bar\n1,2\n", "manager1")
	if code := validationCode(e); code != "FIN-1707" {
		t.Fatalf("missing columns: %q", code)
	}
}
