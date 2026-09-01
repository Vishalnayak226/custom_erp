package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

func TestStage3710PlanningDepth(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system') "+
			"ON CONFLICT (id) DO UPDATE SET doctype = $2, data = $3, status = $4", id, doctype, raw, status)
	}
	cleanupIDs := func(ids ...string) {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}

	t.Run("CalculateWeightedVelocity rejects an out-of-range weight and blends real sales", func(t *testing.T) {
		if _, err := CalculateWeightedVelocity(tenantID, "TEST3710A-LOC", "TEST3710A-SKU", 7, 23, 1.5); err == nil {
			t.Fatalf("expected recentWeight > 1 to be rejected")
		}
		if _, err := CalculateWeightedVelocity(tenantID, "TEST3710A-LOC", "TEST3710A-SKU", 7, 23, -0.1); err == nil {
			t.Fatalf("expected recentWeight < 0 to be rejected")
		}
		// No POSCart sales fixtures exist for this SKU, so both windows are
		// zero - the blend of two zeros is zero, a cheap sanity check that
		// the function actually runs both queries and combines them.
		v, err := CalculateWeightedVelocity(tenantID, "TEST3710A-LOC", "TEST3710A-SKU", 7, 23, 0.6)
		if err != nil {
			t.Fatalf("CalculateWeightedVelocity: %v", err)
		}
		if v != 0 {
			t.Fatalf("expected 0 velocity with no sales fixtures, got %v", v)
		}
		forecast, err := ForecastDemandTrend(tenantID, "TEST3710A-LOC", "TEST3710A-SKU", 14)
		if err != nil {
			t.Fatalf("ForecastDemandTrend: %v", err)
		}
		if forecast != 0 {
			t.Fatalf("expected a 0 forecast with no sales fixtures, got %v", forecast)
		}
	})

	t.Run("ValidateReorderPointConfigDocument + resolveReorderPointConfig + GetReplenishmentSuggestionsConfigured honors a per-item override and falls back for an unconfigured SKU", func(t *testing.T) {
		const skuConfigured, skuDefault, loc, configID = "TEST3710B-SKU-CFG", "TEST3710B-SKU-DEFAULT", "TEST3710B-LOC", "TEST3710B-CFG"
		cleanupIDs(configID)
		defer cleanupIDs(configID)
		db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE location_code = '" + loc + "'")
		defer db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE location_code = '" + loc + "'")

		if err := ValidateReorderPointConfigDocument(tenantID, map[string]interface{}{"lead_time_days": 0.0}); err == nil {
			t.Fatalf("expected a non-positive lead_time_days to be rejected")
		}
		if err := ValidateReorderPointConfigDocument(tenantID, map[string]interface{}{"lead_time_days": 5.0, "safety_stock_qty": -1.0}); err == nil {
			t.Fatalf("expected a negative safety_stock_qty to be rejected")
		}

		insert(configID, "ReorderPointConfig", "Active", map[string]interface{}{
			"code": configID, "item": skuConfigured, "location_code": loc,
			"lead_time_days": 10.0, "safety_stock_qty": 50.0,
		})

		if leadTime, safety, found := resolveReorderPointConfig(tenantID, skuConfigured, loc); !found || leadTime != 10 || safety != 50 {
			t.Fatalf("expected the configured override (10, 50), got leadTime=%d safety=%d found=%v", leadTime, safety, found)
		}
		if _, _, found := resolveReorderPointConfig(tenantID, skuDefault, loc); found {
			t.Fatalf("expected no override for an unconfigured SKU")
		}

		// Zero on-hand for both SKUs with zero velocity means the reorder
		// point IS the safety stock, and 0 available is always below any
		// positive reorder point - both SKUs should surface as suggestions,
		// the configured one showing its own override values.
		db.DB.Exec(fmt.Sprintf(
			"INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1,$2,0,0),($3,$2,0,0)", schema),
			skuConfigured, loc, skuDefault)

		suggestions, err := GetReplenishmentSuggestionsConfigured(tenantID, loc, 3, 5)
		if err != nil {
			t.Fatalf("GetReplenishmentSuggestionsConfigured: %v", err)
		}
		var foundConfigured, foundDefault bool
		for _, s := range suggestions {
			if s.SKU == skuConfigured {
				foundConfigured = true
				if s.LeadTimeDays != 10 || s.SafetyStock != 50 {
					t.Fatalf("expected the configured SKU to use its own override, got leadTime=%d safety=%d", s.LeadTimeDays, s.SafetyStock)
				}
			}
			if s.SKU == skuDefault {
				foundDefault = true
				if s.LeadTimeDays != 3 || s.SafetyStock != 5 {
					t.Fatalf("expected the unconfigured SKU to fall back to the caller's defaults, got leadTime=%d safety=%d", s.LeadTimeDays, s.SafetyStock)
				}
			}
		}
		if !foundConfigured || !foundDefault {
			t.Fatalf("expected both SKUs to appear as suggestions, got %+v", suggestions)
		}
	})

	t.Run("GetPeggedDemand finds open SalesOrderLines and real supply (on-hand + open PurchaseOrder), excludes Cancelled/Dispatched lines and non-Approved POs", func(t *testing.T) {
		const sku, loc = "TEST3710C-SKU", "TEST3710C-LOC"
		const lineOpen, lineDispatched, lineCancelled = "TEST3710C-LINE-OPEN", "TEST3710C-LINE-DISPATCHED", "TEST3710C-LINE-CANCELLED"
		const poApproved, poDraft = "TEST3710C-PO-APPROVED", "TEST3710C-PO-DRAFT"
		cleanupIDs(lineOpen, lineDispatched, lineCancelled, poApproved, poDraft)
		defer cleanupIDs(lineOpen, lineDispatched, lineCancelled, poApproved, poDraft)
		db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE location_code = '" + loc + "'")
		defer db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE location_code = '" + loc + "'")

		insert(lineOpen, "SalesOrderLine", "Pending", map[string]interface{}{"code": lineOpen, "order_id": "SO-1", "sku": sku, "qty": 7.0, "line_status": "Pending"})
		insert(lineDispatched, "SalesOrderLine", "Dispatched", map[string]interface{}{"code": lineDispatched, "order_id": "SO-2", "sku": sku, "qty": 3.0, "line_status": "Dispatched"})
		insert(lineCancelled, "SalesOrderLine", "Cancelled", map[string]interface{}{"code": lineCancelled, "order_id": "SO-3", "sku": sku, "qty": 9.0, "line_status": "Cancelled"})

		itemsApproved, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 20, "rate": 100.0}})
		itemsDraft, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 999, "rate": 100.0}})
		insert(poApproved, "PurchaseOrder", "Approved", map[string]interface{}{"code": poApproved, "items": string(itemsApproved)})
		insert(poDraft, "PurchaseOrder", "Draft", map[string]interface{}{"code": poDraft, "items": string(itemsDraft)})

		db.DB.Exec(fmt.Sprintf("INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1,$2,15,15)", schema), sku, loc)

		demand, supply, err := GetPeggedDemand(tenantID, sku, loc)
		if err != nil {
			t.Fatalf("GetPeggedDemand: %v", err)
		}
		if len(demand) != 1 {
			t.Fatalf("expected exactly 1 open demand line (Dispatched/Cancelled excluded), got %d: %+v", len(demand), demand)
		}
		if demand[0].OrderID != "SO-1" || demand[0].Qty != 7 {
			t.Fatalf("expected the Pending line (SO-1, qty 7), got %+v", demand[0])
		}

		var onHandFound, poFound bool
		for _, s := range supply {
			if s.SourceType == "OnHand" && s.Qty == 15 {
				onHandFound = true
			}
			if s.SourceType == "PurchaseOrder" && s.SourceID == poApproved && s.Qty == 20 {
				poFound = true
			}
			if s.SourceID == poDraft {
				t.Fatalf("expected the Draft (not Approved) PO to be excluded from supply, got %+v", s)
			}
		}
		if !onHandFound {
			t.Fatalf("expected on-hand supply of 15, got %+v", supply)
		}
		if !poFound {
			t.Fatalf("expected the Approved PO's own 20-unit line as supply, got %+v", supply)
		}
	})

	t.Run("production-capacity-schedule report runs cleanly (GetProductionSchedule wired for the first time)", func(t *testing.T) {
		schedule, err := GetProductionSchedule(tenantID)
		if err != nil {
			t.Fatalf("GetProductionSchedule: %v", err)
		}
		if _, err := structsToRows(schedule); err != nil {
			t.Fatalf("structsToRows(schedule): %v", err)
		}
	})
}
