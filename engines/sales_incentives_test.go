package engines

import (
	"encoding/json"
	"fmt"
	"testing"

	"custom_erp/db"
)

// Stage 148.30: sales incentives (module sales_incentives, off by default).
// Every figure below is read from the journal (gl_postings) or the ledger
// table, never from a status.

type incFixture struct {
	*creditFixture
}

func newIncFixture(t *testing.T) *incFixture {
	f := &incFixture{newCreditFixture(t)}
	f.seed("ITEM-INC-A", "Item", "Active", map[string]interface{}{"code": "ITEM-INC-A", "name": "Shirt", "sale_price": 100.0, "standard_cost": 60.0, "category": "CAT-A", "hsn_code": "6205", "tax_treatment": "Exempt"})
	f.seed("ITEM-INC-B", "Item", "Active", map[string]interface{}{"code": "ITEM-INC-B", "name": "Belt", "sale_price": 100.0, "standard_cost": 90.0, "category": "CAT-B", "hsn_code": "6205", "tax_treatment": "Exempt"})
	if _, err := PostInventoryLedger(f.tenantID, "HO", []interface{}{
		map[string]interface{}{"sku": "ITEM-INC-A", "qty": 50.0}, map[string]interface{}{"sku": "ITEM-INC-B", "qty": 50.0}}, false); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"SM-A", "SM-B"} {
		f.seed(s, "Salesman", "Active", map[string]interface{}{"name": "Salesman " + s, "store": "HO", "status": "Active"})
	}
	return f
}

func (f *incFixture) on() {
	f.t.Helper()
	if err := SetModuleEntitlement(f.tenantID, ModuleSalesIncentives, true, "test"); err != nil {
		f.t.Fatalf("SetModuleEntitlement: %v", err)
	}
}

func (f *incFixture) rule(id, category string, minMargin, pct float64) {
	f.seed(id, "IncentiveRule", "Active", map[string]interface{}{"category": category, "min_margin_pct": minMargin, "pct": pct, "status": "Active"})
}

// invoice seeds a Draft SalesInvoice of one line ex-tax rate*qty and posts it.
func (f *incFixture) invoice(id, sku string, qty int, rate float64, extra map[string]interface{}) {
	f.t.Helper()
	d := map[string]interface{}{"code": id, "customer": "", "location": "HO", "total_amount": rate * float64(qty),
		"items": []map[string]interface{}{{"sku": sku, "qty": qty, "rate": rate, "gst_rate": 0, "amount": rate * float64(qty)}}}
	for k, v := range extra {
		d[k] = v
	}
	f.seed(id, "SalesInvoice", "Draft", d)
	if _, err := PostSalesInvoice(f.tenantID, id, "system"); err != nil {
		f.t.Fatalf("PostSalesInvoice %s: %v", id, err)
	}
}

func (f *incFixture) entries(source string) map[string]int64 { // salesman -> signed paise
	rows, err := db.DB.Query(`SELECT salesman_id, COALESCE(SUM(amount_paise),0) FROM `+f.schema+`.sales_incentive_entries WHERE source_id = $1 GROUP BY salesman_id`, source)
	if err != nil {
		f.t.Fatalf("entries: %v", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var s string
		var v int64
		_ = rows.Scan(&s, &v)
		out[s] = v
	}
	return out
}

func (f *incFixture) journal(docType, docID, account string) (dr, cr int64) {
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM `+f.schema+`.gl_postings WHERE document_type = $1 AND document_id = $2 AND account_code = $3`,
		docType, docID, account).Scan(&dr, &cr)
	return
}

func TestStage14830OffMeansOff(t *testing.T) {
	f := newIncFixture(t)
	f.rule("INC-R1", "CAT-A", 0, 10)
	f.invoice("INC-OFF-1", "ITEM-INC-A", 2, 100, map[string]interface{}{"salesman_id": "SM-A"})
	if got := f.entries("INC-OFF-1"); len(got) != 0 {
		t.Fatalf("module off but entries were written: %v", got)
	}
	if dr, cr := f.journal("SalesInvoice", "INC-OFF-1", "5500"); dr != 0 || cr != 0 {
		t.Fatalf("module off but incentive expense posted: %d/%d", dr, cr)
	}
	meta, err := GetDocTypeMeta(f.tenantID, "SalesInvoice")
	if err != nil {
		t.Fatal(err)
	}
	for _, fl := range meta {
		if fl.Fieldname == "salesman_id" || fl.Fieldname == "salesman_split" {
			t.Fatalf("module off but the screen would show %s", fl.Fieldname)
		}
	}
	f.on()
	meta, _ = GetDocTypeMeta(f.tenantID, "SalesInvoice")
	found := false
	for _, fl := range meta {
		found = found || fl.Fieldname == "salesman_id"
	}
	if !found {
		t.Fatal("module on but salesman_id is not in the invoice meta")
	}
}

func TestStage14830AccrualSplitClawbackAndPayout(t *testing.T) {
	f := newIncFixture(t)
	f.on()
	// Two slabs for CAT-A: 10% from any margin, 20% from 30% margin. Sold at
	// 100 against cost 60: margin 40% -> the 20% slab.
	f.rule("INC-R1", "CAT-A", 0, 10)
	f.rule("INC-R2", "CAT-A", 30, 20)
	f.seed("INC-P-A", "IncentiveProfile", "Active", map[string]interface{}{"salesman_id": "SM-A", "user": "sma", "employee_id": "INC-EMP-A"})
	f.seed("INC-P-B", "IncentiveProfile", "Active", map[string]interface{}{"salesman_id": "SM-B", "user": "smb", "employee_id": "INC-EMP-B"})

	f.invoice("INC-INV-1", "ITEM-INC-A", 4, 100, map[string]interface{}{
		"salesman_split": []map[string]interface{}{{"salesman_id": "SM-A", "pct": 60}, {"salesman_id": "SM-B", "pct": 40}}})
	// margin = 400 - 240 = 160 rupees; 20% = 32 rupees = 3200 paise; 60/40 = 1920 / 1280
	got := f.entries("INC-INV-1")
	if got["SM-A"] != 1920 || got["SM-B"] != 1280 {
		t.Fatalf("split accrual = %v, want SM-A 1920, SM-B 1280", got)
	}
	if dr, _ := f.journal("SalesIncentive", "INC-INV-1", "5500"); dr != 3200 {
		t.Fatalf("expense debit = %d, want 3200", dr)
	}
	if _, cr := f.journal("SalesIncentive", "INC-INV-1", "2400"); cr != 3200 {
		t.Fatalf("accrued credit = %d, want 3200", cr)
	}

	t.Run("replay posts nothing twice", func(t *testing.T) {
		if err := AccrueSaleIncentives(f.tenantID, "SalesInvoice", "INC-INV-1"); err != nil {
			t.Fatal(err)
		}
		if got := f.entries("INC-INV-1"); got["SM-A"] != 1920 || got["SM-B"] != 1280 {
			t.Fatalf("replay changed entries: %v", got)
		}
		if dr, _ := f.journal("SalesIncentive", "INC-INV-1", "5500"); dr != 3200 {
			t.Fatalf("replay changed the journal: %d", dr)
		}
	})

	t.Run("a rule change never recomputes past accruals", func(t *testing.T) {
		f.rule("INC-R3", "CAT-A", 50, 90)
		_ = AccrueSaleIncentives(f.tenantID, "SalesInvoice", "INC-INV-1")
		if got := f.entries("INC-INV-1"); got["SM-A"] != 1920 {
			t.Fatalf("rule change moved a past accrual: %v", got)
		}
		if _, err := db.DB.Exec(`UPDATE ` + f.schema + `.documents SET status = 'Inactive' WHERE id = 'INC-R3'`); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("payout through the payslip, then a clawback after payout is carried", func(t *testing.T) {
		f.seed("INC-EMP-A", "Employee", "Active", map[string]interface{}{"name": "Emp A", "bank_account_no": "0001"})
		f.seed("INC-SAL-A", "SalaryStructure", "Active", map[string]interface{}{"employee_id": "INC-EMP-A", "basic": 15000, "hra": 0, "other_allowances": 0, "pf_percent": 0, "esi_percent": 0, "pt_amount": 0, "effective_from": "2026-01-01"})
		today := BusinessToday()
		from, to := today[:8]+"01", today
		f.seed("INC-ATT-A", "Attendance", "Active", map[string]interface{}{"employee_id": "INC-EMP-A", "date": today, "status": "Present"})
		slip, err := RunPayroll(f.tenantID, "INC-EMP-A", from, to, "system")
		if err != nil {
			t.Fatalf("RunPayroll: %v", err)
		}
		if got := f.field("Payslip", slip, "incentive_pay"); got != "19.2" {
			t.Fatalf("payslip incentive_pay = %q, want 19.2", got)
		}
		if err := PostPayslipToGL(f.tenantID, slip, "system"); err != nil {
			t.Fatalf("PostPayslipToGL: %v", err)
		}
		if dr, _ := f.journal("Payslip", slip, "2400"); dr != 1920 {
			t.Fatalf("payslip should debit accrued incentives 1920, got %d", dr)
		}
		var paid int
		_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+f.schema+`.sales_incentive_entries WHERE paid_payslip = $1`, slip).Scan(&paid)
		if paid != 1 {
			t.Fatalf("%d entries marked paid, want 1", paid)
		}

		// The customer returns 2 of the 4 after SM-A was paid.
		if err := ClawbackSaleIncentives(f.tenantID, "INC-INV-1", "RET-1", map[string]int{"ITEM-INC-A": 2}); err != nil {
			t.Fatal(err)
		}
		got := f.entries("INC-INV-1")
		if got["SM-A"] != 960 || got["SM-B"] != 640 {
			t.Fatalf("after clawback net = %v, want SM-A 960, SM-B 640", got)
		}
		if dr, cr := f.journal("SalesIncentive", "INC-INV-1", "5500"); dr != 3200 || cr != 1600 {
			t.Fatalf("clawback journal expense dr/cr = %d/%d, want 3200/1600", dr, cr)
		}
		// Replay of the same return claws nothing more.
		_ = ClawbackSaleIncentives(f.tenantID, "INC-INV-1", "RET-1", map[string]int{"ITEM-INC-A": 2})
		if got := f.entries("INC-INV-1"); got["SM-A"] != 960 {
			t.Fatalf("clawback replay changed entries: %v", got)
		}
		// SM-A's unpaid balance is the negative 960, carried, not lost.
		if bal := IncentiveUnpaidPaise(f.tenantID, "SM-A"); bal != -960 {
			t.Fatalf("SM-A carried balance = %d, want -960", bal)
		}
		// A later sale of 1000 paise nets against it at the next payout.
		f.invoice("INC-INV-2", "ITEM-INC-A", 1, 100, map[string]interface{}{"salesman_id": "SM-A"})
		if bal := IncentiveUnpaidPaise(f.tenantID, "SM-A"); bal != -960+800 {
			t.Fatalf("SM-A balance after a new sale = %d, want -160", bal)
		}
	})

	t.Run("cancelling an invoice claws back what is left", func(t *testing.T) {
		f.invoice("INC-INV-3", "ITEM-INC-A", 2, 100, map[string]interface{}{"salesman_id": "SM-B"})
		if got := f.entries("INC-INV-3"); got["SM-B"] != 1600 { // margin 80 * 20% = 16
			t.Fatalf("accrual = %v", got)
		}
		if err := CancelSalesInvoice(f.tenantID, "INC-INV-3", "system", "wrong bill"); err != nil {
			t.Fatalf("CancelSalesInvoice: %v", err)
		}
		if got := f.entries("INC-INV-3"); got["SM-B"] != 0 {
			t.Fatalf("after cancel net = %v, want 0", got)
		}
	})
}

func TestStage14830PosSaleAccruesFromItsAllocation(t *testing.T) {
	f := newIncFixture(t)
	f.on()
	f.rule("INC-R1", "CAT-B", 0, 50)
	alloc, _ := json.Marshal([]map[string]interface{}{{"sku": "ITEM-INC-B", "qty": 1, "gross_paise": 11800, "offer_paise": 1800, "net_paise": 10000}})
	costs, _ := json.Marshal([]map[string]interface{}{{"sku": "ITEM-INC-B", "qty": 1, "value_paise": 9000}})
	// net 10000 incl 18% tax -> ex-tax 8474 (taxable 100/total 118 ratio); cost 9000 -> no margin
	f.seed("CART-INC-1", "POSCart", "Paid", map[string]interface{}{"salesman_id": "SM-A", "location": "HO",
		"allocation": json.RawMessage(alloc), "sale_costs": json.RawMessage(costs),
		"gst_breakdown": map[string]interface{}{"taxable_amount": 100.0, "total_amount": 118.0}})
	if err := AccrueSaleIncentives(f.tenantID, "POSCart", "CART-INC-1"); err != nil {
		t.Fatal(err)
	}
	if got := f.entries("CART-INC-1"); len(got) != 0 {
		t.Fatalf("a sale below cost accrued %v", got)
	}
	// Same cart with a cost of 5000: ex-tax 8474 - 5000 = 3475 margin -> 50% = 1738 (rounded).
	costs2, _ := json.Marshal([]map[string]interface{}{{"sku": "ITEM-INC-B", "qty": 1, "value_paise": 5000}})
	f.seed("CART-INC-2", "POSCart", "Paid", map[string]interface{}{"salesman_id": "SM-A", "location": "HO",
		"allocation": json.RawMessage(alloc), "sale_costs": json.RawMessage(costs2),
		"gst_breakdown": map[string]interface{}{"taxable_amount": 100.0, "total_amount": 118.0}})
	if err := AccrueSaleIncentives(f.tenantID, "POSCart", "CART-INC-2"); err != nil {
		t.Fatal(err)
	}
	if got := f.entries("CART-INC-2"); got["SM-A"] != 1738 {
		t.Fatalf("POS accrual = %v, want SM-A 1738 (%s)", got, fmt.Sprint(8475-5000))
	}
}

func TestStage14830BadSplitIsRefusedAndRecorded(t *testing.T) {
	f := newIncFixture(t)
	f.on()
	f.rule("INC-R1", "CAT-A", 0, 10)
	f.invoice("INC-INV-BAD", "ITEM-INC-A", 1, 100, map[string]interface{}{
		"salesman_split": []map[string]interface{}{{"salesman_id": "SM-A", "pct": 70}, {"salesman_id": "SM-B", "pct": 40}}})
	// The sale still posted (invoice is Approved) and no entry was written.
	if st := f.creditFixture.field("SalesInvoice", "INC-INV-BAD", "status"); st != "Approved" {
		t.Fatalf("a bad split blocked the sale: status %q", st)
	}
	if got := f.entries("INC-INV-BAD"); len(got) != 0 {
		t.Fatalf("a split of 110%% accrued %v", got)
	}
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + f.schema + `.system_error_logs WHERE module_source = 'sales_incentives'`).Scan(&n)
	if n == 0 {
		t.Fatal("the failed accrual was not recorded as a system error")
	}
}

// A line already clawed back must not use up the returned units meant for the
// next line of the same SKU.
func TestStage14830ClawbackAcrossLinesOfOneSKU(t *testing.T) {
	f := newIncFixture(t)
	f.on()
	f.rule("INC-R1", "CAT-A", 0, 20)
	// Two lines of one SKU: 2 at Rs 100 each. Margin Rs 80 a line, 20% = 1600 paise a line.
	f.invoice("INC-TWO", "ITEM-INC-A", 2, 100, map[string]interface{}{"salesman_id": "SM-A", "total_amount": 400.0,
		"items": []map[string]interface{}{
			{"sku": "ITEM-INC-A", "qty": 2, "rate": 100, "gst_rate": 0, "amount": 200},
			{"sku": "ITEM-INC-A", "qty": 2, "rate": 100, "gst_rate": 0, "amount": 200}}})
	if got := f.entries("INC-TWO"); got["SM-A"] != 3200 {
		t.Fatalf("accrual %v, want 3200", got)
	}
	if err := ClawbackSaleIncentives(f.tenantID, "INC-TWO", "RET-1", map[string]int{"ITEM-INC-A": 2}); err != nil {
		t.Fatal(err)
	}
	if got := f.entries("INC-TWO"); got["SM-A"] != 1600 {
		t.Fatalf("after the first return %v, want 1600", got)
	}
	if err := ClawbackSaleIncentives(f.tenantID, "INC-TWO", "RET-2", map[string]int{"ITEM-INC-A": 2}); err != nil {
		t.Fatal(err)
	}
	if got := f.entries("INC-TWO"); got["SM-A"] != 0 {
		t.Fatalf("after the second return %v, want 0: every unit came back", got)
	}
	if dr, cr := f.journal("SalesIncentive", "INC-TWO", "5500"); dr != 3200 || cr != 3200 {
		t.Fatalf("expense dr/cr %d/%d, want 3200/3200", dr, cr)
	}

	// Paise rounding: 3 units, returned 2 then 1, leaves nothing and takes no more.
	f.invoice("INC-THREE", "ITEM-INC-A", 3, 100, map[string]interface{}{"salesman_id": "SM-B"})
	if got := f.entries("INC-THREE"); got["SM-B"] != 2400 {
		t.Fatalf("accrual %v, want 2400", got)
	}
	_ = ClawbackSaleIncentives(f.tenantID, "INC-THREE", "R-A", map[string]int{"ITEM-INC-A": 2})
	_ = ClawbackSaleIncentives(f.tenantID, "INC-THREE", "R-B", map[string]int{"ITEM-INC-A": 1})
	if got := f.entries("INC-THREE"); got["SM-B"] != 0 {
		t.Fatalf("after 2 then 1 %v, want 0", got)
	}
}

// A return credited by a credit note claws back when the note posts, once; the
// refund of the same return leaves those units alone; a price-only note takes nothing.
func TestStage14830CreditNoteClawsBack(t *testing.T) {
	f := newIncFixture(t)
	f.on()
	f.rule("INC-R1", "CAT-A", 0, 20)
	f.invoice("INC-CRN", "ITEM-INC-A", 4, 100, map[string]interface{}{"salesman_id": "SM-A"})
	if got := f.entries("INC-CRN"); got["SM-A"] != 3200 {
		t.Fatalf("accrual %v, want 3200", got)
	}
	f.seed("RR-CRN", "ReturnRequest", "QC Complete", map[string]interface{}{"status": "QC Complete", "original_order_id": "INC-CRN", "items": []map[string]interface{}{
		{"sku": "ITEM-INC-A", "qty": 1, "disposition": "Sellable", "next_action": returnActionCredit}}})
	// The refund of a return whose only line went to a credit note claws nothing.
	ClawbackReturnIncentives(f.tenantID, "INC-CRN", "RF-CRN", "RR-CRN")
	if got := f.entries("INC-CRN"); got["SM-A"] != 3200 {
		t.Fatalf("after the refund %v, want 3200 untouched", got)
	}
	// A price correction with no return: nothing.
	f.seed("CN-PRICE", "CreditNote", "Draft", map[string]interface{}{"code": "CN-PRICE", "amount": 10.0, "sales_invoice_id": "INC-CRN"})
	if _, err := PostCreditNote(f.tenantID, "CN-PRICE", "system"); err != nil {
		t.Fatalf("post price note: %v", err)
	}
	if got := f.entries("INC-CRN"); got["SM-A"] != 3200 {
		t.Fatalf("after a price note %v, want 3200 untouched", got)
	}
	// The return's note: 1 of 4 units, 800 paise back.
	f.seed("CN-RET", "CreditNote", "Draft", map[string]interface{}{"code": "CN-RET", "amount": 100.0, "sales_invoice_id": "INC-CRN", "return_request_id": "RR-CRN",
		"items": []map[string]interface{}{{"sku": "ITEM-INC-A", "qty": 1}}})
	if _, err := PostCreditNote(f.tenantID, "CN-RET", "system"); err != nil {
		t.Fatalf("post return note: %v", err)
	}
	if got := f.entries("INC-CRN"); got["SM-A"] != 2400 {
		t.Fatalf("after the return note %v, want 2400", got)
	}
}

// A line's own taxable value is the sale, when it has one (a free-text or discounted line).
func TestStage14830NetReadsTaxable(t *testing.T) {
	f := newIncFixture(t)
	f.on()
	f.rule("INC-R1", "CAT-A", 0, 10)
	f.rule("INC-R2", "CAT-A", 30, 20)
	// Rate says Rs 400, taxable says Rs 320. Cost 240: margin Rs 80 = 25%, the 10% slab: 800 paise.
	f.invoice("INC-TAX", "ITEM-INC-A", 4, 100, map[string]interface{}{"salesman_id": "SM-A",
		"items": []map[string]interface{}{{"sku": "ITEM-INC-A", "qty": 4, "rate": 100, "taxable": 320, "gst_rate": 0, "amount": 320}}, "total_amount": 320.0})
	if got := f.entries("INC-TAX"); got["SM-A"] != 800 {
		t.Fatalf("accrual %v, want 800", got)
	}
}
