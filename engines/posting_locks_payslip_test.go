package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stage 135.21 (part B): finance, HR and settlement postings called eight
// times at once - double-click, retry, two users - act once, and the other
// calls are refused. Each test provisions its own tenant (stage135Tenant).

// s13521bRace calls fn from n goroutines released together.
func s13521bRace(n int, fn func(i int) error) []error {
	var wg sync.WaitGroup
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = fn(i)
		}(i)
	}
	close(start)
	wg.Wait()
	return errs
}

// s13521bOneWinner fails unless exactly one call succeeded and every other was
// refused with one of the given messages; it returns the winner's index.
func s13521bOneWinner(t *testing.T, what string, errs []error, refusals ...string) int {
	t.Helper()
	winner, wins := -1, 0
	for i, err := range errs {
		if err == nil {
			winner, wins = i, wins+1
			continue
		}
		refused := false
		for _, r := range refusals {
			refused = refused || strings.Contains(err.Error(), r)
		}
		if !refused {
			t.Errorf("%s: call %d failed with %q, want a refusal containing one of %q", what, i, err, refusals)
		}
	}
	if wins != 1 {
		t.Fatalf("%s: %d of %d calls at once succeeded, want exactly 1 (errors: %v)", what, wins, len(errs), errs)
	}
	return winner
}

func s13521bInt(t *testing.T, q string, args ...interface{}) int64 {
	t.Helper()
	var v int64
	if err := db.DB.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}

func s13521bDoc(t *testing.T, schema, id string) (string, map[string]interface{}) {
	t.Helper()
	var status, raw string
	if err := db.DB.QueryRow("SELECT status, data FROM "+schema+".documents WHERE id = $1", id).Scan(&status, &raw); err != nil {
		t.Fatalf("document %s: %v", id, err)
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &data)
	return status, data
}

// Path 1: a payslip posted eight times at once reduces the loan once.
func TestStage13521bPayslipPostsOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	day := time.Now().AddDate(0, 0, -4).Format("2006-01-02")
	insert("EMP-B1", "Employee", "Active", map[string]interface{}{"name": "Test Loan", "bank_account_no": "0013521"})
	insert("LOAN-B1", "EmployeeLoan", "Active", map[string]interface{}{"employee_id": "EMP-B1", "principal_amount": 5000.0,
		"monthly_deduction": 1000.0, "outstanding_balance": 5000.0, "status": "Active"})
	insert("PS-B1", "Payslip", "Draft", map[string]interface{}{"employee_id": "EMP-B1", "period_from": day, "period_to": day,
		"gross_pay": 20000.0, "pf_deduction": 0.0, "loan_deduction": 1000.0, "net_pay": 19000.0, "status": "Draft"})

	errs := s13521bRace(8, func(int) error { return PostPayslipToGL(tenantID, "PS-B1", "system") })
	if _, loan := s13521bDoc(t, schema, "LOAN-B1"); numFromInterface(loan["outstanding_balance"]) != 4000 {
		t.Errorf("loan outstanding %v after one payslip, want 4000 (deducted once)", loan["outstanding_balance"])
	}
	s13521bOneWinner(t, "post payslip", errs, "only a Draft payslip can be posted")

	if dr := s13521bInt(t, "SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_type = 'Payslip' AND document_id = 'PS-B1'"); dr != 2000000 {
		t.Fatalf("payslip journal debits %d paise, want 2000000 (one journal)", dr)
	}
	if status, _ := s13521bDoc(t, schema, "PS-B1"); status != "Posted" {
		t.Fatalf("payslip status %s, want Posted", status)
	}
}

// Path 2: a loan disbursed eight times at once posts and activates once.
func TestStage13521bLoanDisbursesOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	insert("LOAN-B2", "EmployeeLoan", "Draft", map[string]interface{}{"employee_id": "EMP-B2", "principal_amount": 5000.0,
		"monthly_deduction": 1000.0, "status": "Draft"})

	errs := s13521bRace(8, func(int) error { return DisburseEmployeeLoan(tenantID, "LOAN-B2", "system") })
	s13521bOneWinner(t, "disburse loan", errs, "has already been disbursed")

	if dr := s13521bInt(t, "SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_type = 'EmployeeLoan' AND document_id = 'LOAN-B2'"); dr != 500000 {
		t.Fatalf("disbursal journal debits %d paise, want 500000", dr)
	}
	if status, loan := s13521bDoc(t, schema, "LOAN-B2"); status != "Active" || numFromInterface(loan["outstanding_balance"]) != 5000 {
		t.Fatalf("loan %s outstanding %v, want Active 5000", status, loan["outstanding_balance"])
	}
}

// Path 3: an expense claim paid eight times at once is paid once.
func TestStage13521bExpenseClaimPaidOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	insert("CL-B3", "ExpenseClaim", "Verified", map[string]interface{}{"employee_id": "EMP-B3", "amount": 1000.0, "gst_amount": 0.0, "status": "Verified"})

	errs := s13521bRace(8, func(int) error {
		_, err := PayExpenseClaim(tenantID, "CL-B3")
		return err
	})
	s13521bOneWinner(t, "pay claim", errs, "only a finance-verified claim can be paid")

	if dr := s13521bInt(t, "SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_type = 'ExpenseClaim' AND document_id = 'CL-B3'"); dr != 100000 {
		t.Fatalf("claim payment debits %d paise, want 100000", dr)
	}
	if status, _ := s13521bDoc(t, schema, "CL-B3"); status != "Paid" {
		t.Fatalf("claim status %s, want Paid", status)
	}
}

// Path 4: a card payout matched eight times at once is booked and matched once.
func TestStage13521bPOSPayoutMatchedOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	today := BusinessToday()
	insert("BANK-B4", "BankAccount", "Active", map[string]interface{}{"bank_name": "Test", "account_number": "13521", "gl_account_code": "1100"})
	if err := PostDoubleEntry(tenantID, "Test13521b", "CARD-B4", map[string]int64{"1101": 200000}, map[string]int64{"4100": 200000}, "", ""); err != nil {
		t.Fatal(err)
	}
	insert("BSL-B4", "BankStatementLine", "Active", map[string]interface{}{"bank_account": "BANK-B4", "txn_date": today, "amount": 1960.0, "dr_cr": "Credit", "match_status": "Unmatched"})

	errs := s13521bRace(8, func(i int) error { return MatchPOSPayout(tenantID, "BSL-B4", "1101", 40, "actor"+string(rune('0'+i))) })
	winner := s13521bOneWinner(t, "match card payout", errs, "this line is already Matched")

	if cr := s13521bInt(t, "SELECT COALESCE(SUM(credit), 0) FROM "+schema+".gl_postings WHERE document_type = 'BankStatementLine' AND document_id = 'BSL-B4' AND account_code = '1101'"); cr != 200000 {
		t.Fatalf("clearing credited %d paise, want 200000 (one payout)", cr)
	}
	if _, line := s13521bDoc(t, schema, "BSL-B4"); line["match_status"] != "Matched" || line["matched_by"] != "actor"+string(rune('0'+winner)) {
		t.Fatalf("line %v matched by %v, want Matched by the one call that succeeded (actor%d)", line["match_status"], line["matched_by"], winner)
	}
}

// Path 5: a marketplace settlement processed eight times at once settles once.
func TestStage13521bMarketplaceSettlementOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	insert("ORD-B5", "POSCart", "Shipped", map[string]interface{}{"status": "Shipped"})

	errs := s13521bRace(8, func(i int) error {
		// Each caller sends its own commission split, so a repeat that got
		// through would leave the saved settlement disagreeing with the journal.
		return ProcessMarketplaceSettlement(tenantID, "Shopify", "SETT-B5", 10000, 1000+i, 9000-i, []string{"ORD-B5"})
	})
	winner := s13521bOneWinner(t, "process settlement", errs, "has already been reconciled")

	if cr := s13521bInt(t, "SELECT COALESCE(SUM(credit), 0) FROM "+schema+".gl_postings WHERE document_type = 'MarketplaceSettlement' AND document_id = 'SETT-B5'"); cr != 1000000 {
		t.Fatalf("settlement credited %d paise, want 1000000 (one journal)", cr)
	}
	commission := s13521bInt(t, "SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_type = 'MarketplaceSettlement' AND document_id = 'SETT-B5' AND account_code = '5200'")
	if status, s := s13521bDoc(t, schema, "SETT-B5"); status != "Reconciled" || numFromInterface(s["commission"]) != float64(1000+winner) || commission != int64(1000+winner)*100 {
		t.Fatalf("settlement %s commission %v, journal commission %d paise - want Reconciled, both the winner's %d", status, s["commission"], commission, 1000+winner)
	}
	if status, _ := s13521bDoc(t, schema, "ORD-B5"); status != "Settled" {
		t.Fatalf("order %s, want Settled", status)
	}
}

// s13521bSettlementLine seeds one MarketplaceSettlementLine and a Settlement
// reason code.
func s13521bSettlementLine(insert func(id, doctype, status string, data map[string]interface{}), id string, data map[string]interface{}) {
	line := map[string]interface{}{"channel": "b6test", "channel_order_id": "CH-" + id, "settlement_batch_id": "BATCH-B6",
		"settlement_date": "2026-08-20", "gross_amount": 700.0, "commission": 70.0, "shipping_fee": 10.0, "other_fee": 0.0,
		"tds": 0.0, "tcs": 0.0, "net_payout": 620.0}
	for k, v := range data {
		line[k] = v
	}
	insert(id, "MarketplaceSettlementLine", "Active", line)
}

// Path 6: write-off and resolve of one disputed settlement line at once - one
// of them clears it, once.
func TestStage13521bSettlementLineClearsOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	insert("RC-B6", "ReasonCode", "Active", map[string]interface{}{"code": "RC-B6", "description": "accepted", "category": "Settlement", "status": "Active"})
	s13521bSettlementLine(insert, "MSL-B6", map[string]interface{}{"match_status": "Disputed", "expected_amount": 900.0, "variance_amount": 200.0})

	errs := s13521bRace(8, func(i int) error {
		if i%2 == 0 {
			return WriteOffSettlementVariance(tenantID, "MSL-B6", "RC-B6", "tester")
		}
		return ResolveSettlementDispute(tenantID, "MSL-B6", 900, "tester")
	})
	winner := s13521bOneWinner(t, "write off / resolve", errs, "cannot be written off from status", "is not Disputed")

	if dr, cr := s13521bInt(t, "SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_id = 'MSL-B6'"),
		s13521bInt(t, "SELECT COALESCE(SUM(credit), 0) FROM "+schema+".gl_postings WHERE document_id = 'MSL-B6'"); dr != 90000 || cr != 90000 {
		t.Fatalf("line journal %d / %d paise, want 90000 each (one posting)", dr, cr)
	}
	want := "WrittenOff"
	if winner%2 == 1 {
		want = "Matched"
	}
	if _, line := s13521bDoc(t, schema, "MSL-B6"); line["match_status"] != want {
		t.Fatalf("line %v, want %s - the outcome of the one call that succeeded", line["match_status"], want)
	}
}

// Path 6: eight reconcile passes at once match an Unmatched line once.
func TestStage13521bSettlementReconcileMatchesOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".channel_order_mapping (order_id, channel, channel_order_id) VALUES ('SO-B6', 'b6test', 'CH-MSL-B6R')"); err != nil {
		t.Fatalf("seed channel_order_mapping: %v", err)
	}
	insert("INV-B6", "SalesInvoice", "Approved", map[string]interface{}{"sales_order_id": "SO-B6", "total_amount": 700.0})
	s13521bSettlementLine(insert, "MSL-B6R", map[string]interface{}{"match_status": "Unmatched"})

	var mu sync.Mutex
	matched := 0
	errs := s13521bRace(8, func(int) error {
		res, err := ReconcileMarketplaceSettlements(tenantID, "tester")
		if err == nil {
			mu.Lock()
			matched += res.Matched
			mu.Unlock()
		}
		return err
	})
	for _, err := range errs {
		if err != nil {
			t.Fatalf("reconcile pass: %v", err)
		}
	}
	if matched != 1 {
		t.Fatalf("eight passes at once reported the line matched %d times, want 1", matched)
	}
	if dr := s13521bInt(t, "SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_id = 'MSL-B6R'"); dr != 70000 {
		t.Fatalf("line journal debits %d paise, want 70000", dr)
	}
}

// Path 7: an asset capitalised eight times at once keeps the one useful life
// its journal was posted with.
func TestStage13521bAssetCapitalisedOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	insert("AST-B7", "Asset", "Draft", map[string]interface{}{"name": "Test Asset", "cost": 50000.0, "status": "Draft"})

	errs := s13521bRace(8, func(i int) error { return CapitalizeAssetWithLife(tenantID, "AST-B7", float64(i+1)) })
	winner := s13521bOneWinner(t, "capitalise", errs, "only a Draft asset can be capitalised")

	if dr := s13521bInt(t, "SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_type = 'Asset' AND document_id = 'AST-B7'"); dr != 5000000 {
		t.Fatalf("capitalisation debits %d paise, want 5000000", dr)
	}
	if status, a := s13521bDoc(t, schema, "AST-B7"); status != "Capitalised" || a["useful_life_years"] != float64(winner+1) {
		t.Fatalf("asset %s life %v, want Capitalised with the winner's %d years", status, a["useful_life_years"], winner+1)
	}
}

// Path 8: eight revaluation runs at once for eight different dates revalue an
// open foreign invoice once - the key is per date, so it never stopped them.
func TestStage13521bRevaluationPostsOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	for _, code := range []string{"USD", "INR"} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Currency', $2, 'Active', 'system') ON CONFLICT DO NOTHING",
			"CUR-B8-"+code, `{"code": "`+code+`", "name": "`+code+`"}`); err != nil {
			t.Fatalf("seed currency %s: %v", code, err)
		}
	}
	insert("FXR-B8", "ExchangeRate", "Active", map[string]interface{}{"from_currency": "USD", "to_currency": "INR", "rate": 85.0,
		"rate_type": "Closing", "effective_from": "2020-01-01"})
	insert("SI-B8", "SalesInvoice", "Approved", map[string]interface{}{"currency": "USD", "exchange_rate": 83.0,
		"total_amount": 1000.0, "base_total_amount": 83000.0})

	var mu sync.Mutex
	posted := 0
	errs := s13521bRace(8, func(i int) error {
		run, err := RevalueOpenForeignItems(tenantID, time.Now().AddDate(0, 0, -1-i).Format("2006-01-02"), "Closing", "system", false)
		if err == nil {
			mu.Lock()
			posted += run.DocumentsPosted
			mu.Unlock()
		}
		return err
	})
	for _, err := range errs {
		if err != nil {
			t.Fatalf("revaluation run: %v", err)
		}
	}
	keys := s13521bInt(t, "SELECT COUNT(DISTINCT idempotency_key) FROM "+schema+".gl_postings WHERE document_id = 'SI-B8' AND idempotency_key LIKE '%FX_REVAL%'")
	dr := s13521bInt(t, "SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_id = 'SI-B8' AND account_code = '1300'")
	if posted != 1 || keys != 1 || dr != 200000 {
		t.Fatalf("eight runs at once: %d posted, %d revaluation journals, receivable up %d paise - want 1, 1, 200000", posted, keys, dr)
	}
	if _, inv := s13521bDoc(t, schema, "SI-B8"); numFromInterface(inv["unrealized_fx_cumulative"]) != 2000 {
		t.Fatalf("invoice cumulative revaluation %v, want 2000", inv["unrealized_fx_cumulative"])
	}
}

// Path 9: an Approved journal voucher retried eight times at once posts once.
func TestStage13521bJournalVoucherRetryPostsOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	insert("JV-B9", "JournalVoucher", "Approved", map[string]interface{}{"voucher_date": BusinessToday(), "narration": "TEST-13521B",
		"lines": `[{"account_code":"5100","debit":250},{"account_code":"1100","credit":250}]`, "total_amount": 250, "status": "Approved"})

	errs := s13521bRace(8, func(int) error { return RetryPostApprovedJournalVoucher(tenantID, "JV-B9") })
	s13521bOneWinner(t, "retry voucher", errs, "only an Approved (not yet Posted) journal voucher can be retried")

	if dr := s13521bInt(t, "SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_type = 'JournalVoucher' AND document_id = 'JV-B9'"); dr != 25000 {
		t.Fatalf("voucher debits %d paise, want 25000", dr)
	}
	if status, _ := s13521bDoc(t, schema, "JV-B9"); status != "Posted" {
		t.Fatalf("voucher %s, want Posted", status)
	}
}

// Path 9: an Approved intercompany transaction retried eight times at once
// posts both legs once.
func TestStage13521bIntercompanyRetryPostsOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	for _, e := range []string{"ENT-B9A", "ENT-B9B"} {
		insert(e, "LegalEntity", "Active", map[string]interface{}{"code": e, "name": e, "status": "Active"})
	}
	insert("ICT-B9", "IntercompanyTransaction", "Approved", map[string]interface{}{"transaction_date": BusinessToday(), "narration": "TEST-13521B",
		"from_entity": "ENT-B9A", "from_account_code": "4100", "to_entity": "ENT-B9B", "to_account_code": "5400", "amount": 1000.0, "status": "Approved"})

	errs := s13521bRace(8, func(int) error { return RetryPostApprovedIntercompanyTransaction(tenantID, "ICT-B9") })
	s13521bOneWinner(t, "retry intercompany", errs, "only an Approved or Partially Posted intercompany transaction can be retried")

	rows := s13521bInt(t, "SELECT COUNT(*) FROM "+schema+".gl_postings WHERE document_type = 'IntercompanyTransaction' AND document_id = 'ICT-B9'")
	dr := s13521bInt(t, "SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_type = 'IntercompanyTransaction' AND document_id = 'ICT-B9'")
	if rows != 4 || dr != 200000 {
		t.Fatalf("intercompany journal %d rows, %d paise of debit - want 4 rows, 200000 (two legs once)", rows, dr)
	}
	if status, _ := s13521bDoc(t, schema, "ICT-B9"); status != "Posted" {
		t.Fatalf("transaction %s, want Posted", status)
	}
}
