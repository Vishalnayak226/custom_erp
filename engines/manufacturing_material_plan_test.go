package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestProductionMaterialPlanStage5512 runs 55.12's three production
// scenarios on a provisioned throwaway client: the materials check agrees
// with Issue Material (scrap % folded in, backflushed packaging never
// blocking), a short order is refused with the same shortfall the check
// showed, and a partial output keeps the order open with its material issued
// once and the finished goods received for what was made.
func TestProductionMaterialPlanStage5512(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
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
	const loc = "MFG-55"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Workshop", "type": "Warehouse"})
	insert("FG", "Item", "Active", map[string]interface{}{"code": "FG", "name": "Finished bangle", "hsn_code": "7113", "tax_treatment": "Exempt"})
	insert("RAW", "Item", "Active", map[string]interface{}{"code": "RAW", "name": "Silver wire", "hsn_code": "7113", "tax_treatment": "Exempt"})
	insert("PACK", "Item", "Active", map[string]interface{}{"code": "PACK", "name": "Gift box", "item_type": "Packaging", "hsn_code": "4819", "tax_treatment": "Exempt"})
	comps, _ := json.Marshal([]map[string]interface{}{{"sku": "RAW", "qty": 2, "scrap_percent": 25}, {"sku": "PACK", "qty": 1}})
	insert("BOM-55", "BOM", "Active", map[string]interface{}{"code": "BOM-55", "parent_item": "FG", "components": string(comps), "status": "Active"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "RAW", "qty": 10.0}, map[string]interface{}{"sku": "PACK", "qty": 2.0}}, false); err != nil {
		t.Fatalf("stock: %v", err)
	}
	avail := func(sku string) (onHand, available float64) {
		_ = db.DB.QueryRow("SELECT on_hand, available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&onHand, &available)
		return
	}
	need := func(lines []MaterialNeed, sku string) MaterialNeed {
		for _, l := range lines {
			if l.Sku == sku {
				return l
			}
		}
		t.Fatalf("no line for %s in %+v", sku, lines)
		return MaterialNeed{}
	}

	// Normal: 4 units need 10 wire (2 + 25% scrap) and 4 boxes; the boxes
	// are backflushed, so their shortfall does not stop the issue.
	plan, err := ProductionMaterialPlan(tenantID, "BOM-55", 4, loc)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if w := need(plan, "RAW"); w.Required != 10 || w.Available != 10 || w.Short != 0 || w.Backflush || w.Name != "Silver wire" {
		t.Fatalf("wire: %+v", w)
	}
	if b := need(plan, "PACK"); !b.Backflush || b.Required != 4 || b.Short != 2 {
		t.Fatalf("box: %+v", b)
	}

	// Short material: the check and Issue Material agree on 13 vs 10 - 12.5
	// rounds up to whole units (73.B.1).
	insert("PRO-55-SHORT", "ProductionOrder", "Draft", map[string]interface{}{"bom_id": "BOM-55", "quantity": 5, "location": loc, "status": "Draft"})
	_, _, _, plan, err = ProductionOrderMaterialPlan(tenantID, "PRO-55-SHORT")
	if w := need(plan, "RAW"); err != nil || w.Required != 13 || w.Short != 3 {
		t.Fatalf("short plan: %v %+v", err, w)
	}
	if err := IssueProductionMaterial(tenantID, "PRO-55-SHORT"); err == nil {
		t.Fatalf("issuing a short order must be refused")
	} else if ve, ok := err.(*ValidationError); !ok || ve.Code != "MANUFA-0143" {
		t.Fatalf("want MANUFA-0143, got %v", err)
	}
	if _, a := avail("RAW"); a != 10 {
		t.Fatalf("a refused issue must not move stock, wire available %v", a)
	}

	// Partial output: issue 4, report 1 made - wire issued once, one box
	// backflushed, one finished unit received, order still open for 3.
	insert("PRO-55", "ProductionOrder", "Draft", map[string]interface{}{"bom_id": "BOM-55", "quantity": 4, "location": loc, "status": "Draft"})
	if err := IssueProductionMaterial(tenantID, "PRO-55"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, a := avail("RAW"); a != 0 {
		t.Fatalf("wire after issue: %v", a)
	}
	if err := IssueProductionMaterial(tenantID, "PRO-55"); err == nil {
		t.Fatalf("material must not be issued twice")
	}
	if err := ReportPartialCompletion(tenantID, "PRO-55", 1); err != nil {
		t.Fatalf("partial: %v", err)
	}
	data, status, _ := fetchProductionOrder(tenantID, "PRO-55")
	if status == "Completed" || numFromInterface(data["completed_qty"]) != 1 {
		t.Fatalf("after partial: %s %+v", status, data)
	}
	if fg, _ := avail("FG"); fg != 1 {
		t.Fatalf("finished goods received: %v", fg)
	}
	if box, _ := avail("PACK"); box != 1 {
		t.Fatalf("one box backflushed for one unit, left %v", box)
	}
	if _, a := avail("RAW"); a != 0 {
		t.Fatalf("a partial output must not issue wire again: %v", a)
	}

	// Stage 132.18: every movement names the order that made it - the wire
	// issued, the finished unit and the backflushed box - not a blank
	// "StockAdjustment" the stock ledger cannot trace.
	var named, blank int
	_ = db.DB.QueryRow("SELECT COUNT(*) FILTER (WHERE data->>'voucher_type' = 'ProductionOrder' AND data->>'voucher_id' = 'PRO-55'), COUNT(*) FILTER (WHERE COALESCE(data->>'voucher_id', '') = '' AND data->>'sku' = 'FG') FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry'").Scan(&named, &blank)
	if named < 3 || blank != 0 {
		t.Fatalf("stock ledger: %d lines name PRO-55 (want the wire, the unit and the box), %d finished-goods lines name nothing", named, blank)
	}
}
