package engines

import (
	"custom_erp/db"
	"encoding/json"
	"math"
	"testing"
	"time"
)

// Stage 75.3.6 Wave B on known data: a part-paid sales invoice with its
// postings and receipt, a journal voucher on a cost center, a goods receipt
// still owed for, till bills at two stores and one recorded MRP edit - and
// the ledger, ageing, ranking and audit figures that follow.
func TestCatalogWaveBFigures(t *testing.T) {
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
	doc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		b, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'admin')", id, doctype, b, status)
	}
	gl := func(acct string, debit, credit int64, docType, docID, key, cc, dept string) {
		t.Helper()
		exec("INSERT INTO "+schema+".gl_postings (account_code, debit, credit, document_type, document_id, idempotency_key, cost_center, department) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''))",
			acct, debit, credit, docType, docID, key, cc, dept)
	}
	doc("S1", "Location", "Active", map[string]interface{}{"code": "S1", "name": "Jayanagar"})
	doc("S2", "Location", "Active", map[string]interface{}{"code": "S2", "name": "Indiranagar"})
	doc("C1", "Customer", "Active", map[string]interface{}{"code": "C1", "name": "Asha Rao"})
	doc("V1", "Vendor", "Active", map[string]interface{}{"code": "V1", "name": "Silver Works"})
	doc("CC1", "CostCenter", "Active", map[string]interface{}{"code": "CC1", "name": "Jayanagar Store"})
	doc("I1", "Item", "Active", map[string]interface{}{"code": "I1", "name": "Anarkali Red M", "mrp": 1050, "gst_rate": 5})
	due := time.Now().AddDate(0, 0, -40).Format("2006-01-02")
	doc("SI1", "SalesInvoice", "Approved", map[string]interface{}{"invoice_number": "SI/S1/0001", "customer_id": "C1", "customer": "Asha Rao",
		"location": "S1", "total_amount": 1180, "taxable_amount": 1000, "cgst": 90, "sgst": 90, "total_tax": 180, "outstanding_amount": 680,
		"paid_amount": 500, "due_date": due})
	gl("1300", 118000, 0, "SalesInvoice", "SI1", "SalesInvoice:SI1:POST", "", "")
	gl("4100", 0, 100000, "SalesInvoice", "SI1", "SalesInvoice:SI1:POST", "", "")
	gl("2200", 0, 9000, "SalesInvoice", "SI1", "SalesInvoice:SI1:POST", "", "")
	gl("2201", 0, 9000, "SalesInvoice", "SI1", "SalesInvoice:SI1:POST", "", "")
	gl("1100", 50000, 0, "SalesInvoice", "SI1", "SalesInvoice:SI1:SETTLE:UTR77", "", "")
	gl("1300", 0, 50000, "SalesInvoice", "SI1", "SalesInvoice:SI1:SETTLE:UTR77", "", "")
	doc("J1", "JournalVoucher", "Posted", map[string]interface{}{"voucher_number": "JV/S1/0001", "cost_center": "CC1", "narration": "Staff welfare",
		"lines": `[{"account_code":"5400","debit":200},{"account_code":"1100","credit":200}]`})
	gl("5400", 20000, 0, "JournalVoucher", "J1", "JV:J1", "CC1", "")
	gl("1100", 0, 20000, "JournalVoucher", "J1", "JV:J1", "", "")
	doc("G1", "GRN", "Approved", map[string]interface{}{"code": "GRN/S1/0001", "vendor": "V1", "location": "S1"})
	gl("1200", 300000, 0, "GRN", "G1", "GRN:G1", "", "")
	gl("2100", 0, 300000, "GRN", "G1", "GRN:G1", "", "")
	doc("B1", "POSCart", "Paid", map[string]interface{}{"location": "S1", "bill_number": "S1/0001", "amount_paid": 4140,
		"items": []map[string]interface{}{{"sku": "I1", "qty": 4, "sale_price": 1035, "net_total": 4140}}})
	doc("B2", "POSCart", "Paid", map[string]interface{}{"location": "S2", "bill_number": "S2/0001", "amount_paid": 1050,
		"items": []map[string]interface{}{{"sku": "I1", "qty": 1, "sale_price": 1050, "net_total": 1050}}})
	exec("INSERT INTO " + schema + `.audit_logs (user_id, action, status, details) VALUES ('admin', 'UPDATE_Item', 'SUCCESS', 'Field "mrp" modified from "1000" to "1050" for Document ID: I1')`)

	run := func(id string) []map[string]interface{} {
		t.Helper()
		rows, err := reportRegistry[id].Run(tenantID, map[string]string{})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return rows
	}
	near := func(what string, got interface{}, want float64) {
		t.Helper()
		if math.Abs(catNum(got)-want) > 0.011 {
			t.Errorf("%s = %v, want %.2f", what, got, want)
		}
	}
	is := func(what string, got interface{}, want string) {
		t.Helper()
		if catStr(got) != want {
			t.Errorf("%s = %q, want %q", what, catStr(got), want)
		}
	}
	byKey := func(rows []map[string]interface{}, key, val string) map[string]interface{} {
		for _, r := range rows {
			if catStr(r[key]) == val {
				return r
			}
		}
		return nil
	}

	tb := byKey(run("cat-trial-balance"), "gl_account", "1300 Accounts Receivable")
	if tb == nil {
		t.Fatal("trial balance: no 1300 row")
	}
	near("1300 debit", tb["debit_amount"], 1180)
	near("1300 credit", tb["credit_amount"], 500)
	near("1300 balance", tb["balance"], 680)

	ar := run("cat-ar-aging")
	if len(ar) != 1 {
		t.Fatalf("AR aging: %d rows", len(ar))
	}
	near("AR outstanding", ar[0]["outstanding_amount"], 680)
	is("AR bucket", ar[0]["aging_bucket"], "31-60 days")
	is("AR customer", ar[0]["customer_name"], "Asha Rao")
	arTot, err := reportControlTotals["cat-ar-aging"](tenantID, map[string]string{}, ar)
	if err != nil || !arTot.Tied {
		t.Errorf("AR aging should tie to 1300: %v %+v", err, arTot)
	}

	rec := run("cat-receipt-register")
	if len(rec) != 1 {
		t.Fatalf("receipts: %d", len(rec))
	}
	near("receipt", rec[0]["net_amount"], 500)
	is("receipt ref", rec[0]["reference_no"], "UTR77")
	is("receipt customer", rec[0]["customer_name"], "Asha Rao")

	pl := run("cat-profit-and-loss-statement")
	near("revenue net", byKey(pl, "gl_account", "4100 Sales Revenue Account")["net_amount"], 1000)
	ccpl := byKey(run("cat-cost-center-p-and-l"), "cost_center", "Jayanagar Store")
	if ccpl == nil {
		t.Fatal("cost center P&L: no Jayanagar Store row")
	}
	near("staff expense on CC1", ccpl["net_amount"], -200)

	jv := run("cat-journal-voucher-register")
	if len(jv) != 2 {
		t.Fatalf("JV lines: %d", len(jv))
	}
	near("JV debit", byKey(jv, "gl_account", "5400 Employee Expense Account")["debit_amount"], 200)

	ap := run("cat-ap-aging")
	if len(ap) != 1 {
		t.Fatalf("AP aging: %d rows %v", len(ap), ap)
	}
	near("AP outstanding", ap[0]["outstanding_amount"], 3000)
	is("AP vendor", ap[0]["vendor_name"], "Silver Works")
	is("AP status", ap[0]["status"], "Received, not billed")
	apTot, err := reportControlTotals["cat-ap-aging"](tenantID, map[string]string{}, ap)
	if err != nil || !apTot.Tied {
		t.Errorf("AP aging should tie to 2100: %v %+v", err, apTot)
	}

	led := run("cat-customer-ledger")
	if len(led) != 2 {
		t.Fatalf("customer ledger: %d rows", len(led))
	}
	near("customer balance after receipt", led[len(led)-1]["balance"], 680)

	bs := run("cat-balance-sheet")
	near("2100 owed", byKey(bs, "gl_account", "2100 GRN Suspense Account")["balance"], 3000)

	rank := run("cat-store-performance-ranking")
	s1 := byKey(rank, "store_code", "S1")
	if s1 == nil {
		t.Fatalf("ranking: no S1 in %v", rank)
	}
	near("S1 rank", s1["rank"], 1)
	near("S1 ABV", s1["abv"], 4140)
	near("S1 UPT", s1["upt"], 4)
	near("S2 rank", byKey(rank, "store_code", "S2")["rank"], 2)

	mrp := run("cat-mrp-change-register")
	if len(mrp) != 1 {
		t.Fatalf("MRP changes: %d", len(mrp))
	}
	near("old MRP", mrp[0]["mrp"], 1000)
	near("new MRP", mrp[0]["new_value"], 1050)
	is("variant", mrp[0]["combination_id"], "I1")
	if ch := run("cat-master-data-change-audit"); len(ch) != 1 || catStr(ch[0]["field_changed"]) != "mrp" {
		t.Errorf("master data change audit: %v", ch)
	}
}
