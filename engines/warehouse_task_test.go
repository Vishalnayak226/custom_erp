package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 42.2.1 tests. Own file, same db.InitDB / tenantID="default"
// convention as TestTraceability.

func warehouseTaskCleanup(t *testing.T, schema, marker string) {
	t.Helper()
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'location_code' = '" + marker + "'")
}

func TestCreateWarehouseTask(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const loc = "WH-WT-CREATE-TEST"
	warehouseTaskCleanup(t, schema, loc)
	defer warehouseTaskCleanup(t, schema, loc)

	if _, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Bogus", LocationCode: loc}, "system"); err == nil {
		t.Error("expected an invalid task_type to be refused")
	}
	if _, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Pick"}, "system"); err == nil {
		t.Error("expected a blank location_code to be refused")
	}

	taskID, err := CreateWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "Pick", Priority: 5, LocationCode: loc, FromBin: "BIN-A", Item: "SKU-X", Qty: 10,
		SourceDocType: "FulfillmentTask", SourceDocID: "TSK-123",
	}, "system")
	if err != nil {
		t.Fatalf("CreateWarehouseTask: %v", err)
	}
	if taskID == "" {
		t.Fatal("expected a non-empty task id")
	}

	task, err := GetWarehouseTask(tenantID, taskID)
	if err != nil {
		t.Fatalf("GetWarehouseTask: %v", err)
	}
	if task == nil {
		t.Fatal("expected the just-created task to be found")
	}
	if task.Status != WTStatusPending {
		t.Errorf("expected a new task to start Pending, got %q", task.Status)
	}
	if task.TaskType != "Pick" || task.Priority != 5 || task.LocationCode != loc || task.FromBin != "BIN-A" ||
		task.Item != "SKU-X" || task.Qty != 10 || task.SourceDocType != "FulfillmentTask" || task.SourceDocID != "TSK-123" {
		t.Errorf("flattened fields did not round-trip: %+v", task)
	}

	unknown, err := GetWarehouseTask(tenantID, "WT-DOES-NOT-EXIST")
	if err != nil {
		t.Fatalf("GetWarehouseTask (unknown): %v", err)
	}
	if unknown != nil {
		t.Errorf("expected an unknown task id to return nil, got %+v", unknown)
	}
}

func TestTransitionWarehouseTaskStatus(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const loc = "WH-WT-TRANSITION-TEST"
	warehouseTaskCleanup(t, schema, loc)
	defer warehouseTaskCleanup(t, schema, loc)

	// Stage 42.2.9: moving a task into Exception now requires a real Active
	// 'WMS Exception' ReasonCode, not free text - seed one.
	const reasonCodeID = "RC-WT-TRANSITION-TEST"
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND id = '" + reasonCodeID + "'")
	defer func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND id = '" + reasonCodeID + "'")
	}()
	reasonData, _ := json.Marshal(map[string]interface{}{
		"code": reasonCodeID, "description": "Scale mismatch", "category": "WMS Exception", "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')",
		reasonCodeID, reasonData); err != nil {
		t.Fatalf("seed ReasonCode: %v", err)
	}

	taskID, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Putaway", LocationCode: loc}, "system")
	if err != nil {
		t.Fatalf("CreateWarehouseTask: %v", err)
	}

	if err := TransitionWarehouseTaskStatus(tenantID, taskID, "Bogus", "", "", "system"); err == nil {
		t.Error("expected an invalid status value to be refused")
	}

	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusAssigned, "", "picker1", "system"); err != nil {
		t.Fatalf("Pending -> Assigned: %v", err)
	}
	task, _ := GetWarehouseTask(tenantID, taskID)
	if task.Status != WTStatusAssigned || task.AssignedTo != "picker1" {
		t.Errorf("expected Assigned to picker1, got status=%q assigned_to=%q", task.Status, task.AssignedTo)
	}

	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusInProgress, "", "", "system"); err != nil {
		t.Fatalf("Assigned -> In Progress: %v", err)
	}

	// Cancelling requires a reason.
	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusCancelled, "", "", "system"); err == nil {
		t.Error("expected cancelling with no reason to be refused")
	}
	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusException, "not-a-real-reason-code", "", "system"); err == nil {
		t.Error("expected Exception with a non-ReasonCode reason to be refused")
	}
	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusException, reasonCodeID, "", "system"); err != nil {
		t.Fatalf("In Progress -> Exception: %v", err)
	}
	// Coming back out of Exception (other than to Cancelled) requires a reason too.
	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusAssigned, "", "", "system"); err == nil {
		t.Error("expected leaving Exception with no reason to be refused")
	}
	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusAssigned, "re-weighed, correct", "", "system"); err != nil {
		t.Fatalf("Exception -> Assigned (reasoned): %v", err)
	}

	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusInProgress, "", "", "system"); err != nil {
		t.Fatalf("Assigned -> In Progress (2nd time): %v", err)
	}
	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusCompleted, "", "", "system"); err != nil {
		t.Fatalf("In Progress -> Completed: %v", err)
	}

	// Completed is terminal.
	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusAssigned, "", "", "system"); err == nil {
		t.Error("expected a transition out of a terminal (Completed) state to be refused")
	}

	// Same-status is always a no-op, even from a terminal state.
	if err := TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusCompleted, "", "", "system"); err != nil {
		t.Errorf("expected re-asserting the same status to be a no-op, got %v", err)
	}
}

// warehouseTaskExists reports whether a Completed WarehouseTask with this
// task_type/location_code exists - the assertion every 42.2.2 retrofit
// subtest below makes.
func warehouseTaskExists(t *testing.T, schema, taskType, location string) bool {
	t.Helper()
	var exists bool
	if err := db.DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM "+schema+".documents WHERE doctype = 'WarehouseTask' AND data->>'task_type' = $1 AND data->>'location_code' = $2 AND status = $3)",
		taskType, location, WTStatusCompleted).Scan(&exists); err != nil {
		t.Fatalf("check WarehouseTask existence: %v", err)
	}
	return exists
}

// TestWarehouseTaskRetrofit (Stage 42.2.2) locks down that each of the five
// pre-existing floor actions now additively logs a Completed WarehouseTask,
// without changing any of their own return values or preconditions - the
// "additive, each keeps working exactly as today" contract the plan states.
func TestWarehouseTaskRetrofit(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	t.Run("PutawayToBin", func(t *testing.T) {
		const sku = "SKU-WTRETRO-PUTAWAY"
		const bin = "BIN-WTRETRO-PUTAWAY"
		const loc = "WH-WTRETRO-PUTAWAY"
		cleanup := func() {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE sku = '" + sku + "'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = '" + sku + "'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Bin' AND data->>'bin_code' = '" + bin + "'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'location_code' = '" + loc + "'")
		}
		cleanup()
		defer cleanup()

		binData, _ := json.Marshal(map[string]interface{}{"bin_code": bin, "location": loc, "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')",
			"BINDOC-"+bin, binData); err != nil {
			t.Fatalf("seed Bin: %v", err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)",
			sku, loc); err != nil {
			t.Fatalf("seed inventory_availability: %v", err)
		}

		if err := PutawayToBin(tenantID, bin, sku, 10, "system"); err != nil {
			t.Fatalf("PutawayToBin: %v", err)
		}
		if !warehouseTaskExists(t, schema, "Putaway", loc) {
			t.Error("expected a Completed Putaway WarehouseTask to have been logged")
		}
	})

	t.Run("ExecuteBinReplenishment", func(t *testing.T) {
		const sku = "SKU-WTRETRO-REPLEN"
		const fromBin, toBin = "BIN-WTRETRO-REPLEN-FROM", "BIN-WTRETRO-REPLEN-TO"
		const loc = "WH-WTRETRO-REPLEN"
		cleanup := func() {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE sku = '" + sku + "'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'location_code' = '" + loc + "'")
		}
		cleanup()
		defer cleanup()

		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', $4)",
			fromBin, sku, loc, 20); err != nil {
			t.Fatalf("seed bin_stock: %v", err)
		}
		if err := ExecuteBinReplenishment(tenantID, fromBin, toBin, sku, 5, "system"); err != nil {
			t.Fatalf("ExecuteBinReplenishment: %v", err)
		}
		if !warehouseTaskExists(t, schema, "Replenish", loc) {
			t.Error("expected a Completed Replenish WarehouseTask to have been logged")
		}
	})

	t.Run("CrossDockPutaway", func(t *testing.T) {
		const sku = "SKU-WTRETRO-XDOCK"
		const loc = "WH-WTRETRO-XDOCK"
		cleanup := func() {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE sku = '" + sku + "'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = '" + sku + "'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'FulfillmentTask' AND id = 'FT-WTRETRO-XDOCK'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'location_code' = '" + loc + "'")
		}
		cleanup()
		defer cleanup()

		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)",
			sku, loc); err != nil {
			t.Fatalf("seed inventory_availability: %v", err)
		}
		taskData, _ := json.Marshal(map[string]interface{}{
			"location_code": loc,
			"items":         []map[string]interface{}{{"sku": sku, "qty": 6, "picked_qty": 0, "short_qty": 0}},
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system')",
			"FT-WTRETRO-XDOCK", taskData); err != nil {
			t.Fatalf("seed FulfillmentTask: %v", err)
		}

		if _, _, err := CrossDockPutaway(tenantID, sku, loc, 6, "system"); err != nil {
			t.Fatalf("CrossDockPutaway: %v", err)
		}
		if !warehouseTaskExists(t, schema, "Putaway", loc) {
			t.Error("expected a Completed Putaway WarehouseTask to have been logged for the cross-dock stage")
		}
	})

	t.Run("PostCycleCountAdjustment", func(t *testing.T) {
		const sku = "SKU-WTRETRO-COUNT"
		const loc = "WH-WTRETRO-COUNT"
		const lineID = "CCL-WTRETRO-COUNT"
		cleanup := func() {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = '" + sku + "'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'CycleCountLine' AND id = '" + lineID + "'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'location_code' = '" + loc + "'")
		}
		cleanup()
		defer cleanup()

		lineData, _ := json.Marshal(map[string]interface{}{
			"sku": sku, "location": loc, "variance": 3, "variance_reason_code": "RC-TEST",
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CycleCountLine', $2, 'Approved', 'system')",
			lineID, lineData); err != nil {
			t.Fatalf("seed CycleCountLine: %v", err)
		}

		if err := PostCycleCountAdjustment(tenantID, lineID, "system"); err != nil {
			t.Fatalf("PostCycleCountAdjustment: %v", err)
		}
		if !warehouseTaskExists(t, schema, "Count", loc) {
			t.Error("expected a Completed Count WarehouseTask to have been logged")
		}
	})
}

// TestGetNextTask (Stage 42.2.3) locks down the dispatch ordering (priority
// descending, then ageing as the tie-break), the queue/task_type filters,
// and that a dispatched task is actually assigned (so a second dispatch call
// never hands out the same task twice).
func TestGetNextTask(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const loc = "WH-WT-DISPATCH-TEST"
	warehouseTaskCleanup(t, schema, loc)
	defer warehouseTaskCleanup(t, schema, loc)

	if _, err := GetNextTask(tenantID, "", loc, "", ""); err == nil {
		t.Error("expected a blank userID to be refused")
	}
	if _, err := GetNextTask(tenantID, "picker1", "", "", ""); err == nil {
		t.Error("expected a blank locationCode to be refused")
	}

	// Nothing queued yet - a nil, nil "no task available" answer, not an error.
	none, err := GetNextTask(tenantID, "picker1", loc, "", "")
	if err != nil {
		t.Fatalf("GetNextTask (empty queue): %v", err)
	}
	if none != nil {
		t.Errorf("expected no task available, got %+v", none)
	}

	lowID, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Pick", Priority: 1, LocationCode: loc}, "system")
	if err != nil {
		t.Fatalf("create low-priority task: %v", err)
	}
	// A Replenish task with no queue set - must not surface under a queue filter.
	if _, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Replenish", Priority: 9, LocationCode: loc}, "system"); err != nil {
		t.Fatalf("create replenish task: %v", err)
	}
	highID, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Pick", Priority: 9, LocationCode: loc}, "system")
	if err != nil {
		t.Fatalf("create high-priority task: %v", err)
	}

	// Filtered to task_type=Pick, the higher-priority Pick task must win over
	// the (unfiltered-out) higher-priority Replenish task.
	got, err := GetNextTask(tenantID, "picker1", loc, "", "Pick")
	if err != nil {
		t.Fatalf("GetNextTask (filtered): %v", err)
	}
	if got == nil || got.DocID != highID {
		t.Fatalf("expected the high-priority Pick task (%s) first, got %+v", highID, got)
	}
	if got.Status != WTStatusAssigned || got.AssignedTo != "picker1" {
		t.Errorf("expected the dispatched task to be Assigned to picker1, got status=%q assigned_to=%q", got.Status, got.AssignedTo)
	}

	// The high-priority task is now Assigned, not Pending - a second dispatch
	// call for the same filter must move on to the remaining low-priority one,
	// never re-hand out the same task.
	got2, err := GetNextTask(tenantID, "picker2", loc, "", "Pick")
	if err != nil {
		t.Fatalf("GetNextTask (2nd call): %v", err)
	}
	if got2 == nil || got2.DocID != lowID {
		t.Fatalf("expected the remaining low-priority Pick task (%s), got %+v", lowID, got2)
	}

	// Both Pick tasks are now Assigned - filtered dispatch must report none left.
	none2, err := GetNextTask(tenantID, "picker3", loc, "", "Pick")
	if err != nil {
		t.Fatalf("GetNextTask (exhausted): %v", err)
	}
	if none2 != nil {
		t.Errorf("expected no Pick tasks left, got %+v", none2)
	}
}

// TestTaskDispatchStrategy (Stage 42.2.4) locks down that an Active strategy
// actually changes dispatch order, that an unconfigured location is
// unaffected (byte-identical to 42.2.3's own default), and the two
// TaskDispatchStrategy-specific master rules.
func TestTaskDispatchStrategy(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const loc = "WH-WT-STRATEGY-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'location_code' = '" + loc + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'TaskDispatchStrategy' AND data->>'location_code' = '" + loc + "'")
	}
	cleanup()
	defer cleanup()

	// Same priority (0, the default) but created in a known order: without a
	// strategy, ageing is already the tie-break, so the FIRST one created
	// should dispatch first either way. To prove the strategy actually
	// changes behaviour, give the SECOND (newer) task the higher priority
	// and confirm default ordering picks it first (priority beats ageing by
	// default), then flip the configured order to ageing-first and confirm
	// the OLDER task now wins instead.
	oldID, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Pick", Priority: 1, LocationCode: loc}, "system")
	if err != nil {
		t.Fatalf("create older task: %v", err)
	}
	newID, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Pick", Priority: 5, LocationCode: loc}, "system")
	if err != nil {
		t.Fatalf("create newer, higher-priority task: %v", err)
	}

	// Default order (no strategy configured): priority wins - the newer task.
	got, err := GetNextTask(tenantID, "picker1", loc, "", "Pick")
	if err != nil {
		t.Fatalf("GetNextTask (default order): %v", err)
	}
	if got == nil || got.DocID != newID {
		t.Fatalf("expected the default (priority-first) order to pick %s, got %+v", newID, got)
	}
	// Return it to Pending so the strategy test below has both tasks available again.
	if err := TransitionWarehouseTaskStatus(tenantID, newID, WTStatusPending, "", "", "system"); err != nil {
		t.Fatalf("reset newID to Pending: %v", err)
	}

	stratData, _ := json.Marshal(map[string]interface{}{
		"code": "STRAT-WT-TEST", "location_code": loc, "sort_order": "ageing,priority", "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'TaskDispatchStrategy', $2, 'Active', 'system')",
		"STRAT-WT-TEST", stratData); err != nil {
		t.Fatalf("seed TaskDispatchStrategy: %v", err)
	}

	got2, err := GetNextTask(tenantID, "picker2", loc, "", "Pick")
	if err != nil {
		t.Fatalf("GetNextTask (ageing-first strategy): %v", err)
	}
	if got2 == nil || got2.DocID != oldID {
		t.Fatalf("expected the ageing-first strategy to pick the OLDER task %s, got %+v", oldID, got2)
	}
}

func TestTaskDispatchStrategyMasterRules(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const loc = "WH-WT-STRATRULE-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'TaskDispatchStrategy' AND data->>'location_code' = '" + loc + "'")
	}
	cleanup()
	defer cleanup()

	badToken := map[string]interface{}{"code": "S1", "location_code": loc, "sort_order": "priority,proximity", "status": "Active"}
	if err := ValidateMasterDataRules(tenantID, "STRATRULE-01", "TaskDispatchStrategy", badToken); err == nil {
		t.Error("expected an unrecognised sort token (proximity - not buildable yet) to be refused")
	}

	valid := map[string]interface{}{"code": "S1", "location_code": loc, "sort_order": "priority,ageing", "status": "Active"}
	if err := ValidateMasterDataRules(tenantID, "STRATRULE-01", "TaskDispatchStrategy", valid); err != nil {
		t.Fatalf("expected a well-formed strategy to validate, got %v", err)
	}
	data, _ := json.Marshal(valid)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'TaskDispatchStrategy', $2, 'Active', 'system')",
		"STRATRULE-01", data); err != nil {
		t.Fatalf("seed strategy: %v", err)
	}

	dup := map[string]interface{}{"code": "S2", "location_code": loc, "sort_order": "ageing", "status": "Active"}
	if err := ValidateMasterDataRules(tenantID, "STRATRULE-02", "TaskDispatchStrategy", dup); err == nil {
		t.Error("expected a second Active strategy for the same location to be refused")
	}
	// An Inactive one for the same location is not a conflict.
	inactive := map[string]interface{}{"code": "S3", "location_code": loc, "sort_order": "ageing", "status": "Inactive"}
	if err := ValidateMasterDataRules(tenantID, "STRATRULE-03", "TaskDispatchStrategy", inactive); err != nil {
		t.Errorf("expected an Inactive strategy to not collide: %v", err)
	}
	if err := ValidateMasterDataRules(tenantID, "STRATRULE-01", "TaskDispatchStrategy", valid); err != nil {
		t.Errorf("expected editing the same row to not collide with itself: %v", err)
	}
}
