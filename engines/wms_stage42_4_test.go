package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 42.4 (Outbound depth) tests - Wave lifecycle + dispatch gate,
// sortation slot lifecycle, cartonization v2's weight ceiling, LPN
// deconsolidation, VAS BOM-mismatch guard, and the pre-ship gate's
// inert-until-configured / hold-blocks behaviour. Same db.InitDB/
// tenantID="default" convention as wms_stage42_3_test.go.

func TestWaveLifecycleTransitions(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Wave' AND id LIKE 'WAVE-TEST-%'")
	}
	cleanup()
	defer cleanup()

	waveID := "WAVE-TEST-1"
	data, _ := json.Marshal(map[string]interface{}{"code": waveID, "location_code": "WH01", "status": "Planned", "created_via": "Manual", "task_count": 0})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Wave', $2, 'Planned', 'system')", waveID, data); err != nil {
		t.Fatalf("seed Wave: %v", err)
	}

	// The validator refuses a skip (Planned -> In Progress).
	if err := validateWaveMasterRules(tenantID, waveID, map[string]interface{}{"status": "In Progress"}); err == nil {
		t.Error("expected the validator to refuse skipping a step")
	}
	// The validator accepts the next step.
	if err := validateWaveMasterRules(tenantID, waveID, map[string]interface{}{"status": "Released"}); err != nil {
		t.Errorf("expected the validator to accept Planned -> Released, got %v", err)
	}

	// TransitionWaveStatus refuses the same skip.
	if err := TransitionWaveStatus(tenantID, waveID, "In Progress", "system"); err == nil {
		t.Error("expected TransitionWaveStatus to refuse skipping a step")
	}
	if err := TransitionWaveStatus(tenantID, waveID, "Released", "system"); err != nil {
		t.Fatalf("expected Planned -> Released to succeed: %v", err)
	}
	if err := TransitionWaveStatus(tenantID, waveID, "In Progress", "system"); err != nil {
		t.Fatalf("expected Released -> In Progress to succeed: %v", err)
	}
	// Complete is refused while an open FulfillmentTask still carries this wave_id.
	taskID := "TASK-WAVE-TEST-1"
	taskData, _ := json.Marshal(map[string]interface{}{"code": taskID, "order_id": "ORD-WAVE-TEST", "location_code": "WH01", "wave_id": waveID})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system')", taskID, taskData); err != nil {
		t.Fatalf("seed FulfillmentTask: %v", err)
	}
	defer func() { _, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + taskID + "'") }()
	if err := TransitionWaveStatus(tenantID, waveID, "Complete", "system"); err == nil {
		t.Error("expected Complete to be refused while an open task carries this wave")
	}
	_, _ = db.DB.Exec("UPDATE "+schema+".documents SET status = 'Packed' WHERE id = $1", taskID)
	if err := TransitionWaveStatus(tenantID, waveID, "Complete", "system"); err != nil {
		t.Errorf("expected Complete to succeed once the task is Packed: %v", err)
	}
}

func TestWaveDispatchGate(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	waveID := "WAVE-GATE-TEST-1"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + waveID + "'")
	}
	cleanup()
	defer cleanup()
	data, _ := json.Marshal(map[string]interface{}{"code": waveID, "location_code": "WH01", "status": "Planned", "created_via": "Manual", "task_count": 0})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Wave', $2, 'Planned', 'system')", waveID, data); err != nil {
		t.Fatalf("seed Wave: %v", err)
	}

	if err := waveDispatchGate(tenantID, waveID); err == nil {
		t.Error("expected a Planned wave to be refused for dispatch")
	}
	if err := TransitionWaveStatus(tenantID, waveID, "Released", "system"); err != nil {
		t.Fatalf("Released transition: %v", err)
	}
	if err := waveDispatchGate(tenantID, waveID); err != nil {
		t.Errorf("expected a Released wave to pass the dispatch gate, got %v", err)
	}
	// A waveID with no registered Wave document is a no-op (backward compatible).
	if err := waveDispatchGate(tenantID, "FREE-TEXT-WAVE-NOT-A-DOCUMENT"); err != nil {
		t.Errorf("expected an unregistered wave_id to be a no-op, got %v", err)
	}
}

func TestSortationSlotLifecycle(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const station = "SORT-TEST-1"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'SortSlot' AND data->>'station' = '" + station + "'")
	}
	cleanup()
	defer cleanup()

	created, err := ProvisionSortSlots(tenantID, station, 2, "system")
	if err != nil {
		t.Fatalf("ProvisionSortSlots: %v", err)
	}
	if created != 2 {
		t.Errorf("expected 2 slots created, got %d", created)
	}
	// Idempotent: a second call creates nothing more.
	if created2, err := ProvisionSortSlots(tenantID, station, 2, "system"); err != nil || created2 != 0 {
		t.Errorf("expected idempotent re-provision to create 0, got %d, err %v", created2, err)
	}

	slot, err := AssignSortSlot(tenantID, station, "WAVE-1", "TASK-SORT-1", "SKU-1", 3, "system")
	if err != nil {
		t.Fatalf("AssignSortSlot: %v", err)
	}
	if slot.Status != "Assigned" {
		t.Errorf("expected Assigned, got %s", slot.Status)
	}
	// Re-assigning the same order reuses its slot rather than claiming a second one.
	slot2, err := AssignSortSlot(tenantID, station, "WAVE-1", "TASK-SORT-1", "SKU-1", 3, "system")
	if err != nil {
		t.Fatalf("re-AssignSortSlot: %v", err)
	}
	if slot2.DocID != slot.DocID {
		t.Errorf("expected the same slot to be reused, got %s vs %s", slot2.DocID, slot.DocID)
	}

	if _, err := ConfirmSortSlot(tenantID, slot.DocID, 2, "system"); err != nil {
		t.Fatalf("ConfirmSortSlot partial: %v", err)
	}
	partial, _ := getSortSlot(schema, slot.DocID)
	if partial.Status != "Assigned" {
		t.Errorf("expected still Assigned after partial confirm, got %s", partial.Status)
	}
	filled, err := ConfirmSortSlot(tenantID, slot.DocID, 1, "system")
	if err != nil {
		t.Fatalf("ConfirmSortSlot completing: %v", err)
	}
	if filled.Status != "Filled" {
		t.Errorf("expected Filled once qty_confirmed reaches qty_expected, got %s", filled.Status)
	}
	if err := ClearSortSlot(tenantID, slot.DocID, "system"); err != nil {
		t.Fatalf("ClearSortSlot: %v", err)
	}
	cleared, _ := getSortSlot(schema, slot.DocID)
	if cleared.Status != "Empty" {
		t.Errorf("expected Empty after clear, got %s", cleared.Status)
	}
}

func TestSuggestCartonizationV2WeightAware(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const cartonType = "CTN-V2-TEST"
	const sku = "SKU-V2-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'CartonType' AND data->>'code' = '" + cartonType + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND data->>'code' = '" + sku + "'")
	}
	cleanup()
	defer cleanup()

	// Carton holds up to 100 units by qty, but only 3kg by weight; each unit weighs 1kg.
	ctnData, _ := json.Marshal(map[string]interface{}{"code": cartonType, "name": "V2 Test Carton", "max_qty_capacity": 100, "max_weight_capacity": 3, "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CartonType', $2, 'Active', 'system')", "CTNDOC-V2", ctnData); err != nil {
		t.Fatalf("seed CartonType: %v", err)
	}
	itemData, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "V2 Test Item", "weight": 1})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEMDOC-V2", itemData); err != nil {
		t.Fatalf("seed Item: %v", err)
	}

	boxes, err := SuggestCartonizationV2(tenantID, cartonType, []CartonizationItemV2{{Sku: sku, Qty: 10}})
	if err != nil {
		t.Fatalf("SuggestCartonizationV2: %v", err)
	}
	// 10 units at 1kg each, 3kg ceiling per box -> at least 4 boxes (3+3+3+1),
	// where SuggestCartonization (qty-only, capacity 100) would have made just 1.
	if len(boxes) < 4 {
		t.Errorf("expected the weight ceiling to force at least 4 boxes, got %d", len(boxes))
	}
	for _, b := range boxes {
		if b.UsedCapacity > 3 {
			t.Errorf("box %s holds %d units, over the 3kg-equivalent weight ceiling", b.BoxID, b.UsedCapacity)
		}
	}
}

func TestDeconsolidateLPN(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sourceLPN, destA, destB = "LPN-DECON-SRC", "LPN-DECON-A", "LPN-DECON-B"
	const binCode, sku = "BIN-DECON-TEST", "SKU-DECON-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock_lpn WHERE lpn_code IN ('" + sourceLPN + "', '" + destA + "', '" + destB + "')")
	}
	cleanup()
	defer cleanup()

	if _, err := db.DB.Exec(
		"INSERT INTO "+schema+".bin_stock_lpn (lpn_code, bin_code, sku, condition, qty) VALUES ($1, $2, $3, 'Good', 10)",
		sourceLPN, binCode, sku); err != nil {
		t.Fatalf("seed bin_stock_lpn: %v", err)
	}

	// Splits exceeding the source's assigned qty are refused.
	if err := DeconsolidateLPN(tenantID, sourceLPN, binCode, sku, "Good",
		[]DeconsolidationSplit{{DestLPN: destA, Qty: 11}}, "system"); err == nil {
		t.Error("expected an over-total split to be refused")
	}

	if err := DeconsolidateLPN(tenantID, sourceLPN, binCode, sku, "Good",
		[]DeconsolidationSplit{{DestLPN: destA, Qty: 6}, {DestLPN: destB, Qty: 4}}, "system"); err != nil {
		t.Fatalf("DeconsolidateLPN: %v", err)
	}

	var srcQty, aQty, bQty int
	_ = db.DB.QueryRow("SELECT COALESCE(qty,0) FROM "+schema+".bin_stock_lpn WHERE lpn_code = $1 AND bin_code = $2 AND sku = $3 AND condition = 'Good'", sourceLPN, binCode, sku).Scan(&srcQty)
	_ = db.DB.QueryRow("SELECT COALESCE(qty,0) FROM "+schema+".bin_stock_lpn WHERE lpn_code = $1 AND bin_code = $2 AND sku = $3 AND condition = 'Good'", destA, binCode, sku).Scan(&aQty)
	_ = db.DB.QueryRow("SELECT COALESCE(qty,0) FROM "+schema+".bin_stock_lpn WHERE lpn_code = $1 AND bin_code = $2 AND sku = $3 AND condition = 'Good'", destB, binCode, sku).Scan(&bQty)
	if srcQty != 0 || aQty != 6 || bQty != 4 {
		t.Errorf("expected source=0/a=6/b=4, got source=%d/a=%d/b=%d", srcQty, aQty, bQty)
	}
}

func TestCreateVASTaskRefusesBOMMismatch(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const bomID = "BOM-VAS-TEST-1"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + bomID + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'bom_id' = '" + bomID + "'")
	}
	cleanup()
	defer cleanup()

	components, _ := json.Marshal([]bomComponent{{Sku: "COMPONENT-VAS-TEST", Qty: 2}})
	bomData, _ := json.Marshal(map[string]interface{}{"parent_item": "KIT-VAS-TEST", "components": string(components)})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'BOM', $2, 'Active', 'system')", bomID, bomData); err != nil {
		t.Fatalf("seed BOM: %v", err)
	}

	if _, err := CreateVASTask(tenantID, "WH01", "BIN-A", "BIN-B", "WRONG-ITEM", 1, bomID, "", "system"); err == nil {
		t.Error("expected CreateVASTask to refuse an output item that doesn't match the BOM's parent_item")
	}
	taskID, err := CreateVASTask(tenantID, "WH01", "BIN-A", "BIN-B", "KIT-VAS-TEST", 1, bomID, "", "system")
	if err != nil {
		t.Fatalf("expected a matching output item to be accepted: %v", err)
	}
	task, err := GetWarehouseTask(tenantID, taskID)
	if err != nil || task == nil || task.TaskType != "VAS" {
		t.Fatalf("expected a VAS WarehouseTask to be created, got %+v, err %v", task, err)
	}
}

func TestEvaluatePreShipGate(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const ruleID = "PRESHIP-TEST-1"
	const loadingTaskID = "LOAD-PRESHIP-TEST-1"
	const door = "DOOR-PRESHIP-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id IN ('" + ruleID + "', '" + loadingTaskID + "')")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'DockDoor' AND data->>'code' = '" + door + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Hold' AND data->>'sku' = 'SKU-PRESHIP-TEST'")
	}
	cleanup()
	defer cleanup()

	task := &LoadingTaskInfo{DocID: loadingTaskID, DockDoor: door, ScannedCartonCount: 1, ExpectedCartonCount: 1}
	// No Active rule at all: inert.
	if err := EvaluatePreShipGate(tenantID, task); err != nil {
		t.Errorf("expected no rule to be a no-op, got %v", err)
	}

	doorData, _ := json.Marshal(map[string]interface{}{"code": door, "location": "WH-PRESHIP", "door_type": "Outbound", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'DockDoor', $2, 'Active', 'system')", "DOORDOC-PRESHIP", doorData); err != nil {
		t.Fatalf("seed DockDoor: %v", err)
	}
	ruleData, _ := json.Marshal(map[string]interface{}{
		"code": ruleID, "location_code": "WH-PRESHIP", "require_all_cartons_scanned": "No",
		"require_hold_free": "Yes", "require_documents_present": "No", "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'PreShipValidationRule', $2, 'Active', 'system')", ruleID, ruleData); err != nil {
		t.Fatalf("seed PreShipValidationRule: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'LoadingTask', $2, 'Loading', 'system')",
		loadingTaskID, mustJSON(map[string]interface{}{"code": loadingTaskID, "dock_door": door, "trailer_no": "TRL-1", "status": "Loading", "scanned_carton_count": 0})); err != nil {
		t.Fatalf("seed LoadingTask: %v", err)
	}

	// No hold, no packages: passes (require_hold_free with zero SKUs is a no-op).
	if err := EvaluatePreShipGate(tenantID, task); err != nil {
		t.Errorf("expected the gate to pass with no packages/holds, got %v", err)
	}

	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ShippingPackage', $2, 'Invoiced', 'system')",
		"SP-PRESHIP-TEST", mustJSON(map[string]interface{}{
			"code": "SP-PRESHIP-TEST", "loading_task_id": loadingTaskID, "sales_invoice_id": "INV-1",
			"items": []map[string]interface{}{{"sku": "SKU-PRESHIP-TEST", "qty": 1}},
		})); err != nil {
		t.Fatalf("seed ShippingPackage: %v", err)
	}
	defer func() { _, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = 'SP-PRESHIP-TEST'") }()

	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Hold', $2, 'Active', 'system')",
		"HOLD-PRESHIP-TEST", mustJSON(map[string]interface{}{"hold_code": "QC", "sku": "SKU-PRESHIP-TEST", "location_code": "WH-PRESHIP", "qty": 5, "status": "Active"})); err != nil {
		t.Fatalf("seed Hold: %v", err)
	}
	if err := EvaluatePreShipGate(tenantID, task); err == nil {
		t.Error("expected an Active hold on a loaded SKU to block the pre-ship gate")
	}
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
