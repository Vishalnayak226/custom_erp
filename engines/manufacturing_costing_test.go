package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// costingFixture provisions a throwaway client with a store, items, a BOM
// (2 RAW per unit, one by-product line at the given cost share), RAW stock
// with a known average cost, a two-operation routing and an employee.
type costingFixture struct {
	t        *testing.T
	tenantID string
	schema   string
	loc      string
}

func newCostingFixture(t *testing.T, byProductShare float64) *costingFixture {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	f := &costingFixture{t: t, tenantID: tenantID, schema: schema, loc: "MFG-170"}
	f.insert(f.loc, "Location", "Active", map[string]interface{}{"code": f.loc, "name": "Workshop", "type": "Warehouse"})
	for _, sku := range []string{"FG", "RAW", "BP"} {
		f.insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku, "hsn_code": "7113", "tax_treatment": "Exempt"})
	}
	comps, _ := json.Marshal([]map[string]interface{}{{"sku": "RAW", "qty": 2}})
	bps, _ := json.Marshal([]map[string]interface{}{{"sku": "BP", "qty_per_unit": 1, "cost_share_percent": byProductShare}})
	f.insert("BOM-170", "BOM", "Active", map[string]interface{}{"code": "BOM-170", "parent_item": "FG", "components": string(comps), "by_products": string(bps), "status": "Active"})
	ops, _ := json.Marshal([]map[string]interface{}{{"seq": 10, "operation_name": "Cut"}, {"seq": 20, "operation_name": "Polish"}})
	f.insert("RT-170", "Routing", "Active", map[string]interface{}{"code": "RT-170", "operations": string(ops), "status": "Active"})
	f.insert("EMP-170", "Employee", "Active", map[string]interface{}{"code": "EMP-170", "name": "Asha"})
	if _, err := PostInventoryLedger(tenantID, f.loc, []interface{}{map[string]interface{}{"sku": "RAW", "qty": 40.0}}, false); err != nil {
		t.Fatalf("stock: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".item_cost (item_code, cumulative_qty_received, cumulative_value_received_paise, avg_unit_cost_paise) VALUES ('RAW', 40, 40000, 1000) ON CONFLICT (item_code) DO UPDATE SET avg_unit_cost_paise = 1000"); err != nil {
		t.Fatalf("cost: %v", err)
	}
	return f
}

func (f *costingFixture) insert(id, doctype, status string, data map[string]interface{}) {
	f.t.Helper()
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
		f.t.Fatalf("insert %s: %v", id, err)
	}
}

func (f *costingFixture) order(id string, qty float64, status string) {
	f.insert(id, "ProductionOrder", status, map[string]interface{}{"code": id, "bom_id": "BOM-170", "quantity": qty, "location": f.loc, "status": status, "routing_id": "RT-170"})
}

func (f *costingFixture) glNet(account string) int64 {
	var n int64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit - credit), 0) FROM "+f.schema+".gl_postings WHERE account_code = $1", account).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *costingFixture) glRows() int {
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".gl_postings").Scan(&n)
	return n
}

func (f *costingFixture) onHand(sku string) float64 {
	var n float64
	_ = db.DB.QueryRow("SELECT on_hand FROM "+f.schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, f.loc).Scan(&n)
	return n
}

func (f *costingFixture) avgCost(sku string) int64 {
	var n int64
	_ = db.DB.QueryRow("SELECT avg_unit_cost_paise FROM "+f.schema+".item_cost WHERE item_code = $1", sku).Scan(&n)
	return n
}

func wantMfgCode(t *testing.T, err error, code string) {
	t.Helper()
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

// 170.10: quantities, time and operator are recorded; the total on an operation
// cannot pass the order quantity; an unknown employee is refused; a refusal
// leaves nothing recorded.
func TestOperationConfirmationRecordsQuantityTimeOperator(t *testing.T) {
	f := newCostingFixture(t, 0)
	f.order("PO-CONF", 10, "Material Issued")
	conf := func(seq int, c OperationConfirmation) error {
		_, err := ConfirmOperationRecorded(f.tenantID, "PO-CONF", seq, "tester", c)
		return err
	}
	if err := conf(10, OperationConfirmation{GoodQty: 6, ScrapQty: 1, Minutes: 30, EmployeeID: "EMP-170"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	wantMfgCode(t, conf(10, OperationConfirmation{GoodQty: 4}), "MFG-1740") // 7 + 4 > 10
	wantMfgCode(t, conf(10, OperationConfirmation{GoodQty: 1, EmployeeID: "NOBODY"}), "MFG-1742")
	wantMfgCode(t, conf(10, OperationConfirmation{GoodQty: -1}), "MFG-1741")
	wantMfgCode(t, conf(20, OperationConfirmation{GoodQty: 1}), "MANUFA-0147") // op 10 not complete yet
	if err := conf(10, OperationConfirmation{GoodQty: 3, Minutes: 12}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if err := conf(20, OperationConfirmation{GoodQty: 10, Minutes: 5}); err != nil {
		t.Fatalf("op 20: %v", err)
	}
	rows, err := ListProductionConfirmations(f.tenantID, "PO-CONF")
	if err != nil || len(rows) != 3 {
		t.Fatalf("want 3 confirmations, got %d (%v)", len(rows), err)
	}
	if r := rows[0]; r.GoodQty != 6 || r.ScrapQty != 1 || r.Minutes != 30 || r.OperatorKind != "Employee" || r.OperatorID != "EMP-170" {
		t.Fatalf("first row: %+v", r)
	}
	if r := rows[1]; r.OperatorKind != "User" || r.OperatorID != "tester" {
		t.Fatalf("second row should name the user: %+v", r)
	}
}

// 170.11: with the setting off scrap and rework only log; on, scrap leaves stock at
// cost to 5360 and rework opens a linked order. A scrap the stock cannot cover
// posts nothing.
func TestScrapAndReworkMoveStockAndCostOnlyWhenOn(t *testing.T) {
	f := newCostingFixture(t, 0)
	f.order("PO-SCR", 10, "Material Issued")

	if err := PostScrap(f.tenantID, "PO-SCR", "RAW", 3, "bent", "tester"); err != nil {
		t.Fatalf("scrap (off): %v", err)
	}
	if err := SendToRework(f.tenantID, "PO-SCR", 2, "scratched", "tester"); err != nil {
		t.Fatalf("rework (off): %v", err)
	}
	if f.onHand("RAW") != 40 || f.glRows() != 0 {
		t.Fatalf("setting off must change nothing: stock %v, gl rows %d", f.onHand("RAW"), f.glRows())
	}
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".documents WHERE doctype = 'ProductionOrder'").Scan(&n)
	if n != 1 {
		t.Fatalf("setting off must open no rework order, orders: %d", n)
	}

	if err := SetSetting(f.tenantID, SettingProductionScrapRework, "true", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := PostScrap(f.tenantID, "PO-SCR", "RAW", 3, "bent", "tester"); err != nil {
		t.Fatalf("scrap (on): %v", err)
	}
	if f.onHand("RAW") != 37 || f.glNet("5360") != 3000 || f.glNet("1200") != -3000 {
		t.Fatalf("scrap: stock %v, 5360 %d, 1200 %d", f.onHand("RAW"), f.glNet("5360"), f.glNet("1200"))
	}
	rows := f.glRows()
	if err := PostScrap(f.tenantID, "PO-SCR", "RAW", 500, "too many", "tester"); err == nil {
		t.Fatalf("scrapping more than is in stock must be refused")
	}
	if f.onHand("RAW") != 37 || f.glRows() != rows {
		t.Fatalf("a refused scrap must post nothing: stock %v, gl rows %d vs %d", f.onHand("RAW"), f.glRows(), rows)
	}
	data, _, _ := fetchProductionOrder(f.tenantID, "PO-SCR")
	var log []scrapEntry
	_ = json.Unmarshal([]byte(data["scrap_log"].(string)), &log)
	if len(log) != 2 || log[1].CostPaise != 3000 {
		t.Fatalf("scrap log should hold the legacy entry and the costed one: %+v", log)
	}

	if err := SendToRework(f.tenantID, "PO-SCR", 4, "scratched", "tester"); err != nil {
		t.Fatalf("rework (on): %v", err)
	}
	rw, status, err := fetchProductionOrder(f.tenantID, "PO-SCR-RW2")
	if err != nil || status != "Draft" || rw["rework_of"] != "PO-SCR" || numFromInterface(rw["quantity"]) != 4 {
		t.Fatalf("rework order: %v %s %+v", err, status, rw)
	}
	wantMfgCode(t, SendToRework(f.tenantID, "PO-SCR", 9, "too many", "tester"), "MFG-1744") // 2 + 4 + 9 > 10
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".documents WHERE doctype = 'ProductionOrder'").Scan(&n)
	if n != 2 {
		t.Fatalf("a refused rework must open no order, orders: %d", n)
	}
}

// 170.12 + 170.13: material issued posts Dr WIP / Cr Inventory, a completion moves
// its cost to finished goods and splits by-product cost by share, and WIP nets to
// zero once the order closes. Completing twice posts once; a completion that
// fails posts nothing; what is left in WIP at close goes to the variance account.
func TestWIPAccountingAndByProductCostShare(t *testing.T) {
	f := newCostingFixture(t, 25)
	if err := SetSetting(f.tenantID, SettingProductionWIP, "true", "tester"); err != nil {
		t.Fatal(err)
	}
	f.order("PO-WIP", 4, "Draft")
	if err := IssueProductionMaterial(f.tenantID, "PO-WIP"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if f.glNet("1250") != 8000 || f.glNet("1200") != -8000 {
		t.Fatalf("issue: WIP %d, inventory %d", f.glNet("1250"), f.glNet("1200"))
	}
	if err := ReportPartialCompletion(f.tenantID, "PO-WIP", 1); err != nil {
		t.Fatalf("partial: %v", err)
	}
	if f.glNet("1250") != 6000 { // a quarter of 8000 left WIP
		t.Fatalf("after partial WIP = %d, want 6000", f.glNet("1250"))
	}
	if err := CompleteProductionOrder(f.tenantID, "PO-WIP"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if f.glNet("1250") != 0 || f.glNet("1200") != 0 || f.glNet("5370") != 0 {
		t.Fatalf("closed: WIP %d, inventory %d, variance %d", f.glNet("1250"), f.glNet("1200"), f.glNet("5370"))
	}
	// 75% of 8000 to the main product over 4 units, 25% to the by-product over 4 units.
	if f.avgCost("FG") != 1500 || f.avgCost("BP") != 500 {
		t.Fatalf("cost split: FG %d, BP %d", f.avgCost("FG"), f.avgCost("BP"))
	}
	rows := f.glRows()
	if err := CompleteProductionOrder(f.tenantID, "PO-WIP"); err == nil {
		t.Fatalf("completing a closed order must be refused")
	}
	if f.glRows() != rows || f.onHand("FG") != 4 {
		t.Fatalf("a repeat completion must post nothing: gl %d vs %d, FG %v", f.glRows(), rows, f.onHand("FG"))
	}

	// Partial failure: shares over 100% stop the completion before anything posts.
	if _, err := db.DB.Exec(`UPDATE ` + f.schema + `.documents SET data = jsonb_set(data, '{by_products}', to_jsonb('[{"sku":"BP","qty_per_unit":1,"cost_share_percent":70},{"sku":"BP","qty_per_unit":1,"cost_share_percent":60}]'::text)) WHERE id = 'BOM-170'`); err != nil {
		t.Fatal(err)
	}
	f.order("PO-WIP2", 2, "Draft")
	if err := IssueProductionMaterial(f.tenantID, "PO-WIP2"); err != nil {
		t.Fatalf("issue 2: %v", err)
	}
	rows, fg := f.glRows(), f.onHand("FG")
	wantMfgCode(t, CompleteProductionOrder(f.tenantID, "PO-WIP2"), "MFG-1743")
	if f.glRows() != rows || f.onHand("FG") != fg {
		t.Fatalf("a refused completion must post nothing: gl %d vs %d, FG %v vs %v", f.glRows(), rows, f.onHand("FG"), fg)
	}

	// Variance: WIP carrying 500 paise the finished goods do not take (an overhead
	// charged to the order outside this flow) goes to 5370 when the order closes.
	if _, err := db.DB.Exec(`UPDATE ` + f.schema + `.documents SET data = jsonb_set(data, '{by_products}', to_jsonb('[]'::text)) WHERE id = 'BOM-170'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE ` + f.schema + `.documents SET data = jsonb_set(data, '{wip_paise}', to_jsonb(4500)) WHERE id = 'PO-WIP2'`); err != nil {
		t.Fatal(err)
	}
	before := f.glNet("1250")
	if err := CompleteProductionOrder(f.tenantID, "PO-WIP2"); err != nil {
		t.Fatalf("complete 2: %v", err)
	}
	if f.glNet("5370") != 500 || f.glNet("1250") != before-4000-500 {
		t.Fatalf("variance %d, WIP moved %d", f.glNet("5370"), f.glNet("1250")-before)
	}
}

// With both settings off and no cost share, completing posts no ledger entries
// and the by-product still comes in at no cost.
func TestProductionUnchangedWithSettingsOff(t *testing.T) {
	f := newCostingFixture(t, 0)
	f.order("PO-OFF", 4, "Draft")
	if err := IssueProductionMaterial(f.tenantID, "PO-OFF"); err != nil {
		t.Fatal(err)
	}
	if err := CompleteProductionOrder(f.tenantID, "PO-OFF"); err != nil {
		t.Fatal(err)
	}
	if f.glRows() != 0 || f.avgCost("BP") != 0 || f.avgCost("FG") != 2000 {
		t.Fatalf("gl rows %d, BP %d, FG %d", f.glRows(), f.avgCost("BP"), f.avgCost("FG"))
	}
}
