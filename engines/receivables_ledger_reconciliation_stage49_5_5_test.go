package engines

import (
	"encoding/json"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 49.5.5: the receivables sub-ledger balances against 1300 invoice by
// invoice. A posted invoice and a part-settled one agree and stay out of the
// exceptions; an invoice marked settled without its ledger entry, one
// cancelled by a plain save, one set to Approved without posting, and a
// journal straight to 1300 are each named with the difference and what it
// suggests - and nothing is corrected by reading.
func TestStage4955ReceivablesAgreeWithLedgerByInvoice(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	invoice := func(id string, total float64) {
		t.Helper()
		raw, _ := json.Marshal(map[string]interface{}{"invoice_number": id, "customer": "CUST-" + id, "total_amount": total})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Draft', 'system')", id, raw)
	}
	post := func(id string, total float64) {
		t.Helper()
		invoice(id, total)
		if _, err := PostSalesInvoice(tenantID, id, "system"); err != nil {
			t.Fatalf("PostSalesInvoice %s: %v", id, err)
		}
	}
	byDoc := func(rows []ReceivableLedgerRow) map[string]ReceivableLedgerRow {
		out := map[string]ReceivableLedgerRow{}
		for _, r := range rows {
			out[r.Document] = r
		}
		return out
	}

	post("INV-OK", 1180)
	post("INV-PART", 2000)
	if _, err := SettleSalesInvoiceAmount(tenantID, "INV-PART", "system", 500, "RCPT-1"); err != nil {
		t.Fatalf("part settlement: %v", err)
	}
	all, err := ReceivablesVsLedger(tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	got := byDoc(all)
	if r := got["INV-OK"]; r.Book != 1180 || r.Ledger != 1180 || r.Difference != 0 || r.Hint != "agrees" {
		t.Fatalf("a posted invoice agrees with its ledger: %+v", r)
	}
	if r := got["INV-PART"]; r.Book != 1500 || r.Ledger != 1500 || r.Postings != 2 {
		t.Fatalf("a part-settled invoice agrees at what is still owed, with its receipt's posting: %+v", r)
	}
	if exceptions, _ := ReceivablesVsLedger(tenantID, true); len(exceptions) != 0 {
		t.Fatalf("a balanced ledger has no exceptions: %+v", exceptions)
	}

	// Four ways the two drift apart, each written the way the gap really
	// happens: a hand edit, a plain save, a status set without posting, and
	// a journal.
	post("INV-MARKED", 1000)
	exec("UPDATE " + schema + ".documents SET data = data || '{\"outstanding_amount\": 400}' WHERE id = 'INV-MARKED'")
	post("INV-CANCEL", 750)
	exec("UPDATE " + schema + ".documents SET status = 'Cancelled' WHERE id = 'INV-CANCEL'")
	invoice("INV-NOPOST", 300)
	exec("UPDATE " + schema + ".documents SET status = 'Approved' WHERE id = 'INV-NOPOST'")
	if err := PostDoubleEntry(tenantID, "JournalVoucher", "JV-AR-1", map[string]int64{"1300": 25000}, map[string]int64{"4100": 25000}, "", "JV-AR-1:POST"); err != nil {
		t.Fatalf("journal to 1300: %v", err)
	}
	var glBefore int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".gl_postings").Scan(&glBefore); err != nil {
		t.Fatal(err)
	}

	exceptions, err := ReceivablesVsLedger(tenantID, true)
	if err != nil {
		t.Fatal(err)
	}
	got = byDoc(exceptions)
	if len(exceptions) != 4 {
		t.Fatalf("exactly the four drifted items are exceptions, got %+v", exceptions)
	}
	want := map[string]struct {
		book, ledger, diff float64
		hint               string
	}{
		"INV-MARKED": {400, 1000, -600, "ledger says more is owed"},
		"INV-CANCEL": {0, 750, -750, "is Cancelled but the ledger still carries"},
		"INV-NOPOST": {300, 0, 300, "nothing reached the ledger"},
		"JV-AR-1":    {0, 250, -250, "journal posted straight to receivables"},
	}
	for doc, w := range want {
		r, ok := got[doc]
		if !ok {
			t.Errorf("%s is not named: %+v", doc, exceptions)
			continue
		}
		if r.Book != w.book || r.Ledger != w.ledger || r.Difference != w.diff || !strings.Contains(r.Hint, w.hint) || r.Owner == "" {
			t.Errorf("%s: %+v, want book %.2f ledger %.2f difference %.2f hint ~%q and an owner", doc, r, w.book, w.ledger, w.diff, w.hint)
		}
	}
	if r := got["JV-AR-1"]; r.Source != "Ledger only: JournalVoucher" {
		t.Errorf("a 1300 posting no invoice accounts for says where it came from: %+v", r)
	}

	// The exceptions add up to the ageing's own control-total gap.
	var sum float64
	for _, r := range exceptions {
		sum += r.Difference
	}
	ct, err := receivablesAgeingControlTotals(tenantID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ct.LedgerTotal == nil || round2(ct.SummaryTotal-*ct.LedgerTotal) != round2(sum) {
		t.Errorf("by-invoice differences %.2f should explain the control totals' summary-vs-ledger gap (%+v)", sum, ct)
	}

	// Through the report catalog, and reading changes nothing.
	_, rows, _, err := RunReport(tenantID, "receivables-vs-ledger", "Admin", "", map[string]string{})
	if err != nil || len(rows) != 4 {
		t.Fatalf("report run: %d rows, %v", len(rows), err)
	}
	var glAfter int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".gl_postings").Scan(&glAfter); err != nil {
		t.Fatal(err)
	}
	if glAfter != glBefore {
		t.Errorf("the reconciliation posted something: %d -> %d GL rows", glBefore, glAfter)
	}
}
