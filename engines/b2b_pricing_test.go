package engines

import (
	"strings"
	"testing"
	"time"
)

// Stage 148.26 - pricing reaches B2B: sales orders, direct invoices and CRM
// quotes fill a blank rate from the customer's price list; a typed rate wins;
// no list = today. Setting sales.b2b_pricing (off), sales.below_floor (off).

type b2bFx struct {
	t        *testing.T
	tenantID string
	schema   string
	insert   func(id, doctype, status, createdBy string, data map[string]interface{})
}

func newB2BFx(t *testing.T) *b2bFx {
	tenantID, schema, insert := stage140Tenant(t)
	f := &b2bFx{t: t, tenantID: tenantID, schema: schema, insert: insert}
	insert("ITEM-B1", "Item", "Active", "system", map[string]interface{}{"code": "ITEM-B1", "name": "Carton", "sale_price": 500.0, "cost_price": 250.0, "hsn_code": "4819", "tax_treatment": "Exempt"})
	insert("ITEM-B2", "Item", "Active", "system", map[string]interface{}{"code": "ITEM-B2", "name": "Tape", "sale_price": 100.0, "floor_price": 80.0, "hsn_code": "4819", "tax_treatment": "Exempt"})
	insert("RING-22-5", "Item", "Active", "system", map[string]interface{}{"code": "RING-22-5", "name": "Ring 22k", "family": "RING-A", "purity_karat": 22.0, "net_weight": 5.0, "sale_price": 31000.0, "hsn_code": "7113", "tax_treatment": "Exempt"})
	insert("RING-18-5", "Item", "Active", "system", map[string]interface{}{"code": "RING-18-5", "name": "Ring 18k", "family": "RING-A", "purity_karat": 18.0, "net_weight": 5.0, "sale_price": 25000.0, "hsn_code": "7113", "tax_treatment": "Exempt"})
	insert("CUST-WS", "Customer", "Active", "system", map[string]interface{}{"code": "CUST-WS", "name": "Wholesale Co", "status": "Active", "price_list_code": "WS"})
	insert("CUST-PLAIN", "Customer", "Active", "system", map[string]interface{}{"code": "CUST-PLAIN", "name": "Plain Co", "status": "Active"})
	for _, sku := range []string{"ITEM-B1", "ITEM-B2", "RING-22-5", "RING-18-5"} {
		if _, err := PostInventoryLedger(tenantID, "HO", []interface{}{map[string]interface{}{"sku": sku, "qty": 100.0}}, false); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *b2bFx) on() {
	f.t.Helper()
	if err := SetSetting(f.tenantID, "sales.b2b_pricing", "true", "test"); err != nil {
		f.t.Fatal(err)
	}
}

func (f *b2bFx) floorMode(mode string) {
	f.t.Helper()
	if err := SetSetting(f.tenantID, "sales.below_floor", mode, "test"); err != nil {
		f.t.Fatal(err)
	}
}

func (f *b2bFx) version(id, code, from, status string, rows []map[string]interface{}) {
	items := make([]interface{}, len(rows))
	for i, r := range rows {
		items[i] = r
	}
	f.insert(id, "PriceListVersion", status, "system", map[string]interface{}{"code": id, "price_list_code": code, "effective_from": from, "status": status, "items": items})
}

func (f *b2bFx) order(customerID, customerName string, lines ...SalesOrderLineInput) string {
	f.t.Helper()
	id, err := CreateSalesOrder(f.tenantID, SalesOrderInput{Channel: "Manual", CustomerName: customerName, CustomerID: customerID,
		ShippingAddress: "12 MG Road, Bengaluru, Karnataka 560001", PaymentStatus: "COD", Lines: lines})
	if err != nil {
		f.t.Fatalf("CreateSalesOrder: %v", err)
	}
	return id
}

func (f *b2bFx) lineField(orderID string, n int, key string) string {
	data, _, err := fetchDocData(f.tenantID, "SalesOrderLine", orderID+"-L"+string(rune('0'+n)))
	if err != nil {
		f.t.Fatalf("line %d: %v", n, err)
	}
	return pimString(data[key])
}

func (f *b2bFx) linePrice(orderID string, n int) float64 {
	data, _, _ := fetchDocData(f.tenantID, "SalesOrderLine", orderID+"-L"+string(rune('0'+n)))
	return numFromInterface(data["unit_price"])
}

var wsRows = []map[string]interface{}{
	{"sku": "ITEM-B1", "price": 400.0},
	{"sku": "ITEM-B1", "min_qty": 10.0, "price": 360.0},
}

func TestStage14826BlankRateFilledFromCustomerList(t *testing.T) {
	f := newB2BFx(t)
	f.version("PLV-WS1", "WS", "2020-01-01", "Approved", wsRows)

	// Setting off: a blank rate stays blank - today.
	off := f.order("CUST-WS", "Wholesale Co", SalesOrderLineInput{SKU: "ITEM-B1", Qty: 2})
	if p := f.linePrice(off, 1); p != 0 {
		t.Fatalf("setting off: blank rate must stay as today, got %v", p)
	}

	f.on()
	o := f.order("CUST-WS", "Wholesale Co",
		SalesOrderLineInput{SKU: "ITEM-B1", Qty: 2},
		SalesOrderLineInput{SKU: "ITEM-B1", Qty: 12},
		SalesOrderLineInput{SKU: "ITEM-B1", Qty: 2, UnitPrice: 450})
	if p := f.linePrice(o, 1); p != 400 {
		t.Errorf("list price: got %v want 400", p)
	}
	if r := f.lineField(o, 1, "price_reason"); !strings.Contains(r, "WS") {
		t.Errorf("the line shows why: %q", r)
	}
	// The order's own page gets the same words.
	detail, err := GetOrderConsoleDetail(f.tenantID, o)
	if err != nil {
		t.Fatal(err)
	}
	if ls, _ := detail["lines"].([]map[string]interface{}); len(ls) != 3 {
		t.Errorf("order detail lines: %d", len(ls))
	} else {
		listed := 0
		for _, l := range ls {
			if strings.Contains(l["price_reason"].(string), "WS") {
				listed++
			}
		}
		if listed != 2 {
			t.Errorf("order detail shows the rule on %d lines, want 2: %v", listed, ls)
		}
	}
	if p := f.linePrice(o, 2); p != 360 {
		t.Errorf("qty break at 12: got %v want 360", p)
	}
	if p := f.linePrice(o, 3); p != 450 {
		t.Errorf("a typed rate always wins: got %v", p)
	}
	if r := f.lineField(o, 3, "price_reason"); r != "" {
		t.Errorf("typed rate carries no list reason: %q", r)
	}
	plain := f.order("CUST-PLAIN", "Plain Co", SalesOrderLineInput{SKU: "ITEM-B1", Qty: 2})
	if p := f.linePrice(plain, 1); p != 0 {
		t.Errorf("no list = today: got %v", p)
	}
	// An as-of date before the version's window finds nothing.
	got, err := ResolveCustomerLinePrice(f.tenantID, CustomerPriceQuery{CustomerID: "CUST-WS", SKU: "ITEM-B1", Qty: 1, AsOf: "2019-01-01"})
	if err != nil || got.Found {
		t.Errorf("before the window: %+v %v", got, err)
	}
}

func TestStage14826JewelleryPricedByKaratAndWeightBand(t *testing.T) {
	f := newB2BFx(t)
	f.version("PLV-WS2", "WS", "2020-01-01", "Approved", []map[string]interface{}{
		{"design": "RING-A", "karat": 22.0, "weight_from": 4.0, "weight_to": 6.0, "price": 30000.0},
		{"design": "RING-A", "karat": 18.0, "weight_from": 6.0, "weight_to": 8.0, "price": 20000.0},
	})
	f.on()
	hit, _ := ResolveCustomerLinePrice(f.tenantID, CustomerPriceQuery{CustomerID: "CUST-WS", SKU: "RING-22-5", Qty: 1})
	if !hit.Found || hit.Rate != 30000 {
		t.Fatalf("22k 5g is in the 22k 4-6g band: %+v", hit)
	}
	miss, _ := ResolveCustomerLinePrice(f.tenantID, CustomerPriceQuery{CustomerID: "CUST-WS", SKU: "RING-18-5", Qty: 1})
	if miss.Found {
		t.Fatalf("18k 5g is outside the 18k 6-8g band: %+v", miss)
	}
}

func TestStage14826QuotaCountsDownByOrders(t *testing.T) {
	f := newB2BFx(t)
	f.version("PLV-WS3", "WS", "2020-01-01", "Approved", []map[string]interface{}{{"sku": "ITEM-B1", "price": 400.0, "quota_qty": 5.0}})
	f.on()
	a := f.order("CUST-WS", "Wholesale Co", SalesOrderLineInput{SKU: "ITEM-B1", Qty: 3})
	if p := f.linePrice(a, 1); p != 400 {
		t.Fatalf("first order inside the quota: %v", p)
	}
	b := f.order("CUST-WS", "Wholesale Co", SalesOrderLineInput{SKU: "ITEM-B1", Qty: 3})
	if p := f.linePrice(b, 1); p != 0 {
		t.Fatalf("only 2 left of the quota, a line of 3 is not priced by it: %v", p)
	}
	res, _ := ResolveCustomerLinePrice(f.tenantID, CustomerPriceQuery{CustomerID: "CUST-WS", SKU: "ITEM-B1", Qty: 2})
	if !res.Found || res.QuotaLeft != 2 {
		t.Fatalf("2 left: %+v", res)
	}
}

func TestStage14826BelowFloorHoldsOrReasons(t *testing.T) {
	f := newB2BFx(t)
	f.version("PLV-WS4", "WS", "2020-01-01", "Approved", []map[string]interface{}{{"sku": "ITEM-B1", "price": 400.0, "floor": 380.0}})
	f.on()

	// floor mode off (default): no change, even far below.
	free := f.order("CUST-WS", "Wholesale Co", SalesOrderLineInput{SKU: "ITEM-B1", Qty: 1, UnitPrice: 100})
	if st, _, _ := GetOrderStatus(f.tenantID, free); st == "On Hold" {
		t.Fatalf("no floor mode = today, got %s", st)
	}

	f.floorMode("hold")
	held := f.order("CUST-WS", "Wholesale Co", SalesOrderLineInput{SKU: "ITEM-B1", Qty: 1, UnitPrice: 350})
	st, hold, _ := GetOrderStatus(f.tenantID, held)
	if st != "On Hold" || hold != HoldMargin {
		t.Fatalf("below the floor puts a Margin hold, got %s/%s", st, hold)
	}
	if p := f.linePrice(held, 1); p != 350 {
		t.Fatalf("the typed rate is kept: %v", p)
	}
	if err := ReleaseOrderHold(f.tenantID, held); err == nil || codeOf(err) != "SALESP-0621" {
		t.Fatalf("the generic release refuses a Margin hold (SALESP-0621): %v", err)
	}
	if err := ReleaseMarginHold(f.tenantID, held, "clerk", "Cashier", "ok"); err == nil || codeOf(err) != "SALESP-0622" {
		t.Fatalf("a cashier cannot release a Margin hold (SALESP-0622): %v", err)
	}
	if err := ReleaseMarginHold(f.tenantID, held, "mgr", "Store Manager", "customer is key"); err != nil {
		t.Fatalf("manager release: %v", err)
	}
	if st, hold, _ := GetOrderStatus(f.tenantID, held); st == "On Hold" {
		t.Fatalf("released, still on hold: %s", hold)
	}

	// With a reason typed on the line there is no hold.
	reasoned := f.order("CUST-WS", "Wholesale Co", SalesOrderLineInput{SKU: "ITEM-B1", Qty: 1, UnitPrice: 350, FloorReason: "clearance"})
	if st, _, _ := GetOrderStatus(f.tenantID, reasoned); st == "On Hold" {
		t.Fatalf("a reason avoids the hold, got %s", st)
	}
	if got := f.lineField(reasoned, 1, "floor_reason"); got != "clearance" {
		t.Fatalf("the reason is kept on the line: %q", got)
	}

	// Floor from the item when the list has none (ITEM-B2 floor 80).
	item := f.order("CUST-PLAIN", "Plain Co", SalesOrderLineInput{SKU: "ITEM-B2", Qty: 1, UnitPrice: 70})
	if st, hold, _ := GetOrderStatus(f.tenantID, item); st != "On Hold" || hold != HoldMargin {
		t.Fatalf("item floor price holds too: %s/%s", st, hold)
	}
}

func TestStage14826InvoiceBelowFloorNeedsReason(t *testing.T) {
	f := newB2BFx(t)
	f.version("PLV-WS5", "WS", "2020-01-01", "Approved", []map[string]interface{}{{"sku": "ITEM-B1", "price": 400.0, "floor": 380.0}})
	f.on()
	mk := func(rate float64, reason string) map[string]interface{} {
		line := map[string]interface{}{"sku": "ITEM-B1", "qty": 1.0, "rate": rate}
		if reason != "" {
			line["floor_reason"] = reason
		}
		return map[string]interface{}{"customer": "CUST-WS", "customer_id": "CUST-WS", "invoice_date": "2026-11-10", "items": []interface{}{line}}
	}
	if err := ValidateDocument(f.tenantID, "SalesInvoice", mk(300, "")); codeOf(err) == "SALESP-0624" {
		t.Fatalf("floor mode off must not refuse: %v", err)
	}
	f.floorMode("reason")
	if err := ValidateDocument(f.tenantID, "SalesInvoice", mk(300, "")); codeOf(err) != "SALESP-0624" {
		t.Fatalf("below floor with no reason: want SALESP-0624, got %v", err)
	}
	if err := ValidateDocument(f.tenantID, "SalesInvoice", mk(300, "clearance")); codeOf(err) == "SALESP-0624" {
		t.Fatalf("a reason lets it through: %v", err)
	}
	if err := ValidateDocument(f.tenantID, "SalesInvoice", mk(390, "")); codeOf(err) == "SALESP-0624" {
		t.Fatalf("at or above the floor needs no reason: %v", err)
	}
}

func TestStage14826EndingPreviewAndApply(t *testing.T) {
	f := newB2BFx(t)
	f.version("PLV-WS6", "WS", "2021-01-01", "Draft", []map[string]interface{}{{"sku": "ITEM-B1", "price": 401.0}, {"sku": "ITEM-B2", "price": 90.0}})
	f.version("PLV-WS7", "WS", "2020-01-01", "Approved", []map[string]interface{}{{"sku": "ITEM-B1", "price": 401.0}})
	prev, err := PreviewPriceListEndings(f.tenantID, "PLV-WS6", "99")
	if err != nil || len(prev) != 2 {
		t.Fatalf("preview rows: %+v %v", prev, err)
	}
	if prev[0].From != 401 || prev[0].To != roundUpToEnding(401, "99") {
		t.Fatalf("preview follows the 99 rule: %+v", prev[0])
	}
	data, _, _ := fetchDocData(f.tenantID, "PriceListVersion", "PLV-WS6")
	if rows, _ := data["items"].([]interface{}); numFromInterface(rows[0].(map[string]interface{})["price"]) != 401 {
		t.Fatalf("preview must not save")
	}
	if _, err := ApplyPriceListEndings(f.tenantID, "PLV-WS7", "99"); codeOf(err) != "SALESP-0625" {
		t.Fatalf("an approved version cannot be rewritten (SALESP-0625): %v", err)
	}
	if _, err := ApplyPriceListEndings(f.tenantID, "PLV-WS6", "99"); err != nil {
		t.Fatal(err)
	}
	data, _, _ = fetchDocData(f.tenantID, "PriceListVersion", "PLV-WS6")
	rows, _ := data["items"].([]interface{})
	if got := numFromInterface(rows[0].(map[string]interface{})["price"]); got != prev[0].To {
		t.Fatalf("applied %v, previewed %v", got, prev[0].To)
	}
}

func TestStage14826ChangingAgreementFlagsOpenLinesNeverReprices(t *testing.T) {
	f := newB2BFx(t)
	f.version("PLV-WS8", "WS", "2020-01-01", "Approved", wsRows)
	f.on()
	o := f.order("CUST-WS", "Wholesale Co", SalesOrderLineInput{SKU: "ITEM-B1", Qty: 2}, SalesOrderLineInput{SKU: "ITEM-B2", Qty: 1, UnitPrice: 90})
	f.version("PLV-WS9", "WS", "2026-01-01", "Approved", []map[string]interface{}{{"sku": "ITEM-B1", "price": 380.0}})
	postApprovedPriceListVersion(f.tenantID, "PLV-WS9")
	if got := f.lineField(o, 1, "price_changed"); got == "" {
		t.Fatalf("the open line on a changed price is flagged")
	}
	if p := f.linePrice(o, 1); p != 400 {
		t.Fatalf("flagging never reprices: %v", p)
	}
	if got := f.lineField(o, 2, "price_changed"); got != "" {
		t.Fatalf("a line the agreement does not touch is not flagged: %q", got)
	}
}

func TestStage14826QuoteFillsRateAndShowsMarginToCostRoles(t *testing.T) {
	e := newCRMTestEnv(t)
	e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ('SKU-QB', 'Item', '{"code":"SKU-QB","name":"Uniform shirt","standard_cost":250}', 'Active', 'test')`)
	e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ('PLV-QB', 'PriceListVersion', '{"code":"PLV-QB","price_list_code":"WSQ","effective_from":"2020-01-01","status":"Approved","items":[{"sku":"SKU-QB","price":400}]}', 'Approved', 'test')`)
	l, err := SaveCRMLead(e.tenantID, "", CRMLead{Name: "Ramesh", Company: "Acme", Phone: "9" + time.Now().Format("150405999")[:9], Source: "Exhibition"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	conv, err := ConvertCRMLead(e.tenantID, l.ID, "Uniforms", 0, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE {s}.documents SET data = data || '{"price_list_code":"WSQ"}'::jsonb WHERE id = $1 AND doctype = 'Customer'`, conv.CustomerID)

	// off: a blank rate stays blank
	if _, err := SaveCRMQuotation(e.tenantID, conv.OpportunityID, []CRMQuotationLine{{SKU: "SKU-QB", Qty: 5}}, "", "", "test"); err != nil {
		t.Fatal(err)
	}
	o, _ := GetCRMOpportunity(e.tenantID, conv.OpportunityID)
	_, lines, _, _ := GetCRMQuotation(e.tenantID, o.QuotationID)
	if lines[0].Rate != 0 {
		t.Fatalf("setting off: today, got rate %v", lines[0].Rate)
	}

	e.set("sales.b2b_pricing", "true")
	if _, err := SaveCRMQuotation(e.tenantID, conv.OpportunityID, []CRMQuotationLine{{SKU: "SKU-QB", Qty: 5}}, "", "", "test"); err != nil {
		t.Fatal(err)
	}
	_, lines, _, _ = GetCRMQuotation(e.tenantID, o.QuotationID)
	if lines[0].Rate != 400 || !strings.Contains(lines[0].Why, "WSQ") {
		t.Fatalf("filled rate with its reason: %+v", lines[0])
	}
	if _, err := SaveCRMQuotation(e.tenantID, conv.OpportunityID, []CRMQuotationLine{{SKU: "SKU-QB", Qty: 5, Rate: 420}}, "", "", "test"); err != nil {
		t.Fatal(err)
	}
	_, lines, _, _ = GetCRMQuotation(e.tenantID, o.QuotationID)
	if lines[0].Rate != 420 || lines[0].Why != "" {
		t.Fatalf("a typed rate wins: %+v", lines[0])
	}

	m := CRMQuotationMargins(e.tenantID, "Admin", lines)
	if len(m) != 1 || m[0].MarginPct < 40 || m[0].MarginPct > 41 {
		t.Fatalf("margin for a cost role: %+v", m)
	}
	if CRMQuotationMargins(e.tenantID, "Cashier", lines) != nil {
		t.Fatalf("a role that may not see cost gets no margin")
	}
}
