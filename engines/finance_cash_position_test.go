package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Ask 176.1: treasury cash position - the week buckets, the cash and bank
// balances, and the running position after what is due in and out.

func TestStage1761CashPositionBuckets(t *testing.T) {
	asOf := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	for due, want := range map[string]string{
		"": "now", "bad": "now", "2026-11-01": "now", "2026-11-14": "now",
		"2026-11-15": "w1", "2026-11-21": "w1", "2026-11-22": "w2", "2026-11-28": "w2",
		"2026-11-29": "w3", "2026-12-05": "w3", "2026-12-06": "w4", "2026-12-12": "w4", "2026-12-13": "later",
	} {
		if got := cashPositionBucket(asOf, due); got != want {
			t.Errorf("due %q: want %s, got %s", due, want, got)
		}
	}
}

func TestStage1761CashPositionAddsDuesToTheCashBalance(t *testing.T) {
	f := g170Fix(t)
	today := BusinessToday()
	day, _ := time.Parse("2006-01-02", today)
	at := func(days int) string { return day.AddDate(0, 0, days).Format("2006-01-02") }

	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(fmt.Sprintf(q, f.schema), args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO %s.gl_accounts (account_code, account_name, account_type) VALUES ('1150', 'Test bank', 'Asset') ON CONFLICT DO NOTHING`)
	f.insert("BA-1", "BankAccount", "Active", "system", map[string]interface{}{"bank_name": "HDFC", "gl_account_code": "1150"})
	f.insert("BA-2", "BankAccount", "Inactive", "system", map[string]interface{}{"bank_name": "Closed", "gl_account_code": "1150"})
	post := func(account string, debit, credit int64, created string) {
		exec(`INSERT INTO %s.gl_postings (account_code, debit, credit, document_type, document_id, created_at) VALUES ($1, $2, $3, 'Test', 'T-1', $4::timestamptz)`,
			account, debit, credit, created)
	}
	post("1100", 500000, 0, at(-4)+" 10:00:00+05:30")
	post("1100", 0, 100000, at(6)+" 10:00:00+05:30") // after the day: not in the balance
	post("1150", 200000, 0, at(-1)+" 10:00:00+05:30")

	f.insert("SI-1", "SalesInvoice", "Approved", "system", map[string]interface{}{"total_amount": 1000.0, "due_date": at(4)})
	f.insert("SI-2", "SalesInvoice", "Approved", "system", map[string]interface{}{"total_amount": 900.0, "outstanding_amount": 300.0, "due_date": at(-13)})
	f.insert("SI-3", "SalesInvoice", "Draft", "system", map[string]interface{}{"total_amount": 5000.0, "due_date": at(4)})
	f.insert("VI-1", "VendorInvoice", "Approved", "system", map[string]interface{}{"invoice_amount": 2500.0, "due_date": at(16)})
	f.insert("VI-2", "VendorInvoice", "Paid", "system", map[string]interface{}{"invoice_amount": 9999.0, "due_date": at(16)})

	rows, err := CashPosition(f.tenantID, "")
	if err != nil {
		t.Fatal(err)
	}
	find := func(prefix string) map[string]interface{} {
		for _, r := range rows {
			if strings.HasPrefix(strField(r, "item"), prefix) {
				return r
			}
		}
		t.Fatalf("no row %q in %v", prefix, rows)
		return nil
	}
	if r := find("1100"); r["now"] != 5000.0 {
		t.Fatalf("1100 balance: %v", r)
	}
	if r := find("1150"); r["now"] != 2000.0 || !strings.Contains(strField(r, "item"), "(HDFC)") {
		t.Fatalf("bank account row: %v", r)
	}
	if r := find("Total cash"); r["now"] != 7000.0 {
		t.Fatalf("total cash: %v", r)
	}
	if r := find("Customer invoices"); r["now"] != 300.0 || r["w1"] != 1000.0 || r["total"] != 1300.0 {
		t.Fatalf("receivables: %v", r)
	}
	if r := find("Supplier bills"); r["w3"] != 2500.0 || r["total"] != 2500.0 {
		t.Fatalf("payables: %v", r)
	}
	pos := find("Cash after")
	for b, want := range map[string]float64{"now": 7300, "w1": 8300, "w2": 8300, "w3": 5800, "w4": 5800, "later": 5800, "total": 5800} {
		if pos[b] != want {
			t.Fatalf("position %s: want %v, got %v", b, want, pos)
		}
	}

	// As of a day before the invoices were raised, they are not counted.
	early, err := CashPosition(f.tenantID, at(-30))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range early {
		if r["item"] == "Customer invoices" && r["total"] != 0.0 {
			t.Fatalf("invoices raised later were counted: %v", r)
		}
	}
	if _, err := CashPosition(f.tenantID, "14/11/2026"); err == nil {
		t.Fatal("a bad as-of date was accepted")
	}
	// Through the runner the refusal stays the caller's mistake (GLOBAL-0002,
	// 422), not REPORT-0162 "generation failed" (503) - found by the act run.
	_, _, _, err = RunReportContext(context.Background(), f.tenantID, cashPositionReportID, RoleSuperAdmin, "", map[string]string{"as_of": "14-11-2026"})
	wantCode(t, err, "GLOBAL-0002")
}
