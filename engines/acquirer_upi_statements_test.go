package engines

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

// 185.2: a provider's own statement columns are read; a payout file and other
// doctypes pass through as they were.
func TestUPIStatementColumns(t *testing.T) {
	recs, warn := normalizeImportColumns(AcquirerSettlementDoctype, [][]string{
		{"\xef\xbb\xbfTxn Date", "RRN", "Amount (Rs)", "Status", "Settlement Date", "Settlement UTR", "Payer VPA"},
		{"14/11/2026 10:35:12", "631812345678", "₹1,250.00", "SUCCESS", "15-11-2026", "AXISN001", "a@ybl"},
		{"14/11/2026 11:00:00", "631812345679", "300", "FAILED", "15-11-2026", "AXISN001", "b@ybl"},
		{"Total", "", "1550", "", "", "", ""},
	})
	if want := []string{"sales_date", "payment_ref", "gross", "settlement_date", "utr", "mode", "net"}; !reflect.DeepEqual(recs[0], want) {
		t.Fatalf("columns = %v, want %v", recs[0], want)
	}
	if want := []string{"2026-11-14", "631812345678", "1250.00", "2026-11-15", "AXISN001", "UPI", "1250.00"}; len(recs) != 2 || !reflect.DeepEqual(recs[1], want) {
		t.Fatalf("rows = %v, want one row %v", recs[1:], want)
	}
	if len(warn) != 2 || !strings.Contains(warn[0], "Payer VPA") || !strings.HasPrefix(warn[1], "1 failed") {
		t.Errorf("warnings = %v", warn)
	}
	// A bare UTR beside a Settlement UTR is the customer's reference.
	recs, _ = normalizeImportColumns(AcquirerSettlementDoctype, [][]string{
		{"Transaction Date", "UTR", "Settlement UTR", "Amount", "Fee"},
		{"Nov 14, 2026", "6318000001", "AXISN002", "Rs. 500", "1.18"},
	})
	if want := []string{"sales_date", "payment_ref", "utr", "gross", "fee", "mode", "settlement_date", "net"}; !reflect.DeepEqual(recs[0], want) {
		t.Fatalf("PhonePe-style columns = %v, want %v", recs[0], want)
	}
	if want := []string{"2026-11-14", "6318000001", "AXISN002", "500.00", "1.18", "UPI", "2026-11-14", "498.82"}; !reflect.DeepEqual(recs[1], want) {
		t.Fatalf("PhonePe-style row = %v, want %v", recs[1], want)
	}
	// The payout file the template gives is unchanged.
	payout := [][]string{{"settlement_date", "mode", "gross", "fee", "net", "utr"}, {"2026-11-15", "Card", "1000.00", "20.00", "980.00", "HDFC9"}}
	if recs, warn = normalizeImportColumns(AcquirerSettlementDoctype, payout); !reflect.DeepEqual(recs, payout) || len(warn) != 0 {
		t.Errorf("payout file: %v %v", recs, warn)
	}
	other := [][]string{{"Amount", "RRN"}, {"1", "2"}}
	if recs, _ = normalizeImportColumns("Item", other); !reflect.DeepEqual(recs, other) {
		t.Errorf("another doctype's file changed: %v", recs)
	}
}

// 185.2: BharatPe- and PhonePe-style statements go in through the standard
// import; each payment is checked against the till bill that took it, the
// payout's payments are summed to the bank credit, and a file imported twice
// books nothing twice.
func TestUPIStatementMatchesBillsAndPayout(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	today := BusinessToday()
	day, _ := time.Parse("2006-01-02", today)
	insert("CART-U1", "POSCart", "Paid", "cashier1", map[string]interface{}{"location": "S1", "payment_mode": "UPI", "payment_reference": "631812345678",
		"allocation": []interface{}{map[string]interface{}{"tenders": map[string]interface{}{"UPI": 125000.0}}}})
	insert("CART-U2", "POSCart", "Paid", "cashier1", map[string]interface{}{"location": "S1", "payment_mode": "Cash",
		"tenders":    []interface{}{map[string]interface{}{"mode": "UPI", "name": "UPI", "amount": 200.0, "reference": "631899990000"}},
		"allocation": []interface{}{map[string]interface{}{"tenders": map[string]interface{}{"Cash": 10000.0, "UPI": 20000.0}}}})
	insert("CART-U3", "POSCart", "Paid", "cashier1", map[string]interface{}{"location": "S1", "payment_mode": "UPI", "payment_reference": "631877770000",
		"allocation": []interface{}{map[string]interface{}{"tenders": map[string]interface{}{"UPI": 50000.0}}}})
	// The three bills (1,950) and a 50 UPI bill keyed without a ref: 2,000
	// in UPI clearing.
	if err := PostDoubleEntry(tenantID, "POSCart", "CART-U", map[string]int64{"1102": 200000}, map[string]int64{"4100": 200000}, "", "POSCart:CART-U:T"); err != nil {
		t.Fatal(err)
	}
	insert("BANK-185", "BankAccount", "Active", "system", map[string]interface{}{"bank_name": "Test Bank", "account_number": "000185", "gl_account_code": "1100", "status": "Active"})
	insert("BSL-185", "BankStatementLine", "Unmatched", "system", map[string]interface{}{"bank_account": "BANK-185", "txn_date": today, "amount": 1994.0, "dr_cr": "Credit", "match_status": "Unmatched"})

	bharatPe := "\xef\xbb\xbfTxn Date,RRN,Amount (Rs),Fee,Status,Settlement Date,Settlement UTR,Payer VPA\n" +
		fmt.Sprintf("%s 10:35:12,631812345678,\"₹1,250.00\",5,SUCCESS,%s,AXISN001,a@ybl\n", day.Format("02/01/2006"), day.Format("02-01-2006")) +
		fmt.Sprintf("%s 10:40:00,631899990000,200,0,SUCCESS,%s,AXISN001,b@ybl\n", day.Format("02/01/2006"), day.Format("02-01-2006")) +
		fmt.Sprintf("%s 10:45:00,631855550000,300,0,FAILED,%s,,c@ybl\n", day.Format("02/01/2006"), day.Format("02-01-2006")) +
		"Total,,1750,5,,,,\n"
	phonePe := "Transaction Date,UTR,Settlement UTR,Amount,Transaction Type\n" +
		fmt.Sprintf("\"%s\",631877770000,AXISN001,450,PAYMENT\n", day.Format("Jan 2, 2006")) +
		fmt.Sprintf("\"%s\",631866660000,AXISN001,99,PAYMENT\n", day.Format("Jan 2, 2006")) +
		fmt.Sprintf("\"%s\",631812345678,AXISN001,-1250,REFUND\n", day.Format("Jan 2, 2006"))
	load := func(file string, want int) {
		t.Helper()
		res, err := BulkImportCSV(tenantID, AcquirerSettlementDoctype, strings.NewReader(file), "acct1", RoleSuperAdmin, false)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if len(res.Errors) != 0 || len(res.CreatedIDs) != want {
			t.Fatalf("import: created %v, errors %+v; want %d rows", res.CreatedIDs, res.Errors, want)
		}
	}
	load(bharatPe, 2)
	load(phonePe, 2)

	res, err := MatchAcquirerSettlements(tenantID, "acct1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 1 || len(res.Exceptions) != 0 {
		t.Fatalf("match = %+v; want one payout of four payments matched", res)
	}
	reasons := map[string]string{}
	for _, e := range res.BillExceptions {
		reasons[fmt.Sprint(e["payment_ref"])] = fmt.Sprint(e["reason"])
	}
	if len(reasons) != 2 || !strings.Contains(reasons["631877770000"], "took 500.00") || !strings.Contains(reasons["631866660000"], "no till bill") {
		t.Fatalf("bill exceptions = %v; want the 450 against a 500 bill and the 99 with no bill", reasons)
	}
	var bill, check, status, line string
	_ = db.DB.QueryRow("SELECT data->>'bill', data->>'bill_check', status, data->>'bank_line' FROM "+schema+".documents WHERE doctype = $1 AND data->>'payment_ref' = '631899990000'",
		AcquirerSettlementDoctype).Scan(&bill, &check, &status, &line)
	if bill != "CART-U2" || check != "OK" || status != "Matched" || line != "BSL-185" {
		t.Fatalf("split UPI part's row: bill %q check %q status %q line %q", bill, check, status, line)
	}
	// 2,000 taken less the payout and its charge (1,994 + 5): 1.00 is left.
	var clearing int64
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit) - SUM(credit), 0) FROM " + schema + ".gl_postings WHERE account_code = '1102'").Scan(&clearing)
	if clearing != 100 {
		t.Fatalf("UPI clearing = %d paise, want 100", clearing)
	}

	load(bharatPe, 2)
	res, err = MatchAcquirerSettlements(tenantID, "acct1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 0 || len(res.Exceptions) != 2 || !strings.Contains(fmt.Sprint(res.Exceptions[0]["reason"]), "same UPI ref") {
		t.Fatalf("the file again: %+v; want both rows refused as repeats", res)
	}
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit) - SUM(credit), 0) FROM " + schema + ".gl_postings WHERE account_code = '1102'").Scan(&clearing)
	if clearing != 100 {
		t.Fatalf("UPI clearing after the repeat = %d paise, want 100 still", clearing)
	}
}
