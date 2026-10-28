package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 132.22 (found by Guide on app): a production order took its raw
// material out of stock at cost, but the finished good came in with no cost,
// so Inventory Valuation fell below GL 1200 by the material used. The finished
// good now carries that material's value: a production run leaves the stock's
// total value unchanged.
func TestStage132ProductionCarriesMaterialCost(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	const loc = "MFG-132"
	insert("FG-132", "Item", "Active", map[string]interface{}{"code": "FG-132", "name": "Stool"})
	insert("RM-132", "Item", "Active", map[string]interface{}{"code": "RM-132", "name": "Plank"})
	comps, _ := json.Marshal([]map[string]interface{}{{"sku": "RM-132", "qty": 2}})
	insert("BOM-132", "BOM", "Active", map[string]interface{}{"code": "BOM-132", "parent_item": "FG-132", "components": string(comps), "status": "Active"})
	insert("PRO-132", "ProductionOrder", "Draft", map[string]interface{}{"bom_id": "BOM-132", "quantity": 3, "location": loc, "status": "Draft"})
	// 10 planks received at Rs 50.00 each.
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "RM-132", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	if err := upsertItemCostReceipt(schema, "RM-132", 10, 50000); err != nil {
		t.Fatal(err)
	}
	stockValue := func() int64 {
		t.Helper()
		var v int64
		if err := db.DB.QueryRow(`SELECT COALESCE(SUM(ia.on_hand * COALESCE(ic.avg_unit_cost_paise, 0)), 0)::bigint
			FROM ` + schema + `.inventory_availability ia LEFT JOIN ` + schema + `.item_cost ic ON ic.item_code = ia.sku`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	if err := IssueProductionMaterial(tenantID, "PRO-132"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := ReportPartialCompletion(tenantID, "PRO-132", 1); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	var avg int64
	_ = db.DB.QueryRow(`SELECT avg_unit_cost_paise FROM ` + schema + `.item_cost WHERE item_code = 'FG-132'`).Scan(&avg)
	if avg != 10000 {
		t.Fatalf("finished good cost after 1 of 3 = %d paise, want 10000 (2 planks x Rs 50)", avg)
	}
	if err := CompleteProductionOrder(tenantID, "PRO-132"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	// 4 planks left (Rs 200) + 3 stools at Rs 100 = Rs 500, what the 10 planks cost.
	if v := stockValue(); v != 50000 {
		t.Fatalf("stock value after production = %d paise, want 50000 (unchanged)", v)
	}
	data, status, _ := fetchProductionOrder(tenantID, "PRO-132")
	if status != "Completed" || numFromInterface(data["material_cost_allocated_paise"]) != 30000 {
		t.Fatalf("order %s, material allocated %v - want Completed, 30000", status, data["material_cost_allocated_paise"])
	}
}
