package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Stage 72.1-72.5, 72.9-72.11 - the item & material master blueprint at the
// till: independent policy dimensions, sellability, sales-triggered packaging
// consumption with overrides and the short-stock policy, and what each kind of
// line takes out of stock and books. Own throwaway tenant schema.
func TestStage72ItemPolicyAndPOSConsumption(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	item := func(code, typ, category string, price float64) {
		t.Helper()
		insert(code, "Item", map[string]interface{}{"code": code, "name": code + " name", "item_type": typ, "category": category,
			"sale_price": price, "hsn_code": "7113", "tax_treatment": "Exempt"})
	}
	const loc = "STORE-72"
	insert(loc, "Location", map[string]interface{}{"code": loc, "name": "Store 72", "type": "Store", "sellable": "Yes"})
	stock := func(sku string, qty int, unitCost int64) {
		t.Helper()
		if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": float64(qty)}}, false); err != nil {
			t.Fatalf("stock %s: %v", sku, err)
		}
		if unitCost > 0 {
			if err := RecordStockReceiptCost(tenantID, sku, float64(qty), unitCost); err != nil {
				t.Fatalf("cost %s: %v", sku, err)
			}
		}
	}
	avail := func(sku string) int {
		var v int
		_ = db.DB.QueryRow(`SELECT available FROM `+schemaName+`.inventory_availability WHERE sku = $1 AND location_code = $2`, sku, loc).Scan(&v)
		return v
	}
	code := func(err error) string {
		var verr *ValidationError
		if errors.As(err, &verr) {
			return verr.Code
		}
		return ""
	}

	// --- 72.1 policy resolution ---------------------------------------------
	item("EARRING", "", "Earrings", 5000)
	item("NECKLACE", "Product", "Necklace", 20000)
	item("TIARA", "Product", "Tiara", 30000)
	item("SBOX", "Packaging", "", 0)
	item("LBOX", "Packaging", "", 0)
	item("PBOX", "Packaging", "", 0)
	item("BPAPER", "Consumable - Periodic", "", 0)
	item("REPAIR", "Service", "", 300)

	p, _, ok := ItemPolicyForSKU(tenantID, "EARRING")
	if !ok || p.Sellability != SellNormal || !p.Valuated() || p.Source["sellability"] != "item_type" {
		t.Errorf("blank type should resolve as Product merchandise: %+v", p)
	}
	if p, _, _ := ItemPolicyForSKU(tenantID, "SBOX"); p.Sellability != SellNot || p.Consumption != ConsumeAutoBackflush || p.ExpenseAccount != AccountPackagingExpense {
		t.Errorf("Packaging profile: %+v", p)
	}
	if p, _, _ := ItemPolicyForSKU(tenantID, "BPAPER"); p.Valuated() {
		t.Errorf("Consumable - Periodic is non-valuated: %+v", p)
	}
	override := ItemPolicyFor(tenantID, map[string]interface{}{"item_type": "Packaging", "sellability": "Normal sale", "bill_mode": "Item type default"})
	if override.Sellability != SellNormal || override.Source["sellability"] != "item" || override.BillMode != BillHidden {
		t.Errorf("item override should win only where set: %+v", override)
	}
	legacy := ItemPolicyFor(tenantID, map[string]interface{}{"item_type": "No Such Type"})
	if legacy.Sellability != SellNormal || legacy.Source["sellability"] != "default" {
		t.Errorf("unknown type falls back to the company default: %+v", legacy)
	}

	// --- 72.1 compatibility (blueprint 15.2) ---------------------------------
	for _, tc := range []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{"not tracked but valuated", map[string]interface{}{"stock_tracking": "Not tracked", "valuation": "Valuated"}, "MASTER-0402"},
		{"valuated as expense", map[string]interface{}{"valuation": "Valuated", "accounting_treatment": "Expense"}, "MASTER-0401"},
		{"fixed asset accounting on merchandise", map[string]interface{}{"accounting_treatment": "Fixed asset", "valuation": "Non-valuated"}, "MASTER-0403"},
		{"stored value sold normally", map[string]interface{}{"commercial_class": "Stored Value", "stock_tracking": "Not tracked", "accounting_treatment": "Stored value liability"}, "MASTER-0404"},
		{"backflush without stock", map[string]interface{}{"stock_tracking": "Not tracked", "consumption_method": "Auto backflush"}, "MASTER-0405"},
		{"unknown expense account", map[string]interface{}{"expense_account": "9999"}, "MASTER-0406"},
	} {
		if got := code(ValidateItemPolicyCompatibility(tenantID, "ItemType", tc.data)); got != tc.want {
			t.Errorf("%s: code %q, want %s", tc.name, got, tc.want)
		}
	}
	if err := ValidateItemPolicyCompatibility(tenantID, "ItemType", map[string]interface{}{"commercial_class": "Packaging", "valuation": "Non-valuated", "accounting_treatment": "Expense", "expense_account": "5160"}); err != nil {
		t.Errorf("a valid non-valuated packaging profile was refused: %v", err)
	}

	// --- 72.2 sellability at the till ---------------------------------------
	cart := "CART-72-NS"
	for i := 0; i < 2; i++ {
		_, err := ResolvePOSQuote(tenantID, QuoteRequest{CartNumber: cart, Location: loc, Lines: []QuoteLineRequest{{Sku: "EARRING", Qty: 1}, {Sku: "SBOX", Qty: 1}}})
		if code(err) != "POSOFF-0411" {
			t.Fatalf("packaging on a bill: err = %v, want POSOFF-0411", err)
		}
	}
	var attempts int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+schemaName+`.documents WHERE doctype = 'ItemException' AND data->>'source_id' = $1 AND data->>'sku' = 'SBOX'`, cart).Scan(&attempts)
	if attempts != 1 {
		t.Errorf("a refused attempt is logged once per cart, got %d records", attempts)
	}
	q, err := ResolvePOSQuote(tenantID, QuoteRequest{CartNumber: "CART-72-SVC", Location: loc, Lines: []QuoteLineRequest{{Sku: "REPAIR", Qty: 1}}})
	if err != nil || len(q.Lines) != 1 || !q.Lines[0].NotStocked {
		t.Errorf("a service is sold and flagged not stocked: %v %+v", err, q)
	}

	// --- 72.3 rules: specificity, groups, overrides --------------------------
	rule := func(id string, data map[string]interface{}) {
		t.Helper()
		data["code"], data["status"] = id, "Active"
		if err := ValidateMasterDataRules(tenantID, id, "BackflushRule", data); err != nil {
			t.Fatalf("rule %s refused: %v", id, err)
		}
		insert(id, "BackflushRule", data)
	}
	rule("BF-1", map[string]interface{}{"basis": "Category", "basis_value": "Earrings", "component_item": "SBOX", "group": "Box", "reason_required": "Yes"})
	rule("BF-2", map[string]interface{}{"basis": "Category", "basis_value": "Necklace", "component_item": "LBOX", "group": "Box", "reason_required": "Yes"})
	rule("BF-3", map[string]interface{}{"basis": "All items", "component_item": "BPAPER", "group": "Butter paper", "override_allowed": "No"})
	rule("BF-4", map[string]interface{}{"basis": "Item", "basis_value": "TIARA", "component_item": "PBOX", "group": "Box", "no_component_allowed": "No"})
	rule("BF-5", map[string]interface{}{"basis": "Category", "basis_value": "Tiara", "component_item": "LBOX", "group": "Box"})
	rule("BF-OLD", map[string]interface{}{"basis": "All items", "component_item": "PBOX", "group": "Ribbon", "effective_to": "2020-01-01"})
	if c := code(ValidateMasterDataRules(tenantID, "BF-X", "BackflushRule", map[string]interface{}{"basis": "All items", "component_item": "REPAIR"})); c != "MASTER-0405" {
		t.Errorf("a non-stocked component: code %q, want MASTER-0405", c)
	}
	if c := code(ValidateMasterDataRules(tenantID, "BF-X", "BackflushRule", map[string]interface{}{"basis": "Category", "basis_value": "Nowhere", "component_item": "SBOX"})); c != "MASTER-0408" {
		t.Errorf("an unknown category: code %q, want MASTER-0408", c)
	}

	lines := []ConsumptionLineInput{{Sku: "EARRING", Qty: 2}, {Sku: "NECKLACE", Qty: 1}, {Sku: "TIARA", Qty: 1}, {Sku: "REPAIR", Qty: 1}}
	cons, err := ResolveSaleConsumption(tenantID, loc, lines, nil, time.Time{})
	if err != nil {
		t.Fatalf("ResolveSaleConsumption: %v", err)
	}
	totals := ConsumptionComponentTotals(cons)
	if totals["SBOX"] != 2 || totals["LBOX"] != 1 || totals["PBOX"] != 1 || totals["BPAPER"] != 5 {
		t.Errorf("defaults: %v, want SBOX 2, LBOX 1, PBOX 1 (item rule beats category), BPAPER 5 (REPAIR is a service but still sold normally)", totals)
	}
	for _, c := range cons {
		if c.Group == "Box" && len(c.Choices) != 3 {
			t.Errorf("Box choices = %v, want the three boxes", c.Choices)
		}
		if c.Group == "Ribbon" {
			t.Errorf("an expired rule applied: %+v", c)
		}
	}
	_, err = ResolveSaleConsumption(tenantID, loc, lines, []ConsumptionOverride{{Sku: "NECKLACE", Group: "Box", Component: "PBOX"}}, time.Time{})
	if code(err) != "POSOFF-0414" {
		t.Errorf("changed box without a reason: err = %v, want POSOFF-0414", err)
	}
	for _, tc := range []struct {
		o    ConsumptionOverride
		want string
	}{
		{ConsumptionOverride{Sku: "NECKLACE", Group: "Box", Component: "BPAPER", Reason: "x"}, "POSOFF-0415"},
		{ConsumptionOverride{Sku: "EARRING", Group: "Butter paper", Component: "", Reason: "x"}, "POSOFF-0415"},
		{ConsumptionOverride{Sku: "TIARA", Group: "Box", Component: "", Reason: "x"}, "POSOFF-0415"},
		{ConsumptionOverride{Sku: "EARRING", Group: "Ribbon", Component: "", Reason: "x"}, "POSOFF-0415"},
	} {
		if _, err := ResolveSaleConsumption(tenantID, loc, lines, []ConsumptionOverride{tc.o}, time.Time{}); code(err) != tc.want {
			t.Errorf("override %+v: err = %v, want %s", tc.o, err, tc.want)
		}
	}
	cons, err = ResolveSaleConsumption(tenantID, loc, lines, []ConsumptionOverride{
		{Sku: "NECKLACE", Group: "Box", Component: "PBOX", Reason: "customer asked for premium"},
		{Sku: "EARRING", Group: "Box", Component: "", Reason: "own pouch"},
	}, time.Time{})
	if err != nil {
		t.Fatalf("valid overrides: %v", err)
	}
	totals = ConsumptionComponentTotals(cons)
	if totals["SBOX"] != 0 || totals["LBOX"] != 0 || totals["PBOX"] != 2 {
		t.Errorf("after overrides: %v, want no SBOX/LBOX and PBOX 2", totals)
	}
	// A store's own rule beats the all-stores one.
	rule("BF-6", map[string]interface{}{"basis": "Category", "basis_value": "Earrings", "location": loc, "component_item": "LBOX", "group": "Box"})
	if c, _ := ResolveSaleConsumption(tenantID, loc, []ConsumptionLineInput{{Sku: "EARRING", Qty: 1}}, nil, time.Time{}); ConsumptionComponentTotals(c)["LBOX"] != 1 {
		t.Errorf("store-specific rule should win for EARRING: %+v", c)
	}
	if _, err := db.DB.Exec(`UPDATE ` + schemaName + `.documents SET status = 'Inactive' WHERE id = 'BF-6'`); err != nil {
		t.Fatal(err)
	}

	// --- 72.5 short stock -----------------------------------------------------
	stock("EARRING", 10, 200000)
	stock("NECKLACE", 5, 1000000)
	stock("TIARA", 5, 1500000)
	stock("SBOX", 10, 1500)
	stock("LBOX", 10, 2500)
	stock("PBOX", 10, 9000)
	stock("BPAPER", 2, 0)
	shortages, err := CheckConsumptionStock(tenantID, loc, cons)
	if err != nil || len(shortages) != 1 || shortages[0].Component != "BPAPER" || shortages[0].Action != ShortStockAllow || BlockingShortage(shortages) != nil {
		t.Errorf("BPAPER short with Allow: %v %+v", err, shortages)
	}
	blocked := []ConsumptionLine{{ForSKU: "EARRING", Component: "SBOX", Qty: 50, ShortStock: ShortStockBlock}}
	if s, _ := CheckConsumptionStock(tenantID, loc, blocked); code(BlockingShortage(s)) != "POSOFF-0416" {
		t.Errorf("a Block rule's shortage must refuse the sale: %+v", s)
	}

	// --- finalize: stock, ledger, GL, exceptions ------------------------------
	insert("KIT", "Item", map[string]interface{}{"code": "KIT", "name": "Gift set", "item_type": "Virtual Kit", "sale_price": 9000.0, "hsn_code": "7113", "tax_treatment": "Exempt"})
	insert("PB-KIT", "ProductBundle", map[string]interface{}{"bundle_sku": "KIT", "name": "Gift set", "fulfillment_mode": "Virtual", "pricing_mode": "Fixed Price", "fixed_price": 9000.0,
		"components": `[{"sku":"EARRING","quantity":1},{"sku":"SBOX","quantity":1}]`})
	for i := range cons {
		cons[i].Choices = nil
	}
	cartNumber := "CART-72-SALE"
	cartData, _ := json.Marshal(map[string]interface{}{
		"location": loc, "payment_mode": "Cash",
		"items": []map[string]interface{}{
			{"sku": "EARRING", "qty": 2, "sale_price": 5000.0}, {"sku": "NECKLACE", "qty": 1, "sale_price": 20000.0},
			{"sku": "TIARA", "qty": 1, "sale_price": 30000.0}, {"sku": "REPAIR", "qty": 1, "sale_price": 300.0},
			{"sku": "KIT", "qty": 1, "sale_price": 9000.0},
		},
		"consumption": cons,
	})
	if _, err := db.DB.Exec(`INSERT INTO `+schemaName+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Processing', 'cashier72')`, cartNumber, cartData); err != nil {
		t.Fatalf("seed cart: %v", err)
	}
	if _, _, err := FinalizePOSCheckout(tenantID, cartNumber, ""); err != nil {
		t.Fatalf("FinalizePOSCheckout: %v", err)
	}
	for sku, want := range map[string]int{"EARRING": 7, "NECKLACE": 4, "TIARA": 4, "SBOX": 9, "LBOX": 10, "PBOX": 8, "BPAPER": -3, "KIT": 0, "REPAIR": 0} {
		if got := avail(sku); got != want {
			t.Errorf("%s available = %d, want %d", sku, got, want)
		}
	}
	gl := func(account, side string) int64 {
		var v int64
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(`+side+`), 0) FROM `+schemaName+`.gl_postings WHERE document_id = $1 AND account_code = $2`, cartNumber, account).Scan(&v)
		return v
	}
	if got := gl("5160", "debit"); got != 2*9000 {
		t.Errorf("packaging consumption (2 premium boxes, butter paper non-valuated) = %d paise, want %d", got, 2*9000)
	}
	// COGS: 2 earrings + necklace + tiara + kit (one earring + one small box); no repair.
	if got, want := gl("5100", "debit"), int64(2*200000+1000000+1500000+200000+1500); got != want {
		t.Errorf("COGS = %d, want %d", got, want)
	}
	var ledger, gaps, shortEx int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+schemaName+`.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_type' = 'POSConsumption' AND data->>'voucher_id' = $1`, cartNumber).Scan(&ledger)
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schemaName + `.documents WHERE doctype = 'POSCostingGap'`).Scan(&gaps)
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+schemaName+`.documents WHERE doctype = 'ItemException' AND data->>'process_type' = $1 AND data->>'sku' = 'BPAPER' AND status = 'Open'`, ItemExceptionShortStock).Scan(&shortEx)
	if ledger != 2 {
		t.Errorf("consumption ledger entries for the bill = %d, want 2 (PBOX, BPAPER)", ledger)
	}
	if gaps != 0 {
		t.Errorf("a service or non-valuated line must not record a costing gap, got %d", gaps)
	}
	if shortEx != 1 {
		t.Errorf("BPAPER going short should open one Item Exception, got %d", shortEx)
	}
	// A replay posts nothing twice.
	if _, _, err := FinalizePOSCheckout(tenantID, cartNumber, ""); err != nil || avail("PBOX") != 8 {
		t.Errorf("replay: err %v, PBOX %d", err, avail("PBOX"))
	}

	// Production follows the same Consumption field: a Packaging component is
	// backflushed at completion, an ordinary one issued up front.
	if issueNow, bf := splitBackflushComponents(tenantID, []bomComponent{{Sku: "SBOX", Qty: 1}, {Sku: "EARRING", Qty: 1}}); len(bf) != 1 || bf[0].Sku != "SBOX" || len(issueNow) != 1 {
		t.Errorf("production split by consumption method: issue %+v, backflush %+v", issueNow, bf)
	}

	// --- 72.12 treatment change after history (ITM-018) -----------------------
	if c := code(ValidateMasterDataRules(tenantID, "EARRING", "Item", map[string]interface{}{"code": "EARRING", "item_type": "Service", "hsn_code": "7113", "tax_treatment": "Exempt"})); c != "MASTER-0407" {
		t.Errorf("EARRING has stock history - switching it to a non-stock type: code %q, want MASTER-0407", c)
	}
	if c := code(ValidateMasterDataRules(tenantID, "Packaging", "ItemType", map[string]interface{}{"code": "Packaging", "commercial_class": "Packaging", "stock_tracking": "Tracked", "valuation": "Non-valuated", "accounting_treatment": "Expense", "sellability": "Not sellable"})); c != "MASTER-0407" {
		t.Errorf("Packaging items have stock history - changing the profile's valuation: code %q, want MASTER-0407", c)
	}
}

// Stage 72.9/72.10/72.13 - receiving and transferring by policy, and the
// section 20 reports. Own throwaway tenant.
func TestStage72ReceivingTransfersAndReports(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	lines := func(ls ...map[string]interface{}) string { b, _ := json.Marshal(ls); return string(b) }
	const loc = "WH-72R"
	insert(loc, "Location", map[string]interface{}{"code": loc, "name": "Receiving 72", "type": "Warehouse"})
	insert("GOODS", "Item", map[string]interface{}{"code": "GOODS", "name": "Goods", "hsn_code": "6109", "tax_treatment": "Exempt"})
	insert("PAPER", "Item", map[string]interface{}{"code": "PAPER", "name": "Butter paper", "item_type": "Consumable - Periodic", "hsn_code": "4823", "tax_treatment": "Exempt"})
	insert("INSTALL", "Item", map[string]interface{}{"code": "INSTALL", "name": "Installation", "item_type": "Service", "hsn_code": "9987", "tax_treatment": "Exempt"})
	insert("PO-72R", "PurchaseOrder", map[string]interface{}{"code": "PO-72R", "gst_mode": GSTModeExclusive,
		"items": lines(map[string]interface{}{"sku": "GOODS", "qty": 10, "rate": 100.0}, map[string]interface{}{"sku": "PAPER", "qty": 500, "rate": 2.0}, map[string]interface{}{"sku": "INSTALL", "qty": 1, "rate": 700.0})})
	insert("GRN-72R", "GRN", map[string]interface{}{"code": "GRN-72R", "po_id": "PO-72R", "location": loc,
		"received_items": lines(map[string]interface{}{"sku": "GOODS", "qty": 10, "accepted_qty": 10}, map[string]interface{}{"sku": "PAPER", "qty": 500, "accepted_qty": 500}, map[string]interface{}{"sku": "INSTALL", "qty": 1, "accepted_qty": 1})})
	if _, err := PostGRNReceiptWithQC(tenantID, loc, []interface{}{
		map[string]interface{}{"sku": "GOODS", "qty": 10.0, "accepted_qty": 10.0},
		map[string]interface{}{"sku": "PAPER", "qty": 500.0, "accepted_qty": 500.0},
		map[string]interface{}{"sku": "INSTALL", "qty": 1.0, "accepted_qty": 1.0},
	}, "system", "GRN-72R"); err != nil {
		t.Fatalf("PostGRNReceiptWithQC: %v", err)
	}
	avail := func(sku string) int {
		var v int
		_ = db.DB.QueryRow(`SELECT available FROM `+schemaName+`.inventory_availability WHERE sku = $1 AND location_code = $2`, sku, loc).Scan(&v)
		return v
	}
	if avail("GOODS") != 10 || avail("PAPER") != 500 || avail("INSTALL") != 0 {
		t.Errorf("stock after receipt: goods %d, paper %d, installation %d - want 10, 500 and none (a service keeps no stock)", avail("GOODS"), avail("PAPER"), avail("INSTALL"))
	}
	gl := func(account string) int64 {
		var v int64
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit), 0) FROM `+schemaName+`.gl_postings WHERE document_id = 'GRN-72R' AND account_code = $1`, account).Scan(&v)
		return v
	}
	if gl("1200") != 100000 || gl("5180") != 500*200+70000 {
		t.Errorf("receipt GL: 1200 %d (want 100000, goods only), 5180 %d (want %d: non-valuated paper + the service)", gl("1200"), gl("5180"), 500*200+70000)
	}
	if _, ok, _ := GetItemUnitCost(tenantID, "PAPER"); ok {
		t.Errorf("non-valuated paper must not enter the moving average")
	}

	// Transfers refuse an item that keeps no stock (ITM-002).
	var verr *ValidationError
	err := ValidateTransactionalRules(tenantID, "TransferOrder", "TO-72", "", nil, map[string]interface{}{
		"from_warehouse": loc, "to_warehouse": "ELSEWHERE", "items": lines(map[string]interface{}{"sku": "INSTALL", "qty": 1})})
	if !errors.As(err, &verr) || verr.Code != "INVENT-0421" {
		t.Errorf("transferring a service: err = %v, want INVENT-0421", err)
	}
	if err := ValidateTransactionalRules(tenantID, "TransferOrder", "TO-72", "", nil, map[string]interface{}{
		"from_warehouse": loc, "to_warehouse": "ELSEWHERE", "items": lines(map[string]interface{}{"sku": "GOODS", "qty": 1})}); err != nil {
		t.Errorf("transferring goods was refused: %v", err)
	}

	// Section 20 reports run and say the right things.
	run := func(id string, params map[string]string) []map[string]interface{} {
		t.Helper()
		def, ok := reportRegistry[id]
		if !ok {
			t.Fatalf("report %s not registered", id)
		}
		rows, err := def.Run(tenantID, params)
		if err != nil {
			t.Fatalf("report %s: %v", id, err)
		}
		return rows
	}
	reg := run("item-classification-register", map[string]string{})
	classes := map[string]string{}
	for _, r := range reg {
		classes[r["sku"].(string)] = r["commercial_class"].(string) + "/" + r["sellability"].(string)
	}
	if classes["INSTALL"] != "Service/Normal sale" || classes["PAPER"] != "Consumable/Not sellable" || classes["GOODS"] != "Merchandise/Normal sale" {
		t.Errorf("classification register: %v", classes)
	}
	if rows := run("non-valuated-stock", nil); len(rows) != 1 || rows[0]["sku"] != "PAPER" {
		t.Errorf("non-valuated stock: %v", rows)
	}
	missing := map[string]string{}
	for _, r := range run("item-master-completeness", nil) {
		missing[r["sku"].(string)] = r["missing"].(string)
	}
	if !strings.Contains(missing["INSTALL"], "sale price") || strings.Contains(missing["GOODS"], "HSN") {
		t.Errorf("a service sold with no price should be listed, goods with an HSN should not miss it: %v", missing)
	}
	for _, id := range []string{"backflush-consumption", "gwp-issue-register", "inactive-items-with-stock", "item-exceptions", "stored-value-liability", "stored-value-ledger"} {
		run(id, map[string]string{})
	}
}
