package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// TestStage14614ReorderCountsStockOnOrder: the suggestion counts what is
// already on an open PO, in transit, inward or waiting, and honours the
// config's min, max and multiple.
func TestStage14614ReorderCountsStockOnOrder(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		loc  = "S14614-LOC"
		from = "S14614-FROM"
		sA   = "S14614-A" // open PO covers the shortfall
		sB   = "S14614-B" // config: multiple 12, min 24
		sC   = "S14614-C" // in transit + waiting orders
		sD   = "S14614-D" // max 100
	)
	ids := []string{"S14614-PO1", "S14614-CFG-B", "S14614-CFG-D", "S14614-TO1", "S14614-SOL1"}
	clean := func() {
		for _, id := range ids {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE location_code = '" + loc + "'")
	}
	clean()
	defer clean()
	put := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	for _, s := range []string{sA, sB, sC, sD} {
		if _, err := db.DB.Exec(fmt.Sprintf("INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 0, 0)", schema), s, loc); err != nil {
			t.Fatalf("seed stock: %v", err)
		}
	}
	// Safety stock 20 is the reorder point (no sales, so no velocity).
	items, _ := json.Marshal([]map[string]interface{}{{"sku": sA, "qty": 20}})
	put("S14614-PO1", "PurchaseOrder", "Approved", map[string]interface{}{"items": string(items), "target_warehouse": loc, "vendor": "V"})
	put("S14614-CFG-B", "ReorderPointConfig", "Active", map[string]interface{}{"code": "S14614-CFG-B", "item": sB, "location_code": loc, "lead_time_days": 1.0, "safety_stock_qty": 20.0, "order_multiple": 12.0, "min_order_qty": 24.0})
	put("S14614-CFG-D", "ReorderPointConfig", "Active", map[string]interface{}{"code": "S14614-CFG-D", "item": sD, "location_code": loc, "lead_time_days": 1.0, "safety_stock_qty": 20.0, "max_stock_qty": 100.0})
	titems, _ := json.Marshal([]map[string]interface{}{{"sku": sC, "qty": 15}})
	bal, _ := json.Marshal([]map[string]interface{}{{"sku": sC, "dispatched": 15}})
	put("S14614-TO1", "TransferOrder", "Dispatched", map[string]interface{}{"items": string(titems), "from_warehouse": from, "to_warehouse": loc, "transfer_balance": string(bal)})
	put("S14614-SOL1", "SalesOrderLine", "Pending", map[string]interface{}{"sku": sC, "qty": 5, "location_code": loc})

	got, err := GetReplenishmentSuggestionsConfigured(tenantID, loc, 1, 20)
	if err != nil {
		t.Fatalf("suggestions: %v", err)
	}
	by := map[string]ReplenishmentSuggestion{}
	for _, s := range got {
		by[s.SKU] = s
	}
	if s, ok := by[sA]; ok {
		t.Fatalf("an open PO for the shortfall must stop a new proposal, got %+v", s)
	}
	if s := by[sB]; s.SuggestedQty != 24 {
		t.Fatalf("B: need 20 -> min 24 (a multiple of 12) = 24, got %+v", s)
	}
	// C: position 0 + 15 in transit - 5 waiting = 10 -> need 10.
	if s := by[sC]; s.SuggestedQty != 10 || s.InTransit != 15 || s.BackOrders != 5 || s.Position != 10 {
		t.Fatalf("C: want position 10 / suggest 10, got %+v", s)
	}
	// D: order up to max 100 from position 0.
	if s := by[sD]; s.SuggestedQty != 100 {
		t.Fatalf("D: want 100 (up to max), got %+v", s)
	}

	// The plain entry point (served by the operations handler) counts the same
	// stock on order; it ignores per-item config, so B and D use the defaults.
	legacy, err := GetReplenishmentSuggestions(tenantID, loc, 1, 20)
	if err != nil {
		t.Fatalf("legacy suggestions: %v", err)
	}
	lby := map[string]ReplenishmentSuggestion{}
	for _, s := range legacy {
		lby[s.SKU] = s
	}
	if s, ok := lby[sA]; ok {
		t.Fatalf("legacy path: an open PO for the shortfall must stop a new proposal, got %+v", s)
	}
	if s := lby[sC]; s.SuggestedQty != 10 || s.Position != 10 {
		t.Fatalf("legacy path C: want position 10 / suggest 10, got %+v", s)
	}
	if s := lby[sB]; s.SuggestedQty != 20 {
		t.Fatalf("legacy path B: defaults, want 20, got %+v", s)
	}
}
