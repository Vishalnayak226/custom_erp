package engines

import (
	"encoding/json"
	"fmt"
	"testing"

	"custom_erp/db"
)

// Stage 42.2.5-42.2.10 tests - the rest of the warehouse task spine's
// architectural phase, beyond the 42.2.1-42.2.4 tests in
// warehouse_task_test.go. Same db.InitDB/tenantID="default" convention.

func TestZoneAndBinValidation(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const zoneCode = "ZONE-VALIDATION-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Zone' AND data->>'code' = '" + zoneCode + "'")
	}
	cleanup()
	defer cleanup()

	// A blank zone is always allowed.
	if err := validateBinMasterRules(tenantID, "BIN-VALIDATION-TEST", map[string]interface{}{"bin_code": "B1", "zone": ""}); err != nil {
		t.Errorf("expected a blank zone to be allowed, got %v", err)
	}
	// A zone code that doesn't exist yet is refused.
	if err := validateBinMasterRules(tenantID, "BIN-VALIDATION-TEST", map[string]interface{}{"bin_code": "B1", "zone": zoneCode}); err == nil {
		t.Error("expected an unknown zone code to be refused")
	}

	// Register the Zone, then the same Bin payload is accepted.
	if err := validateZoneMasterRules(tenantID, "ZONEDOC-1", map[string]interface{}{"code": zoneCode, "status": "Active"}); err != nil {
		t.Fatalf("validateZoneMasterRules (new zone): %v", err)
	}
	zoneData, _ := json.Marshal(map[string]interface{}{"code": zoneCode, "status": "Active", "hazmat_allowed": "Yes"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Zone', $2, 'Active', 'system')",
		"ZONEDOC-1", zoneData); err != nil {
		t.Fatalf("seed Zone: %v", err)
	}
	if err := validateBinMasterRules(tenantID, "BIN-VALIDATION-TEST", map[string]interface{}{"bin_code": "B1", "zone": zoneCode}); err != nil {
		t.Errorf("expected a Bin referencing a real Active zone to be allowed, got %v", err)
	}

	// A second Active zone with the same code is refused.
	if err := validateZoneMasterRules(tenantID, "ZONEDOC-2", map[string]interface{}{"code": zoneCode, "status": "Active"}); err == nil {
		t.Error("expected a duplicate Active zone code to be refused")
	}
	// Re-saving the SAME zone document is fine (id excluded from the dup check).
	if err := validateZoneMasterRules(tenantID, "ZONEDOC-1", map[string]interface{}{"code": zoneCode, "status": "Active"}); err != nil {
		t.Errorf("expected re-saving the same zone to be allowed, got %v", err)
	}
}

func TestBinCapacityEnforcement(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sku = "SKU-BINCAP-TEST"
	const bin = "BIN-BINCAP-TEST"
	const loc = "WH-BINCAP-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE bin_code = '" + bin + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Bin' AND data->>'bin_code' = '" + bin + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND data->>'code' = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'location_code' = '" + loc + "'")
	}
	cleanup()
	defer cleanup()

	itemData, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Bin Capacity Test Item", "weight": 2, "volume": 1})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')",
		"ITEM-"+sku, itemData); err != nil {
		t.Fatalf("seed Item: %v", err)
	}
	// capacity (max_qty) = 5, so a putaway of 10 must be refused even though
	// on-hand stock covers it.
	binData, _ := json.Marshal(map[string]interface{}{"bin_code": bin, "location": loc, "status": "Active", "capacity": 5})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')",
		"BINDOC-"+bin, binData); err != nil {
		t.Fatalf("seed Bin: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)",
		sku, loc); err != nil {
		t.Fatalf("seed inventory_availability: %v", err)
	}

	// bin_status = Blocked refuses putaway outright, checked before capacity
	// even matters.
	if _, err := db.DB.Exec(fmt.Sprintf(
		"UPDATE %s.documents SET data = data || '{\"bin_status\": \"Blocked\"}'::jsonb WHERE doctype = 'Bin' AND data->>'bin_code' = $1", schema),
		bin); err != nil {
		t.Fatalf("mark bin Blocked: %v", err)
	}
	if err := PutawayToBin(tenantID, bin, sku, 1, "system"); err == nil {
		t.Error("expected putaway into a Blocked bin to be refused")
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		"UPDATE %s.documents SET data = data || '{\"bin_status\": \"Available\"}'::jsonb WHERE doctype = 'Bin' AND data->>'bin_code' = $1", schema),
		bin); err != nil {
		t.Fatalf("mark bin Available again: %v", err)
	}

	if err := PutawayToBin(tenantID, bin, sku, 10, "system"); err == nil {
		t.Error("expected a putaway exceeding the bin's capacity (5) to be refused")
	}
	if err := PutawayToBin(tenantID, bin, sku, 5, "system"); err != nil {
		t.Fatalf("expected a putaway within capacity to succeed, got %v", err)
	}
	// A further putaway of even 1 more must now be refused (5 already used, capacity 5).
	if err := PutawayToBin(tenantID, bin, sku, 1, "system"); err == nil {
		t.Error("expected a putaway that would exceed a now-full bin to be refused")
	}
}

func TestSuggestPutawayBin(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sku = "SKU-SUGGEST-TEST"
	const loc = "WH-SUGGEST-TEST"
	const zoneNear, zoneFar = "ZONE-SUGGEST-NEAR", "ZONE-SUGGEST-FAR"
	const binNear, binFar = "BIN-SUGGEST-NEAR", "BIN-SUGGEST-FAR"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock_batch WHERE sku = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE sku = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Bin' AND data->>'location' = '" + loc + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Zone' AND data->>'code' IN ('" + zoneNear + "', '" + zoneFar + "')")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'PutawayStrategy' AND data->>'location_code' = '" + loc + "'")
	}
	cleanup()
	defer cleanup()

	// No PutawayStrategy configured yet - no suggestion, no error.
	binCode, reason, err := SuggestPutawayBin(tenantID, sku, loc, 5, "")
	if err != nil {
		t.Fatalf("SuggestPutawayBin (no strategy): %v", err)
	}
	if binCode != "" || reason == "" {
		t.Errorf("expected no suggestion with no configured strategy, got bin=%q reason=%q", binCode, reason)
	}

	seedZone := func(id, code string, seq int) {
		data, _ := json.Marshal(map[string]interface{}{"code": code, "status": "Active", "hazmat_allowed": "Yes", "putaway_sequence": seq})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Zone', $2, 'Active', 'system')", id, data); err != nil {
			t.Fatalf("seed Zone %s: %v", code, err)
		}
	}
	seedZone("ZONEDOC-NEAR", zoneNear, 1)
	seedZone("ZONEDOC-FAR", zoneFar, 99)

	seedBin := func(id, code, zone string) {
		data, _ := json.Marshal(map[string]interface{}{"bin_code": code, "location": loc, "status": "Active", "zone": zone})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')", id, data); err != nil {
			t.Fatalf("seed Bin %s: %v", code, err)
		}
	}
	seedBin("BINDOC-NEAR", binNear, zoneNear)
	seedBin("BINDOC-FAR", binFar, zoneFar)

	stratData, _ := json.Marshal(map[string]interface{}{
		"code": "PS-SUGGEST-TEST", "location_code": loc, "criteria": "zone_sequence,batch_consolidation", "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'PutawayStrategy', $2, 'Active', 'system')",
		"PSDOC-SUGGEST-TEST", stratData); err != nil {
		t.Fatalf("seed PutawayStrategy: %v", err)
	}

	binCode, _, err = SuggestPutawayBin(tenantID, sku, loc, 5, "")
	if err != nil {
		t.Fatalf("SuggestPutawayBin (zone_sequence): %v", err)
	}
	if binCode != binNear {
		t.Errorf("expected zone_sequence to prefer %s (lower putaway_sequence), got %q", binNear, binCode)
	}

	// Existing-batch consolidation overrides zone_sequence when batchNo matches
	// stock already in the FAR bin.
	if _, err := db.DB.Exec(
		"INSERT INTO "+schema+".bin_stock_batch (bin_code, sku, batch_no, condition, location_code, qty) VALUES ($1, $2, 'LOT-1', 'Good', $3, 3)",
		binFar, sku, loc); err != nil {
		t.Fatalf("seed bin_stock_batch: %v", err)
	}
	binCode, reason, err = SuggestPutawayBin(tenantID, sku, loc, 2, "LOT-1")
	if err != nil {
		t.Fatalf("SuggestPutawayBin (batch consolidation): %v", err)
	}
	if binCode != binFar {
		t.Errorf("expected batch consolidation to prefer %s (already holds LOT-1), got %q, reason=%q", binFar, binCode, reason)
	}
}

func TestAllocationStrategyOverride(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sku = "SKU-ALLOCSTRAT-TEST"
	const loc = "WH-ALLOCSTRAT-TEST"
	const binOld, binNew = "BIN-ALLOCSTRAT-OLD", "BIN-ALLOCSTRAT-NEW"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE sku = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'AllocationStrategy' AND data->>'item' = '" + sku + "'")
	}
	cleanup()
	defer cleanup()

	if _, err := db.DB.Exec(
		"INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty, updated_at) VALUES ($1, $2, $3, 'Good', 5, CURRENT_TIMESTAMP - interval '2 days')",
		binOld, sku, loc); err != nil {
		t.Fatalf("seed old bin_stock: %v", err)
	}
	if _, err := db.DB.Exec(
		"INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty, updated_at) VALUES ($1, $2, $3, 'Good', 5, CURRENT_TIMESTAMP)",
		binNew, sku, loc); err != nil {
		t.Fatalf("seed new bin_stock: %v", err)
	}

	// Default (no AllocationStrategy configured): FIFO, oldest bin first.
	cands, _, err := AllocateFromStock(tenantID, sku, loc, 5)
	if err != nil || len(cands) == 0 {
		t.Fatalf("AllocateFromStock (default FIFO): cands=%v err=%v", cands, err)
	}
	if cands[0].BinCode != binOld {
		t.Errorf("expected default FIFO to pick the older bin %s first, got %s", binOld, cands[0].BinCode)
	}

	// Configure LIFO for this item - now the newer bin must come first.
	stratData, _ := json.Marshal(map[string]interface{}{"code": "AS-ALLOCSTRAT-TEST", "item": sku, "strategy": "LIFO", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'AllocationStrategy', $2, 'Active', 'system')",
		"ASDOC-ALLOCSTRAT-TEST", stratData); err != nil {
		t.Fatalf("seed AllocationStrategy: %v", err)
	}
	defer func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'AllocationStrategy' AND id = 'ASDOC-ALLOCSTRAT-TEST'")
	}()

	if got := ResolveAllocationStrategy(tenantID, sku); got != StrategyLIFO {
		t.Errorf("expected ResolveAllocationStrategy to honour the configured LIFO strategy, got %q", got)
	}
	cands, _, err = AllocateFromStock(tenantID, sku, loc, 5)
	if err != nil || len(cands) == 0 {
		t.Fatalf("AllocateFromStock (configured LIFO): cands=%v err=%v", cands, err)
	}
	if cands[0].BinCode != binNew {
		t.Errorf("expected configured LIFO to pick the newer bin %s first, got %s", binNew, cands[0].BinCode)
	}
}

func TestExceptionFollowOnCreatesCountTask(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const loc = "WH-EXCFOLLOWON-TEST"
	const reasonCodeID = "RC-EXCFOLLOWON-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'location_code' = '" + loc + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND id = '" + reasonCodeID + "'")
	}
	cleanup()
	defer cleanup()

	reasonData, _ := json.Marshal(map[string]interface{}{
		"code": reasonCodeID, "description": "Scale variance", "category": "WMS Exception",
		"process_step": "Count", "follow_on_action": "Create Count Task", "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')",
		reasonCodeID, reasonData); err != nil {
		t.Fatalf("seed ReasonCode: %v", err)
	}

	taskID, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Pick", LocationCode: loc, FromBin: "BIN-EXCFOLLOWON", Item: "SKU-EXCFOLLOWON"}, "system")
	if err != nil {
		t.Fatalf("CreateWarehouseTask: %v", err)
	}
	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusException, reasonCodeID, "", "system"); err != nil {
		t.Fatalf("TransitionWarehouseTaskStatus (Exception): %v", err)
	}

	var count int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'WarehouseTask' AND data->>'task_type' = 'Count' AND data->>'source_doc_id' = $1`, schema),
		taskID).Scan(&count); err != nil {
		t.Fatalf("query auto-created Count task: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly one auto-created Count task for the 'Create Count Task' follow-on, got %d", count)
	}
}
