package engines

import (
	"testing"

	"custom_erp/db"
)

// Stage 148.36 - custom-order configurator: off by default and invisible while
// off; the price is weight x the day's rate + making + the rule lines, each part
// shown; no rate for today refuses; a saved quote keeps the rate it used; the
// order is a Draft that reserves nothing.

func (f *creditFixture) coChoice(kind, value string, extra map[string]interface{}) {
	d := map[string]interface{}{"kind": kind, "value": value, "status": "Active"}
	for k, v := range extra {
		d[k] = v
	}
	f.seed("CC-"+kind+"-"+value, CustomChoiceDoctype, "Active", d)
}

func (f *creditFixture) coSetup() {
	f.t.Helper()
	if err := SetModuleEntitlement(f.tenantID, ModuleCustomOrders, true, "test"); err != nil {
		f.t.Fatal(err)
	}
	f.coChoice("Metal", "Gold", map[string]interface{}{"hsn_code": "7113", "gst_rate": 3.0})
	f.coChoice("Purity", "22K", nil)
	f.coChoice("Stone", "Diamond", nil)
	f.coChoice("Size", "12", nil)
	f.coChoice("Making type", "Hand made", map[string]interface{}{"making_basis": "Per gram", "making_value": 500.0})
	f.coChoice("Making type", "Plain", nil) // no charge set
	f.seed("CR-1", CustomOrderRuleDoctype, "Active", map[string]interface{}{"line_type": "Stone", "applies_kind": "Stone", "choice": "Diamond", "label": "Diamond", "basis": "Per stone", "amount": 2000.0, "status": "Active"})
	f.seed("CR-2", CustomOrderRuleDoctype, "Active", map[string]interface{}{"line_type": "Setting", "applies_kind": "Stone", "choice": "diamond", "label": "Setting", "basis": "Flat", "amount": 800.0, "status": "Active"})
	f.seed("CR-3", CustomOrderRuleDoctype, "Active", map[string]interface{}{"line_type": "Setting", "applies_kind": "Size", "choice": "12", "label": "Resize", "basis": "% of metal", "amount": 1.0, "status": "Inactive"})
}

func (f *creditFixture) coRate(date string, rate float64) string {
	id := "MR-" + date
	f.seed(id, MetalRateDoctype, "Active", map[string]interface{}{"metal": "Gold", "purity": "22k", "rate_date": date, "rate_per_gram": rate})
	return id
}

func (f *creditFixture) coToday() string {
	var today string
	if err := db.DB.QueryRow("SELECT to_char(" + crmLocalTodaySQL(crmZoneName(f.schema)) + ", 'YYYY-MM-DD')").Scan(&today); err != nil {
		f.t.Fatal(err)
	}
	return today
}

var coInput = CustomOrderInput{Metal: "Gold", Purity: "22K", NetWeight: 10, Stone: "Diamond", StoneCount: 3, Size: "12", MakingType: "Hand made"}

func TestStage14836OffByDefaultAndInvisibleWhileOff(t *testing.T) {
	f := newCreditFixture(t)
	if on, _ := IsModuleEnabled(f.tenantID, ModuleCustomOrders); on {
		t.Fatalf("custom_orders must default to off")
	}
	if _, err := PriceCustomOrder(f.tenantID, coInput); t5510Code(err) != "SALESP-0760" {
		t.Fatalf("price while off: want SALESP-0760, got %v", err)
	}
	if _, err := CustomOrderChoices(f.tenantID); t5510Code(err) != "SALESP-0760" {
		t.Fatalf("choices while off: want SALESP-0760, got %v", err)
	}
	if _, err := SaveCustomOrder(f.tenantID, "u1", &CustomOrderPrice{}, "X", CustomOrderCustomer{}); t5510Code(err) != "SALESP-0760" {
		t.Fatalf("save while off: want SALESP-0760, got %v", err)
	}
	want := map[string]bool{MetalRateDoctype: true, CustomChoiceDoctype: true, CustomOrderRuleDoctype: true, CustomOrderQuoteDoctype: true}
	types, _ := GetDocTypes(f.tenantID)
	for _, d := range types {
		if want[d["name"].(string)] {
			t.Fatalf("%v must not be listed while the module is off", d["name"])
		}
	}
	for name := range want {
		key, err := ModuleForDoctype(f.tenantID, name)
		if err != nil || key != ModuleCustomOrders {
			t.Fatalf("%s must belong to the module: %q %v", name, key, err)
		}
		if RequireModules(f.tenantID, key) == nil {
			t.Fatalf("the generic routes must refuse %s while the module is off", name)
		}
	}
	if err := SetModuleEntitlement(f.tenantID, ModuleCustomOrders, true, "u1"); err != nil {
		t.Fatal(err)
	}
	seen := 0
	types, _ = GetDocTypes(f.tenantID)
	for _, d := range types {
		if want[d["name"].(string)] {
			seen++
		}
	}
	if seen != 4 {
		t.Fatalf("with the module on, 4 doctypes show, saw %d", seen)
	}
}

func TestStage14836PriceIsWeightTimesRatePlusMakingPlusLines(t *testing.T) {
	f := newCreditFixture(t)
	f.coSetup()
	if _, err := PriceCustomOrder(f.tenantID, coInput); t5510Code(err) != "SALESP-0761" {
		t.Fatalf("no rate today: want SALESP-0761, got %v", err)
	}
	f.coRate("2000-01-01", 5000) // an old day's rate is not today's
	if _, err := PriceCustomOrder(f.tenantID, coInput); t5510Code(err) != "SALESP-0761" {
		t.Fatalf("an old rate must not price today's order: got %v", err)
	}
	f.coRate(f.coToday(), 6000)
	p, err := PriceCustomOrder(f.tenantID, coInput)
	if err != nil {
		t.Fatal(err)
	}
	// metal 10 x 6000 = 60000, making 10 x 500 = 5000, stones 3 x 2000 = 6000, setting 800.
	if p.MetalAmount != 60000 || p.MakingAmt != 5000 || p.ExtraAmount != 6800 || p.Total != 71800 {
		t.Fatalf("price parts wrong: %+v", p)
	}
	parts := map[string]float64{}
	for _, l := range p.Lines {
		parts[l.Part] += l.Amount
	}
	if len(p.Lines) != 4 || parts["Metal"] != 60000 || parts["Making"] != 5000 || parts["Stone"] != 6000 || parts["Setting"] != 800 {
		t.Fatalf("each part must show (the inactive rule must not): %+v", p.Lines)
	}
	if p.HSN != "7113" || p.GSTRate != 3 || p.RatePerGram != 6000 {
		t.Fatalf("tax and rate used: %+v", p)
	}
	// Choices come from the masters.
	for name, in := range map[string]CustomOrderInput{
		"metal": {Metal: "Brass", Purity: "22K", NetWeight: 1, MakingType: "Hand made"},
		"stone": {Metal: "Gold", Purity: "22K", NetWeight: 1, MakingType: "Hand made", Stone: "Ruby"},
		"size":  {Metal: "Gold", Purity: "22K", NetWeight: 1, MakingType: "Hand made", Size: "99"},
		"blank": {Metal: "Gold", Purity: "22K", NetWeight: 1},
	} {
		if _, err := PriceCustomOrder(f.tenantID, in); t5510Code(err) != "SALESP-0762" {
			t.Fatalf("%s outside the masters: want SALESP-0762, got %v", name, err)
		}
	}
	if _, err := PriceCustomOrder(f.tenantID, CustomOrderInput{Metal: "Gold", Purity: "22K", MakingType: "Hand made"}); t5510Code(err) != "SALESP-0763" {
		t.Fatalf("no weight: want SALESP-0763, got %v", err)
	}
	if _, err := PriceCustomOrder(f.tenantID, CustomOrderInput{Metal: "Gold", Purity: "22K", NetWeight: 1, MakingType: "Plain"}); t5510Code(err) != "SALESP-0764" {
		t.Fatalf("making type with no charge: want SALESP-0764, got %v", err)
	}
	// An inactive choice is not offered.
	f.seed("CC-OFF", CustomChoiceDoctype, "Active", map[string]interface{}{"kind": "Size", "value": "14", "status": "Inactive"})
	if _, err := PriceCustomOrder(f.tenantID, CustomOrderInput{Metal: "Gold", Purity: "22K", NetWeight: 1, MakingType: "Hand made", Size: "14"}); t5510Code(err) != "SALESP-0762" {
		t.Fatalf("an inactive choice: want SALESP-0762, got %v", err)
	}
}

func TestStage14836SavedQuoteKeepsItsRateAndOrderIsDraft(t *testing.T) {
	f := newCreditFixture(t)
	f.coSetup()
	today := f.coToday()
	f.coRate(today, 6000)
	p, err := PriceCustomOrder(f.tenantID, coInput)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckCustomOrderReady(p, CustomOrderCustomer{Name: "Asha"}); t5510Code(err) != "SALESP-0766" {
		t.Fatalf("no address: want SALESP-0766, got %v", err)
	}
	res, err := SaveCustomOrder(f.tenantID, "u1", p, "ITEM-CC", CustomOrderCustomer{Name: "Asha", Address: "12 MG Road, Bengaluru"})
	if err != nil {
		t.Fatal(err)
	}
	// The rate moves: a newer save for today at 7000. The saved quote does not.
	f.seed("MR-LATER", MetalRateDoctype, "Active", map[string]interface{}{"metal": "Gold", "purity": "22K", "rate_date": today, "rate_per_gram": 7000.0})
	var rate, total, orderStatus, lineStatus, qStatus string
	var reserved int
	if err := db.DB.QueryRow("SELECT data->>'rate_per_gram', data->>'total', status FROM "+f.schema+".documents WHERE id = $1 AND doctype = 'CustomOrderQuote'", res.QuoteID).Scan(&rate, &total, &qStatus); err != nil {
		t.Fatal(err)
	}
	if rate != "6000" || total != "71800" || qStatus != "Ordered" {
		t.Fatalf("saved quote: rate %s total %s status %s", rate, total, qStatus)
	}
	if err := db.DB.QueryRow("SELECT data->>'order_status' FROM "+f.schema+".documents WHERE id = $1 AND doctype = 'SalesOrder'", res.OrderID).Scan(&orderStatus); err != nil || orderStatus != "Draft" {
		t.Fatalf("order must be Draft: %q %v", orderStatus, err)
	}
	var amount, sku string
	if err := db.DB.QueryRow("SELECT data->>'unit_price', data->>'sku' FROM "+f.schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1", res.OrderID).Scan(&amount, &sku); err != nil || amount != "71800" || sku != "ITEM-CC" {
		t.Fatalf("order line: %s %s %v", amount, sku, err)
	}
	_ = lineStatus
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+f.schema+".inventory_reservation WHERE order_id = $1", res.OrderID).Scan(&reserved)
	if reserved != 0 {
		t.Fatalf("a draft order reserves no stock, found %d", reserved)
	}
	// A new quote today uses the newer rate; the old one is unchanged.
	p2, err := PriceCustomOrder(f.tenantID, coInput)
	if err != nil || p2.RatePerGram != 7000 {
		t.Fatalf("a new quote takes the latest rate of the day: %+v %v", p2, err)
	}
	if res.OrderNumber == "" {
		t.Fatalf("the order has a number")
	}
}

// Review fix (M11): the quote total is GST-inclusive, like every price in this
// ERP. The invoice splits the same amount into taxable + GST, never adds tax.
func TestStage14836QuoteTotalIsGSTInclusiveAndInvoiceSplitsIt(t *testing.T) {
	f := newCreditFixture(t)
	f.coSetup()
	f.coRate(f.coToday(), 6000)
	p, err := PriceCustomOrder(f.tenantID, coInput)
	if err != nil {
		t.Fatal(err)
	}
	body := CustomOrderItemBody(f.tenantID, p)
	body["code"] = "ITEM-CCGST"
	f.seed("ITEM-CCGST", "Item", "Active", body)
	g, err := ComputeGSTForLines(f.tenantID, []GSTLineInput{{Sku: "ITEM-CCGST", Qty: 1, UnitRate: p.Total}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := round2(g.TaxableAmount + g.TotalTax); got != p.Total {
		t.Fatalf("invoice taxable %.2f + GST %.2f = %.2f, quote total %.2f", g.TaxableAmount, g.TotalTax, got, p.Total)
	}
	if p.GSTAmount != round2(g.TotalTax) || round2(p.Total-p.GSTAmount) != round2(g.TaxableAmount) {
		t.Fatalf("quote shows GST %.2f, invoice %.2f (taxable %.2f)", p.GSTAmount, g.TotalTax, g.TaxableAmount)
	}
}

// Review fix (M10): an item made for an order that then failed to save is
// removed; an item an order already uses is left alone.
func TestStage14836DiscardOnlyRemovesAnUnusedCustomItem(t *testing.T) {
	f := newCreditFixture(t)
	f.coSetup()
	f.seed("ITEM-CCORPH", "Item", "Active", map[string]interface{}{"code": "ITEM-CCORPH", "name": "x", "product_type": "Custom jewellery"})
	f.seed("ITEM-CCUSED", "Item", "Active", map[string]interface{}{"code": "ITEM-CCUSED", "name": "y", "product_type": "Custom jewellery"})
	f.seed("ITEM-CCPLAIN", "Item", "Active", map[string]interface{}{"code": "ITEM-CCPLAIN", "name": "z", "product_type": "Ring"})
	f.seed("SOL-CCUSED", "SalesOrderLine", "Pending", map[string]interface{}{"order_id": "SO-X", "sku": "ITEM-CCUSED", "qty": 1})
	for _, id := range []string{"ITEM-CCORPH", "ITEM-CCUSED", "ITEM-CCPLAIN"} {
		DiscardCustomOrderItem(f.tenantID, id)
	}
	gone := func(id string) bool {
		var n int
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+f.schema+".documents WHERE id = $1 AND deleted_at IS NULL", id).Scan(&n)
		return n == 0
	}
	if !gone("ITEM-CCORPH") || gone("ITEM-CCUSED") || gone("ITEM-CCPLAIN") {
		t.Fatalf("orphan gone=%v used gone=%v plain gone=%v", gone("ITEM-CCORPH"), gone("ITEM-CCUSED"), gone("ITEM-CCPLAIN"))
	}
}
