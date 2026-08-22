package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 42.5 (Inventory control depth) engine tests: physical inventory
// (freeze/count/reconcile/close), CycleClass, demand-driven replenishment,
// facility hierarchy/copy, and slotting v2. Kept in its own file, same
// per-Stage convention wms_p2_test.go/wms_enterprise_test.go already use.
func TestStage42_5InventoryControlDepth(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	seedLocation := func(code string) {
		data, _ := json.Marshal(map[string]interface{}{"code": code, "name": code, "type": "Warehouse", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system') ON CONFLICT (id) DO UPDATE SET data = $2, status = 'Active'", code, data); err != nil {
			t.Fatalf("failed to seed location %s: %v", code, err)
		}
	}
	seedBin := func(binCode, location, zone, binType string) {
		data := map[string]interface{}{"bin_code": binCode, "location": location, "bin_type": binType, "capacity": 1000}
		if zone != "" {
			data["zone"] = zone
		}
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system') ON CONFLICT (id) DO UPDATE SET data = $2, status = 'Active'", "BINDOC-"+binCode, b); err != nil {
			t.Fatalf("failed to seed bin %s: %v", binCode, err)
		}
	}
	seedBinStock := func(binCode, sku, location string, qty int) {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1,$2,$3,'Good',$4) ON CONFLICT (bin_code, sku, condition) DO UPDATE SET qty = $4", binCode, sku, location, qty); err != nil {
			t.Fatalf("failed to seed bin_stock: %v", err)
		}
	}
	seedAvailability := func(sku, location string, onHand, available int) {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1,$2,$3,$4) ON CONFLICT (sku,location_code) DO UPDATE SET on_hand=$3, available=$4",
			sku, location, onHand, available); err != nil {
			t.Fatalf("failed to seed inventory_availability: %v", err)
		}
	}
	seedItem := func(sku, status string) {
		data, _ := json.Marshal(map[string]interface{}{"code": sku, "name": sku, "barcode": sku, "hsn_code": "0000", "gst_rate": 0})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, $3, 'system') ON CONFLICT (id) DO UPDATE SET data = $2, status = $3", "ITEM-"+sku, data, status); err != nil {
			t.Fatalf("failed to seed item %s: %v", sku, err)
		}
	}

	t.Run("PhysicalInventoryFreezeCountReconcileClose", func(t *testing.T) {
		location := "WH-PI-TEST"
		bin := "BIN-PI-01"
		sku := "SKU-PI-01"
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE bin_code = 'BIN-PI-01'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id IN ('BINDOC-BIN-PI-01') OR (doctype = 'CycleCountLine' AND data->>'location' = 'WH-PI-TEST') OR (doctype = 'PhysicalInventory' AND data->>'location' = 'WH-PI-TEST')")

		seedLocation(location)
		seedBin(bin, location, "", "PickFace")
		seedBinStock(bin, sku, location, 40)
		seedAvailability(sku, location, 40, 40)

		piID, lineCount, err := StartPhysicalInventory(tenantID, location, "", "system")
		if err != nil {
			t.Fatalf("StartPhysicalInventory failed: %v", err)
		}
		if lineCount == 0 {
			t.Fatalf("expected at least one line, got 0")
		}

		var binStatus string
		if err := db.DB.QueryRow("SELECT COALESCE(data->>'bin_status','') FROM "+schema+".documents WHERE id = $1", "BINDOC-"+bin).Scan(&binStatus); err != nil {
			t.Fatalf("failed to read bin status: %v", err)
		}
		if binStatus != "Counting" {
			t.Fatalf("expected bin to be frozen (Counting), got %q", binStatus)
		}

		// Picking must be refused while the bin is frozen.
		candidates, shortfall, err := AllocateFromStock(tenantID, sku, location, 5)
		if err != nil {
			t.Fatalf("AllocateFromStock failed: %v", err)
		}
		if len(candidates) != 0 || shortfall != 5 {
			t.Fatalf("expected allocation to find nothing from a frozen bin, got candidates=%v shortfall=%d", candidates, shortfall)
		}

		// A second attempt to start another physical inventory over the same
		// already-frozen bin must be refused.
		if _, _, err := StartPhysicalInventory(tenantID, location, "", "system"); err == nil {
			t.Fatalf("expected starting a second overlapping physical inventory to fail")
		}

		var lineID string
		if err := db.DB.QueryRow("SELECT id FROM "+schema+".documents WHERE doctype = 'CycleCountLine' AND data->>'physical_inventory' = $1 AND data->>'sku' = $2", piID, sku).Scan(&lineID); err != nil {
			t.Fatalf("failed to find generated line: %v", err)
		}
		// Count exactly matches on-hand - zero variance, should auto-post on reconcile.
		if err := SubmitPhysicalInventoryCount(tenantID, lineID, 40, "system"); err != nil {
			t.Fatalf("SubmitPhysicalInventoryCount failed: %v", err)
		}

		posted, pendingApproval, err := ReconcilePhysicalInventory(tenantID, piID, "system", "HR/Admin")
		if err != nil {
			t.Fatalf("ReconcilePhysicalInventory failed: %v", err)
		}
		if posted != 1 || pendingApproval != 0 {
			t.Fatalf("expected 1 posted / 0 pending for a zero-variance count, got posted=%d pending=%d", posted, pendingApproval)
		}

		if err := ClosePhysicalInventory(tenantID, piID, "system"); err != nil {
			t.Fatalf("ClosePhysicalInventory failed: %v", err)
		}

		if err := db.DB.QueryRow("SELECT COALESCE(data->>'bin_status','') FROM "+schema+".documents WHERE id = $1", "BINDOC-"+bin).Scan(&binStatus); err != nil {
			t.Fatalf("failed to re-read bin status: %v", err)
		}
		if binStatus == "Counting" {
			t.Fatalf("expected bin to be unfrozen after close, still Counting")
		}
		// Picking should work again now that the bin is unfrozen.
		candidates, shortfall, err = AllocateFromStock(tenantID, sku, location, 5)
		if err != nil {
			t.Fatalf("AllocateFromStock after close failed: %v", err)
		}
		if len(candidates) == 0 || shortfall != 0 {
			t.Fatalf("expected allocation to succeed after unfreeze, got candidates=%v shortfall=%d", candidates, shortfall)
		}
	})

	t.Run("CycleClassOverridesDefaultABCSplit", func(t *testing.T) {
		location := "WH-CC-TEST"
		sku := "SKU-CC-01"
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'CycleClass' AND data->>'code' LIKE 'CC-TEST-%'")
		seedLocation(location)
		seedAvailability(sku, location, 10, 10)

		// No CycleClass rows yet - GetCycleCountPlan must fall back to the
		// fixed A/B/C GetABCCycleCountPlan behaviour (a single SKU always
		// lands in tier A, the top of the ranking).
		plan, err := GetCycleCountPlan(tenantID, location)
		if err != nil {
			t.Fatalf("GetCycleCountPlan (fallback) failed: %v", err)
		}
		if len(plan) != 1 || plan[0].Tier != "A" {
			t.Fatalf("expected fallback plan to classify the single SKU as tier A, got %+v", plan)
		}

		classData, _ := json.Marshal(map[string]interface{}{
			"code": "CC-TEST-HOT", "name": "Hot", "sequence": 1, "pareto_cutoff_pct": 100, "interval_days": 7, "status": "Active",
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CycleClass', $2, 'Active', 'system')",
			"CC-TEST-HOT", classData); err != nil {
			t.Fatalf("failed to seed CycleClass: %v", err)
		}

		plan, err = GetCycleCountPlan(tenantID, location)
		if err != nil {
			t.Fatalf("GetCycleCountPlan (custom) failed: %v", err)
		}
		if len(plan) != 1 || plan[0].Tier != "CC-TEST-HOT" || plan[0].IntervalDays != 7 {
			t.Fatalf("expected the SKU classified under the custom CycleClass, got %+v", plan)
		}
	})

	t.Run("DemandDrivenReplenishmentFiresAboveMinQty", func(t *testing.T) {
		location := "WH-DD-TEST"
		pickBin := "BIN-DD-PICK"
		reserveBin := "BIN-DD-RESERVE"
		sku := "SKU-DD-01"
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE bin_code IN ('BIN-DD-PICK','BIN-DD-RESERVE')")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'from_bin' = 'BIN-DD-PICK'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'BinReplenishmentRule' AND data->>'bin_code' = 'BIN-DD-PICK'")

		seedLocation(location)
		seedBin(pickBin, location, "", "PickFace")
		seedBin(reserveBin, location, "", "Reserve")
		// Above min_qty on purpose - a static min/max scan would ignore this bin.
		seedBinStock(pickBin, sku, location, 10)
		seedBinStock(reserveBin, sku, location, 100)

		ruleData, _ := json.Marshal(map[string]interface{}{"bin_code": pickBin, "sku": sku, "min_qty": 5, "max_qty": 20, "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'BinReplenishmentRule', $2, 'Active', 'system')",
			"BRR-DD-TEST", ruleData); err != nil {
			t.Fatalf("failed to seed BinReplenishmentRule: %v", err)
		}
		taskData, _ := json.Marshal(map[string]interface{}{
			"code": "WT-DD-TEST", "task_type": "Pick", "location_code": location, "from_bin": pickBin, "item": sku, "qty": 25,
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'WarehouseTask', $2, 'Pending', 'system')",
			"WT-DD-TEST", taskData); err != nil {
			t.Fatalf("failed to seed WarehouseTask: %v", err)
		}

		suggestions, err := GetDemandDrivenReplenishmentSuggestions(tenantID, location)
		if err != nil {
			t.Fatalf("GetDemandDrivenReplenishmentSuggestions failed: %v", err)
		}
		var found bool
		for _, s := range suggestions {
			if s.BinCode == pickBin && s.Sku == sku {
				found = true
				if s.Shortage != 15 { // 25 open demand - 10 on hand
					t.Errorf("expected shortage 15, got %d", s.Shortage)
				}
			}
		}
		if !found {
			t.Fatalf("expected a demand-driven suggestion for %s/%s despite being above min_qty, got %+v", pickBin, sku, suggestions)
		}
	})

	t.Run("FacilityHierarchyAndCopy", func(t *testing.T) {
		root := "FAC-ROOT-TEST"
		child := "FAC-CHILD-TEST"
		grandchild := "FAC-GC-TEST"
		target := "FAC-TARGET-TEST"
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype IN ('Zone','Bin') AND data->>'location' = 'FAC-TARGET-TEST'")

		seedLocation(root)
		seedLocation(child)
		seedLocation(grandchild)
		seedLocation(target)
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = data || '{\"parent\": \""+root+"\"}'::jsonb WHERE doctype='Location' AND id=$1", child); err != nil {
			t.Fatalf("failed to set parent on child: %v", err)
		}
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = data || '{\"parent\": \""+child+"\"}'::jsonb WHERE doctype='Location' AND id=$1", grandchild); err != nil {
			t.Fatalf("failed to set parent on grandchild: %v", err)
		}

		descendants, err := GetFacilityDescendants(tenantID, root)
		if err != nil {
			t.Fatalf("GetFacilityDescendants failed: %v", err)
		}
		want := map[string]bool{root: true, child: true, grandchild: true}
		if len(descendants) != 3 {
			t.Fatalf("expected 3 descendants (root+2), got %v", descendants)
		}
		for _, d := range descendants {
			if !want[d] {
				t.Errorf("unexpected descendant %s", d)
			}
		}

		sku := "SKU-FAC-01"
		seedAvailability(sku, root, 5, 5)
		seedAvailability(sku, grandchild, 7, 7)
		rollup, err := GetFacilityRollup(tenantID, root)
		if err != nil {
			t.Fatalf("GetFacilityRollup failed: %v", err)
		}
		var gotTotal int
		for _, r := range rollup {
			if r.Sku == sku {
				gotTotal = r.OnHand
			}
		}
		if gotTotal != 12 {
			t.Fatalf("expected rollup on_hand 12 (5+7), got %d", gotTotal)
		}

		zoneData, _ := json.Marshal(map[string]interface{}{"code": "FAC-ROOT-TEST-Z1", "location": root, "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Zone', $2, 'Active', 'system') ON CONFLICT (id) DO UPDATE SET data = $2",
			"FAC-ROOT-TEST-Z1", zoneData); err != nil {
			t.Fatalf("failed to seed Zone: %v", err)
		}
		seedBin("FAC-ROOT-TEST-B1", root, "FAC-ROOT-TEST-Z1", "PickFace")

		zonesCopied, binsCopied, err := CopyFacilityConfig(tenantID, root, target, "system")
		if err != nil {
			t.Fatalf("CopyFacilityConfig failed: %v", err)
		}
		if zonesCopied != 1 || binsCopied != 1 {
			t.Fatalf("expected 1 zone and 1 bin copied, got zones=%d bins=%d", zonesCopied, binsCopied)
		}
		var copiedBinLocation, copiedBinZone string
		if err := db.DB.QueryRow("SELECT data->>'location', COALESCE(data->>'zone','') FROM "+schema+".documents WHERE doctype='Bin' AND id = $1",
			"FAC-TARGET-TEST-B1").Scan(&copiedBinLocation, &copiedBinZone); err != nil {
			t.Fatalf("expected cloned bin to exist: %v", err)
		}
		if copiedBinLocation != target || copiedBinZone != "FAC-TARGET-TEST-Z1" {
			t.Fatalf("expected cloned bin at %s in zone FAC-TARGET-TEST-Z1, got location=%s zone=%s", target, copiedBinLocation, copiedBinZone)
		}
	})

	t.Run("SlottingV2UnslottingAndConsolidation", func(t *testing.T) {
		location := "WH-SV2-TEST"
		pickBin := "BIN-SV2-PICK"
		reserveA := "BIN-SV2-RES-A"
		reserveB := "BIN-SV2-RES-B"
		skuDead := "SKU-SV2-DEAD"
		skuSplit := "SKU-SV2-SPLIT"
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE bin_code IN ('BIN-SV2-PICK','BIN-SV2-RES-A','BIN-SV2-RES-B')")

		seedLocation(location)
		seedBin(pickBin, location, "", "PickFace")
		seedBin(reserveA, location, "", "Reserve")
		seedBin(reserveB, location, "", "Reserve")

		seedItem(skuDead, "Cancelled")
		seedBinStock(pickBin, skuDead, location, 8)

		unslot, err := GetUnslottingSuggestions(tenantID, location)
		if err != nil {
			t.Fatalf("GetUnslottingSuggestions failed: %v", err)
		}
		var sawUnslot bool
		for _, s := range unslot {
			if s.Sku == skuDead && s.FromBinCode == pickBin {
				sawUnslot = true
			}
		}
		if !sawUnslot {
			t.Fatalf("expected an unslotting suggestion for cancelled item %s, got %+v", skuDead, unslot)
		}

		seedBinStock(reserveA, skuSplit, location, 5)
		seedBinStock(reserveB, skuSplit, location, 90)
		consolidation, err := GetConsolidationSuggestions(tenantID, location)
		if err != nil {
			t.Fatalf("GetConsolidationSuggestions failed: %v", err)
		}
		var sawConsolidation bool
		for _, s := range consolidation {
			if s.Sku == skuSplit && s.FromBinCode == reserveA && s.ToBinCode == reserveB {
				sawConsolidation = true
			}
		}
		if !sawConsolidation {
			t.Fatalf("expected a consolidation suggestion moving %s from %s into %s, got %+v", skuSplit, reserveA, reserveB, consolidation)
		}
	})
}
