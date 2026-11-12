package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 140.12 - what Guide's whole-organisation run found.

func stage140Tenant(t *testing.T) (string, string, func(id, doctype, status, createdBy string, data map[string]interface{})) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status, createdBy string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, $5)", id, doctype, raw, status, createdBy); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	return tenantID, schema, insert
}

func stage140GL(t *testing.T, schema, docType, docID, account string) (debit, credit int64) {
	t.Helper()
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit), 0), COALESCE(SUM(credit), 0) FROM "+schema+".gl_postings WHERE document_type = $1 AND document_id = $2 AND account_code = $3",
		docType, docID, account).Scan(&debit, &credit)
	return
}

// 140.12.1: the till close and the X-report use one expected-cash formula,
// cash advances included.
func TestStage140TillCloseCountsAdvances(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	insert("SESS-140", "POSSession", "Open", "cashier1", map[string]interface{}{"location": "S140", "cashier": "cashier1", "opening_cash": 100.0, "status": "Open"})
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".customer_advances (instrument_id, customer_id, location, amount_paise, payment_mode, pos_session) VALUES ('ADV-140', 'C140', 'S140', 50000, 'Cash', 'SESS-140')"); err != nil {
		t.Fatal(err)
	}
	pos, err := SessionCashPosition(tenantID, "SESS-140")
	if err != nil {
		t.Fatal(err)
	}
	closeExpected, err := expectedCashForSession(tenantID, "SESS-140", 100)
	if err != nil {
		t.Fatal(err)
	}
	if pos.Expected != 600 || closeExpected != pos.Expected {
		t.Fatalf("X-report expects %.2f, close expects %.2f; want both 600 (float 100 + advance 500)", pos.Expected, closeExpected)
	}
}

// 140.12.2: an advance or store credit paying a bill is its own tender line,
// not "Gift Card".
func TestStage140TenderTotalsSplitStoredValue(t *testing.T) {
	cart := map[string]interface{}{
		"payment_mode": "Cash",
		"allocation":   []interface{}{map[string]interface{}{"tenders": map[string]interface{}{"Gift Card": 30000.0, "Cash": 5000.0}}},
		"gift_card_tenders": []interface{}{
			map[string]interface{}{"card": "Advance CA1", "amount_paise": 10000.0},
			map[string]interface{}{"card": "Store credit", "amount_paise": 5000.0},
			map[string]interface{}{"card": "GC ****1234", "amount_paise": 15000.0},
		},
	}
	got := cartTenderTotals(cart)
	if got["Advance"] != 100 || got["Store Credit"] != 50 || got["Gift Card"] != 150 || got["Cash"] != 50 {
		t.Fatalf("tender totals = %v; want Advance 100, Store Credit 50, Gift Card 150, Cash 50", got)
	}
}

// 140.12.3: an invoice line saved by the Sales Invoice screen (rate, gst_rate,
// amount) prices a return at what the customer was charged.
func TestStage140InvoiceLinePaidPrice(t *testing.T) {
	for _, c := range []struct {
		line salesInvoiceLine
		want float64
	}{
		{salesInvoiceLine{Qty: 2, UnitPrice: 299}, 299},
		{salesInvoiceLine{Qty: 2, Rate: 100, GSTRate: 5, Amount: 210}, 105},
		{salesInvoiceLine{Qty: 1, Rate: 100, GSTRate: 18}, 118},
	} {
		if got := c.line.paidUnitPrice(); got != c.want {
			t.Fatalf("%+v: paid unit price %.2f, want %.2f", c.line, got, c.want)
		}
	}
}

// 140.12.3 / 140.12.4: posting a credit invoice issues its stock at cost; a
// return against it reduces what the customer owes before anything is paid
// back; a store that is short refuses the posting and leaves no revenue.
func TestStage140CreditInvoiceStockAndReturn(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc = "S140"
	insert(loc, "Location", "Active", "system", map[string]interface{}{"code": loc, "name": "Store 140", "type": "Store", "sellable": "Yes"})
	insert("TEE140", "Item", "Active", "system", map[string]interface{}{"code": "TEE140", "name": "Tee", "sale_price": 100.0, "standard_cost": 40.0, "gst_rate": 5.0})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "TEE140", "qty": 5.0}}, false); err != nil {
		t.Fatal(err)
	}
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "TEE140", "qty": 2, "rate": 100, "gst_rate": 5, "taxable": 200, "tax": 10, "amount": 210}})
	invoice := map[string]interface{}{"customer": "C140", "location": loc, "items": string(items), "taxable_amount": 200.0, "cgst": 5.0, "sgst": 5.0, "total_amount": 210.0}
	insert("SI-140", "SalesInvoice", "Draft", "manager1", invoice)
	if _, err := PostSalesInvoice(tenantID, "SI-140", "manager1"); err != nil {
		t.Fatalf("post: %v", err)
	}
	if onHand, _, _ := stage135Stock(t, schema, "TEE140", loc); onHand != 3 {
		t.Fatalf("on hand after a 2-unit invoice = %d, want 3", onHand)
	}
	if d, _ := stage140GL(t, schema, "SalesInvoice", "SI-140", "5100"); d <= 0 {
		t.Fatalf("no cost of goods booked for the invoice")
	}
	stage135BookAgrees(t, tenantID, loc)

	// An invoice raised from an order's shipment already shipped its goods.
	shipped := map[string]interface{}{"sales_order_id": "SO-140"}
	for k, v := range invoice {
		shipped[k] = v
	}
	insert("SI-140-SYS", "SalesInvoice", "Draft", "system", shipped)
	if _, err := PostSalesInvoice(tenantID, "SI-140-SYS", "manager1"); err != nil {
		t.Fatalf("post system invoice: %v", err)
	}
	if onHand, _, _ := stage135Stock(t, schema, "TEE140", loc); onHand != 3 {
		t.Fatalf("a system invoice moved stock: on hand %d, want 3", onHand)
	}

	// Short store: refused, still Draft, no revenue.
	big, _ := json.Marshal([]map[string]interface{}{{"sku": "TEE140", "qty": 9, "rate": 100, "gst_rate": 5, "amount": 945}})
	insert("SI-140-BIG", "SalesInvoice", "Draft", "manager1", map[string]interface{}{"customer": "C140", "location": loc, "items": string(big), "total_amount": 945.0})
	if _, err := PostSalesInvoice(tenantID, "SI-140-BIG", "manager1"); err == nil || !strings.Contains(err.Error(), "only 3") {
		t.Fatalf("posting 9 with 3 on hand: want a shortage refusal, got %v", err)
	}
	if d, _ := stage140GL(t, schema, "SalesInvoice", "SI-140-BIG", "1300"); d != 0 {
		t.Fatalf("a refused invoice still booked receivable %d", d)
	}

	// A return of 1 unit is valued at 105 and taken off the 210 owed.
	prices, err := resolveOriginalSaleLinePrices(tenantID, "SI-140")
	if err != nil || prices["TEE140"].SalePrice != 105 {
		t.Fatalf("return price = %+v (%v), want 105", prices["TEE140"], err)
	}
	for i := 0; i < 2; i++ { // the second call replays
		tx, _ := db.DB.Begin()
		applied, err := applyReturnToSalesInvoiceTx(tx, tenantID, schema, "SI-140", "RF-140", "manager1", 10500)
		if err != nil || applied != 10500 {
			t.Fatalf("applied %d (%v), want 10500", applied, err)
		}
		_ = tx.Commit()
	}
	var outstanding float64
	_ = db.DB.QueryRow("SELECT (data->>'outstanding_amount')::numeric FROM " + schema + ".documents WHERE id = 'SI-140'").Scan(&outstanding)
	if outstanding != 105 {
		t.Fatalf("outstanding after the return = %.2f, want 105", outstanding)
	}
}

// 140.12.11: a closed till's variance reaches the GL once.
func TestStage140TillVariancePosts(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	insert("SESS-140-V", "POSSession", "Closed", "cashier1", map[string]interface{}{"location": "S140", "variance": -100.0, "status": "Closed"})
	for i := 0; i < 2; i++ {
		if err := postTillVariance(tenantID, "SESS-140-V"); err != nil {
			t.Fatal(err)
		}
	}
	if d, _ := stage140GL(t, schema, "POSSession", "SESS-140-V", AccountCashShortOver); d != 10000 {
		t.Fatalf("5295 debit = %d paise, want 10000 once", d)
	}
	if _, c := stage140GL(t, schema, "POSSession", "SESS-140-V", "1100"); c != 10000 {
		t.Fatalf("1100 credit = %d paise, want 10000", c)
	}
}

// 140.12.10: finance reports carry the finance screens' right.
func TestStage140FinanceReportsNeedFinanceRole(t *testing.T) {
	def := ReportDefinition{ID: "x", Label: "Trial Balance", ModuleKey: "finance"}
	if ReportAllowedForRole(def, "Cashier") {
		t.Fatal("a Cashier may run a finance report")
	}
	for _, role := range []string{RoleAdmin, "Accountant", "Accounts Receivable"} {
		if !ReportAllowedForRole(def, role) {
			t.Fatalf("%s may not run a finance report", role)
		}
	}
	if !ReportAllowedForRole(ReportDefinition{ModuleKey: "sales"}, "Cashier") {
		t.Fatal("a Cashier lost a sales report")
	}
}

// 140.12.15 / 140.12.19: a confirmed wave line is picked on the wave's task,
// and picked units are not waiting for put-away.
func TestStage140WavePickCountsOnTask(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc = "WH140"
	insert(loc, "Location", "Active", "system", map[string]interface{}{"code": loc, "name": "WH 140", "type": "Warehouse"})
	insert("BIN-140", "Bin", "Active", "system", map[string]interface{}{"code": "BIN-140", "location": loc})
	insert("TEE140", "Item", "Active", "system", map[string]interface{}{"code": "TEE140", "name": "Tee"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "TEE140", "qty": 5.0}}, false); err != nil {
		t.Fatal(err)
	}
	insert("FT-140", "FulfillmentTask", "Pending", "system", map[string]interface{}{"location_code": loc, "wave_id": "WAVE-140",
		"items": []interface{}{map[string]interface{}{"sku": "TEE140", "qty": 2}}})
	if err := ConfirmWavePickLine(tenantID, "WAVE-140", WavePickLineKey("TEE140", "BIN-140", ""), 2, "picker1"); err != nil {
		t.Fatal(err)
	}
	task, err := loadPickPackTask(schema, "FT-140")
	if err != nil {
		t.Fatal(err)
	}
	if task.status != "Picking" || task.items[0].PickedQty != 2 {
		t.Fatalf("after the wave confirm the task is %s with %d picked; want Picking, 2", task.status, task.items[0].PickedQty)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_allocation (task_id, sku, location_code, bin_code, batch_no, owner_id, qty, picked_qty, status) VALUES ('FT-140', 'TEE140', $1, 'BIN-140', '', '', 2, 2, 'Picked')", loc); err != nil {
		t.Fatal(err)
	}
	work, err := InboundWork(tenantID, loc)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range work.Putaway {
		if p.Sku == "TEE140" && p.Waiting != 3 {
			t.Fatalf("waiting for put-away = %v, want 3 (5 on hand - 2 picked)", p.Waiting)
		}
	}
}

// 140.12.5 / 140.12.6: one receipt across two invoices into a bank account,
// with TDS and an on-account remainder that becomes an advance; a later
// receipt pays an invoice from that advance alone.
func TestStage140CustomerReceipt(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	insert("C140", "Customer", "Active", "system", map[string]interface{}{"code": "C140", "name": "Receipt Customer", "phone": "9000000140"})
	insert("BANK-140", "BankAccount", "Active", "system", map[string]interface{}{"bank_name": "Test Bank", "account_number": "000140", "gl_account_code": "1100", "status": "Active"})
	for _, id := range []string{"SI-R1", "SI-R2", "SI-R3"} {
		insert(id, "SalesInvoice", "Draft", "system", map[string]interface{}{"customer": "C140", "location": "S140", "total_amount": 1000.0})
		if _, err := PostSalesInvoice(tenantID, id, "acct1"); err != nil {
			t.Fatalf("post %s: %v", id, err)
		}
	}
	if _, err := RecordCustomerReceipt(tenantID, "acct1", CustomerReceiptInput{Customer: "C140", Location: "S140", Mode: "Cheque", Amount: 1500}); err == nil {
		t.Fatal("a cheque without a bank account was taken")
	}
	rec, err := RecordCustomerReceipt(tenantID, "acct1", CustomerReceiptInput{Customer: "C140", Location: "S140", Mode: "Bank Transfer", BankAccount: "BANK-140",
		Amount: 2000, TDS: 100, Invoices: []ReceiptLine{{Ref: "SI-R1", Amount: 1000}, {Ref: "SI-R2", Amount: 600}}, IdempotencyKey: "k140"})
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if rec.Allocated != 1600 || rec.OnAccountAmt != 500 || rec.OnAccount == "" {
		t.Fatalf("receipt = %+v; want 1600 to invoices, 500 on account", rec)
	}
	again, err := RecordCustomerReceipt(tenantID, "acct1", CustomerReceiptInput{Customer: "C140", Location: "S140", Mode: "Bank Transfer", BankAccount: "BANK-140",
		Amount: 2000, TDS: 100, Invoices: []ReceiptLine{{Ref: "SI-R1", Amount: 1000}, {Ref: "SI-R2", Amount: 600}}, IdempotencyKey: "k140"})
	if err != nil || again.Number != rec.Number {
		t.Fatalf("a retry made a second receipt: %v %v", again, err)
	}
	if _, c := stage140GL(t, schema, "CustomerReceipt", rec.Number, "1300"); c != 160000 {
		t.Fatalf("1300 credit = %d, want 160000", c)
	}
	if d, _ := stage140GL(t, schema, "CustomerReceipt", rec.Number, AccountTDSReceivable); d != 10000 {
		t.Fatalf("TDS debit = %d, want 10000", d)
	}
	if _, c := stage140GL(t, schema, "CustomerAdvance", rec.OnAccount, AccountCustomerAdvLia); c != 50000 {
		t.Fatalf("on-account advance = %d, want 50000 in 2640", c)
	}
	var status string
	var outstanding float64
	_ = db.DB.QueryRow("SELECT status, (data->>'outstanding_amount')::numeric FROM "+schema+".documents WHERE id = 'SI-R2'").Scan(&status, &outstanding)
	if status != "Approved" || outstanding != 400 {
		t.Fatalf("SI-R2 is %s with %.2f outstanding; want Approved, 400", status, outstanding)
	}

	// The advance pays SI-R2's 400 with no new money.
	rec2, err := RecordCustomerReceipt(tenantID, "acct1", CustomerReceiptInput{Customer: "C140", Location: "S140", Mode: "Bank Transfer",
		Advances: []ReceiptLine{{Ref: rec.OnAccount, Amount: 400}}, Invoices: []ReceiptLine{{Ref: "SI-R2", Amount: 400}}})
	if err != nil {
		t.Fatalf("advance receipt: %v", err)
	}
	if d, _ := stage140GL(t, schema, "CustomerReceipt", rec2.Number, AccountCustomerAdvLia); d != 40000 {
		t.Fatalf("2640 debit = %d, want 40000", d)
	}
	_ = db.DB.QueryRow("SELECT status FROM " + schema + ".documents WHERE id = 'SI-R2'").Scan(&status)
	if status != "Paid" {
		t.Fatalf("SI-R2 is %s after the advance, want Paid", status)
	}
	// 140.12.7: the statement shows the advance as well as the invoices and
	// ends at what the customer owes net of what is held for them: SI-R3's
	// 1000 less the 100 of advance still held = 900.
	d := PrintDocument{Fields: map[string]interface{}{}}
	if _, err := buildStatement(tenantID, PrintTxnCustomerStatement, "C140", &d); err != nil {
		t.Fatal(err)
	}
	sawAdvance := false
	for _, l := range d.Lines {
		sawAdvance = sawAdvance || l["particulars"] == "Advance received"
	}
	if !sawAdvance || d.Fields["closing_balance"] != 900.0 {
		t.Fatalf("statement: advance shown %v, closing %v; want the advance and 900", sawAdvance, d.Fields["closing_balance"])
	}
	invoices, advances, err := CustomerOpenItems(tenantID, "C140")
	if err != nil || len(invoices) != 1 || invoices[0].ID != "SI-R3" || len(advances) != 1 || advances[0].Balance != 100 {
		t.Fatalf("open items = %+v / %+v (%v); want SI-R3 and the advance with 100 left", invoices, advances, err)
	}
}

// 140.12.13: the Day Book lists every voucher of the day and balances; the
// Z-report shows a closed till's drawer.
func TestStage140DayBookAndZReport(t *testing.T) {
	tenantID, _, insert := stage140Tenant(t)
	insert("SESS-140-Z", "POSSession", "Closed", "cashier1", map[string]interface{}{"location": "S140", "cashier": "cashier1", "opening_cash": 200.0,
		"expected_cash": 200.0, "closing_counted_cash": 150.0, "variance": -50.0, "status": "Closed"})
	if err := postTillVariance(tenantID, "SESS-140-Z"); err != nil {
		t.Fatal(err)
	}
	_, rows, _, err := RunReport(tenantID, "day-book", RoleAdmin, "admin", map[string]string{})
	if err != nil || len(rows) < 2 {
		t.Fatalf("day book = %d rows (%v), want the variance voucher's two lines", len(rows), err)
	}
	var dr, cr float64
	for _, r := range rows {
		dr += r["debit"].(float64)
		cr += r["credit"].(float64)
	}
	if dr != cr {
		t.Fatalf("day book debits %.2f != credits %.2f", dr, cr)
	}
	_, rows, _, err = RunReport(tenantID, "z-report", RoleAdmin, "admin", map[string]string{"store": "S140"})
	if err != nil || len(rows) != 1 || rows[0]["variance"] != -50.0 || rows[0]["opening"] != 200.0 {
		t.Fatalf("z-report = %+v (%v); want the one till with variance -50", rows, err)
	}
	if _, _, _, err := RunReport(tenantID, "day-book", "Cashier", "cashier1", map[string]string{}); err == nil {
		t.Fatal("a Cashier read the Day Book")
	}
	if _, _, _, err := RunReport(tenantID, "customer-receipts", RoleAdmin, "admin", map[string]string{}); err != nil {
		t.Fatalf("receipt register: %v", err)
	}
}

// 140.12.14: an open till or an offline sale not on the server holds the
// period close.
func TestStage140PeriodCloseLooksAtTills(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	today := BusinessToday()
	periodID, err := CreateAccountingPeriod(tenantID, "P140", today[:8]+"01", today, "acct1")
	if err != nil {
		t.Fatal(err)
	}
	failed := func() map[string]bool {
		t.Helper()
		c, err := GetPeriodCloseChecklist(tenantID, periodID)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, ch := range c.Checks {
			if !ch.Passed {
				out[ch.Name] = true
			}
		}
		return out
	}
	if f := failed(); f["No till open or awaiting approval"] || f["No offline sales waiting to sync"] {
		t.Fatalf("a clean period fails the till checks: %v", f)
	}
	insert("SESS-140-P", "POSSession", "Open", "cashier1", map[string]interface{}{"location": "S140", "status": "Open"})
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".pos_offline_heartbeats (session_id, cashier, location, cart_numbers) VALUES ('SESS-140-P', 'cashier1', 'S140', '[\"CART-LOST-140\"]')"); err != nil {
		t.Fatal(err)
	}
	if f := failed(); !f["No till open or awaiting approval"] || !f["No offline sales waiting to sync"] {
		t.Fatalf("an open till and a lost offline sale pass the checklist: %v", f)
	}
	if open, lost, err := periodTillBlockers(schema, today); err != nil || open != 1 || lost != 1 {
		t.Fatalf("blockers = %d open, %d unsynced (%v); want 1 and 1", open, lost, err)
	}
}

// 140.12.8: a posted invoice that took no money cancels with a full
// reversal; one with a receipt is refused; a credit note for an invoice is
// numbered in the invoice's store series.
func TestStage140CancelInvoiceAndNoteNumbering(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc = "S140C"
	insert(loc, "Location", "Active", "system", map[string]interface{}{"code": loc, "name": "Store 140C", "type": "Store", "sellable": "Yes"})
	insert("TEE140", "Item", "Active", "system", map[string]interface{}{"code": "TEE140", "name": "Tee", "standard_cost": 40.0})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "TEE140", "qty": 5.0}}, false); err != nil {
		t.Fatal(err)
	}
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "TEE140", "qty": 2, "rate": 100, "gst_rate": 5, "amount": 210}})
	for _, id := range []string{"SI-C1", "SI-C2"} {
		insert(id, "SalesInvoice", "Draft", "manager1", map[string]interface{}{"customer": "C140", "location": loc, "items": string(items), "cgst": 5.0, "sgst": 5.0, "total_amount": 210.0})
		if _, err := PostSalesInvoice(tenantID, id, "acct1"); err != nil {
			t.Fatal(err)
		}
	}
	if err := CancelSalesInvoice(tenantID, "SI-C1", "acct1", ""); err == nil {
		t.Fatal("cancelled without a reason")
	}
	if err := CancelSalesInvoice(tenantID, "SI-C1", "acct1", "billed twice"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	var net int64
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(ABS(t.n)), 0) FROM (SELECT SUM(debit) - SUM(credit) AS n FROM " + schema + ".gl_postings WHERE document_type = 'SalesInvoice' AND document_id = 'SI-C1' GROUP BY account_code) t").Scan(&net)
	if net != 0 {
		t.Fatalf("a cancelled invoice still nets %d paise on its accounts", net)
	}
	if onHand, _, _ := stage135Stock(t, schema, "TEE140", loc); onHand != 3 {
		t.Fatalf("on hand after one cancel = %d, want 3 (5 - 2 - 2 + 2)", onHand)
	}
	stage135BookAgrees(t, tenantID, loc)
	if _, err := SettleSalesInvoiceAmount(tenantID, "SI-C2", "acct1", 50, "R1"); err != nil {
		t.Fatal(err)
	}
	if err := CancelSalesInvoice(tenantID, "SI-C2", "acct1", "wrong"); err == nil || !strings.Contains(err.Error(), "credit note") {
		t.Fatalf("an invoice with a receipt was cancelled: %v", err)
	}
	src := documentSourceStores["CreditNote"][0]
	if got := numberingStore(tenantID, "HO", sourceDocumentStore(tenantID, src.Doctype, "SI-C2", src.StoreField)); got != loc {
		t.Fatalf("a credit note for SI-C2 numbers in %q, want the invoice's store %s", got, loc)
	}
}

// 140.12.12: an imported card settlement row finds its bank credit and
// empties card clearing; a row with no bank credit yet stays an exception.
func TestStage140AcquirerSettlementMatch(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	today := BusinessToday()
	insert("BANK-140A", "BankAccount", "Active", "system", map[string]interface{}{"bank_name": "Test Bank", "account_number": "000141", "gl_account_code": "1100", "status": "Active"})
	if err := PostDoubleEntry(tenantID, "POSCart", "CART-140A", map[string]int64{"1101": 100000}, map[string]int64{"4100": 100000}, "", "POSCart:CART-140A:T"); err != nil {
		t.Fatal(err)
	}
	insert("BSL-140A", "BankStatementLine", "Unmatched", "system", map[string]interface{}{"bank_account": "BANK-140A", "txn_date": today, "amount": 980.0, "dr_cr": "Credit", "match_status": "Unmatched"})
	insert("ACQ-1", AcquirerSettlementDoctype, "Pending", "acct1", map[string]interface{}{"settlement_date": today, "mode": "Card", "gross": 1000.0, "fee": 20.0, "net": 980.0})
	insert("ACQ-2", AcquirerSettlementDoctype, "Pending", "acct1", map[string]interface{}{"settlement_date": today, "mode": "UPI", "gross": 500.0, "fee": 0.0, "net": 500.0})
	res, err := MatchAcquirerSettlements(tenantID, "acct1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 1 || len(res.Exceptions) != 1 || res.Exceptions[0]["id"] != "ACQ-2" {
		t.Fatalf("match = %+v; want ACQ-1 matched, ACQ-2 an exception", res)
	}
	var clearing int64
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit) - SUM(credit), 0) FROM " + schema + ".gl_postings WHERE account_code = '1101'").Scan(&clearing)
	if clearing != 0 {
		t.Fatalf("card clearing left at %d paise, want 0", clearing)
	}

	insert("SESS-140-D", "POSSession", "Closed", "cashier1", map[string]interface{}{"location": "S140", "status": "Closed"})
	card := 100.0
	if err := RecordDeclaredTenders(tenantID, "SESS-140-D", &card, nil); err != nil {
		t.Fatal(err)
	}
	var diff float64
	_ = db.DB.QueryRow("SELECT (data->>'card_difference')::numeric FROM " + schema + ".documents WHERE id = 'SESS-140-D'").Scan(&diff)
	if diff != 100 {
		t.Fatalf("card difference = %.2f, want 100 (declared 100, bills 0)", diff)
	}
}
