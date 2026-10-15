package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestStage59_1BinAllocation locks down Stage 59.1: a bin is promised to one
// task at a time, and picking or dispatching takes the units off the bin they
// were promised from - before it, bin_stock never went down at all.
func TestStage59_1BinAllocation(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		sku      = "SKU-BA59-TEST"
		bin1     = "BIN-BA59-1"
		bin2     = "BIN-BA59-2"
		location = "WH-BA59"
	)
	cleanup := func() {
		for _, q := range []string{
			"DELETE FROM " + schema + ".bin_allocation WHERE sku = $1",
			"DELETE FROM " + schema + ".bin_stock WHERE sku = $1",
			"DELETE FROM " + schema + ".inventory_availability WHERE sku = $1",
			"DELETE FROM " + schema + ".documents WHERE doctype = 'FulfillmentTask' AND data->>'items' LIKE '%' || $1 || '%'",
			"DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND data->>'code' = $1",
			"DELETE FROM " + schema + ".documents WHERE doctype = 'Bin' AND data->>'bin_code' LIKE 'BIN-BA59-%' AND $1 <> ''",
		} {
			_, _ = db.DB.Exec(q, sku)
		}
	}
	cleanup()
	defer cleanup()

	itemData, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Bin allocation test item"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')",
		"ITEM-BA59-TEST", itemData); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	seedBinWithStock(t, schema, bin1, sku, location, 5)
	seedBinWithStock(t, schema, bin2, sku, location, 3)

	binQty := func(bin string) int {
		var q int
		_ = db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'", bin, sku).Scan(&q)
		return q
	}
	reservedFor := func(taskID string) int {
		var q int
		_ = db.DB.QueryRow("SELECT COALESCE(SUM(qty - picked_qty), 0) FROM "+schema+".bin_allocation WHERE task_id = $1 AND status = 'Open'", taskID).Scan(&q)
		return q
	}
	newTask := func(qty int) string {
		id, err := CreateFulfillmentTasks(tenantID, "ORD-BA59", location, []interface{}{
			map[string]interface{}{"sku": sku, "qty": qty},
		})
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		return id
	}
	sumPick := func(lines []PickListLine) (pick, short int) {
		for _, l := range lines {
			pick += l.PickQty
			short += l.Shortfall
		}
		return
	}

	// 8 units in bins. Task A wants 6, task B wants 4: B must get only the
	// 2 A has not been promised, and see a shortfall of 2 - before 59.1 both
	// pick lists named the same units.
	taskA := newTask(6)
	taskB := newTask(4)

	t.Run("BinIsPromisedOnce", func(t *testing.T) {
		linesA, err := GenerateBinPickList(tenantID, taskA)
		if err != nil {
			t.Fatalf("pick list A: %v", err)
		}
		if p, s := sumPick(linesA); p != 6 || s != 0 {
			t.Fatalf("task A: expected pick 6 / short 0, got %d / %d (%+v)", p, s, linesA)
		}
		linesB, err := GenerateBinPickList(tenantID, taskB)
		if err != nil {
			t.Fatalf("pick list B: %v", err)
		}
		if p, s := sumPick(linesB); p != 2 || s != 2 {
			t.Fatalf("task B: expected pick 2 / short 2 (A holds 6 of 8), got %d / %d (%+v)", p, s, linesB)
		}
	})

	t.Run("PickListIsStable", func(t *testing.T) {
		again, err := GenerateBinPickList(tenantID, taskA)
		if err != nil {
			t.Fatalf("pick list A again: %v", err)
		}
		if p, _ := sumPick(again); p != 6 || reservedFor(taskA) != 6 {
			t.Fatalf("a second call must show the same reservation, not add one: pick %d, reserved %d", p, reservedFor(taskA))
		}
	})

	t.Run("ScanTakesUnitOffItsBin", func(t *testing.T) {
		before := binQty(bin1) + binQty(bin2)
		if _, _, err := ScanPickItem(tenantID, taskA, sku); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if after := binQty(bin1) + binQty(bin2); after != before-1 {
			t.Fatalf("a pick scan must take one unit off the bins: before %d, after %d", before, after)
		}
		if reservedFor(taskA) != 5 {
			t.Fatalf("expected 5 still reserved for A after one scan, got %d", reservedFor(taskA))
		}
	})

	t.Run("DispatchTakesUnscannedUnits", func(t *testing.T) {
		// A has 1 scanned, 5 unscanned; dispatch ships all 6.
		if err := TransitionTaskStatus(tenantID, taskA, "Dispatched"); err != nil {
			t.Fatalf("dispatch A: %v", err)
		}
		if got := binQty(bin1) + binQty(bin2); got != 2 {
			t.Fatalf("8 binned - 6 shipped should leave 2 in the bins, got %d", got)
		}
		if reservedFor(taskA) != 0 {
			t.Fatalf("a dispatched task must hold no reservation, got %d", reservedFor(taskA))
		}
	})

	t.Run("ShortPickReleasesForOthers", func(t *testing.T) {
		// B holds the last 2. Short-picking B must hand them back so a new
		// task can be promised them.
		if err := ShortPickLine(tenantID, taskB, sku, ensureShortPickReason(t, schema)); err != nil {
			t.Fatalf("short pick B: %v", err)
		}
		if reservedFor(taskB) != 0 {
			t.Fatalf("short-picked SKU must release its reservation, %d still held", reservedFor(taskB))
		}
		taskC := newTask(2)
		lines, err := GenerateBinPickList(tenantID, taskC)
		if err != nil {
			t.Fatalf("pick list C: %v", err)
		}
		if p, s := sumPick(lines); p != 2 || s != 0 {
			t.Fatalf("task C should get the 2 B released, got pick %d / short %d", p, s)
		}
		if err := TransitionTaskStatus(tenantID, taskC, "Rejected"); err != nil {
			t.Fatalf("reject C: %v", err)
		}
		if reservedFor(taskC) != 0 {
			t.Fatalf("a rejected task must hold no reservation, got %d", reservedFor(taskC))
		}
	})

	t.Run("ReconciliationReportsDrift", func(t *testing.T) {
		// Simulate pre-59.1 history: a bin that kept shipped units.
		if _, err := db.DB.Exec("UPDATE "+schema+".bin_stock SET qty = qty + 4 WHERE bin_code = $1 AND sku = $2", bin1, sku); err != nil {
			t.Fatalf("inflate bin: %v", err)
		}
		rows, err := BinStockReconciliation(tenantID, sku, location, true)
		if err != nil {
			t.Fatalf("reconciliation: %v", err)
		}
		if len(rows) != 1 || rows[0].Unbinned != -4 || rows[0].Status != "Bins over-state stock" {
			t.Fatalf("expected one row with the bins 4 over on_hand, got %+v", rows)
		}
	})
}

// TestStage59_1BinlessLocationUnaffected: a location that does not use bins
// reserves nothing and dispatches exactly as before.
func TestStage59_1BinlessLocationUnaffected(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const sku, location = "SKU-BA59-NOBIN", "WH-BA59-NOBIN"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_allocation WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND data->>'items' LIKE '%' || $1 || '%'", sku)
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 5, 5)", sku, location); err != nil {
		t.Fatalf("seed availability: %v", err)
	}
	taskID, err := CreateFulfillmentTasks(tenantID, "ORD-BA59-NOBIN", location, []interface{}{
		map[string]interface{}{"sku": sku, "qty": 3},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	lines, err := GenerateBinPickList(tenantID, taskID)
	if err != nil {
		t.Fatalf("pick list: %v", err)
	}
	if len(lines) != 1 || lines[0].Shortfall != 3 {
		t.Fatalf("no bins: expected one shortfall line of 3, got %+v", lines)
	}
	if err := TransitionTaskStatus(tenantID, taskID, "Dispatched"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var onHand, rows int
	_ = db.DB.QueryRow("SELECT on_hand FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&onHand)
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".bin_allocation WHERE task_id = $1", taskID).Scan(&rows)
	if onHand != 2 || rows != 0 {
		t.Fatalf("expected on_hand 5-3=2 and no reservations, got on_hand %d, %d reservation rows", onHand, rows)
	}
}

// ensureShortPickReason returns an Active "Short Pick" reason code, creating
// one only if the tenant has none.
func ensureShortPickReason(t *testing.T, schema string) string {
	t.Helper()
	var code string
	err := db.DB.QueryRow("SELECT data->>'code' FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND status = 'Active' AND data->>'category' = 'Short Pick' LIMIT 1").Scan(&code)
	if err == nil && code != "" {
		return code
	}
	code = "RC-BA59-SHORT"
	data, _ := json.Marshal(map[string]interface{}{"code": code, "name": "Not found in bin", "category": "Short Pick", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system') ON CONFLICT DO NOTHING", code, data); err != nil {
		t.Fatalf("seed reason code: %v", err)
	}
	t.Cleanup(func() { _, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", code) })
	return code
}

// TestStage59_1ConcurrentReservationNeverOverPromises: ten tasks racing for
// five binned units must be promised exactly five between them - the
// per-SKU advisory lock is what stops two of them reading the same free unit.
func TestStage59_1ConcurrentReservationNeverOverPromises(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const sku, bin, location = "SKU-BA59-RACE", "BIN-BA59-RACE", "WH-BA59-RACE"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_allocation WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND data->>'items' LIKE '%' || $1 || '%'", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Bin' AND data->>'bin_code' = $1", bin)
	}
	cleanup()
	defer cleanup()
	seedBinWithStock(t, schema, bin, sku, location, 5)

	var tasks []string
	for i := 0; i < 10; i++ {
		id, err := CreateFulfillmentTasks(tenantID, "ORD-BA59-RACE", location, []interface{}{
			map[string]interface{}{"sku": sku, "qty": 1},
		})
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		tasks = append(tasks, id)
	}
	errs := make(chan error, len(tasks))
	for _, id := range tasks {
		go func(id string) { errs <- ReserveTaskBinStock(tenantID, id) }(id)
	}
	for range tasks {
		if err := <-errs; err != nil {
			t.Fatalf("reserve: %v", err)
		}
	}
	var promised int
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(qty), 0) FROM "+schema+".bin_allocation WHERE sku = $1 AND status = 'Open'", sku).Scan(&promised)
	if promised != 5 {
		t.Fatalf("5 units in the bin, 10 one-unit tasks racing: expected exactly 5 promised, got %d", promised)
	}
}

// TestStage59_1MoveRespectsReservation: a shelf move or a condition change
// cannot take stock a pick task has been sent to; the unreserved rest moves.
func TestStage59_1MoveRespectsReservation(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const sku, from, to, location = "SKU-BA59-MOVE", "BIN-BA59-MVFROM", "BIN-BA59-MVTO", "WH-BA59-MOVE"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_allocation WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND data->>'items' LIKE '%' || $1 || '%'", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Bin' AND data->>'bin_code' IN ($1, $2)", from, to)
	}
	cleanup()
	defer cleanup()
	seedBinWithStock(t, schema, from, sku, location, 3)
	seedBinWithStock(t, schema, to, sku, location, 1)

	taskID, err := CreateFulfillmentTasks(tenantID, "ORD-BA59-MOVE", location, []interface{}{
		map[string]interface{}{"sku": sku, "qty": 4},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := ReserveTaskBinStock(tenantID, taskID); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	// The task holds all 4 (3 in from, 1 in to): nothing in `from` is free.
	if err := MoveBinStock(tenantID, BinMoveInput{FromBin: from, ToBin: to, SKU: sku, Qty: 1, UserID: "system"}); err == nil {
		t.Fatalf("moving reserved stock must be refused")
	}
	if err := TransitionBinStockCondition(tenantID, from, sku, 1, "Good", "Damaged", "system"); err == nil {
		t.Fatalf("marking reserved stock Damaged must be refused")
	}
	// Short-pick hands it back; now the move goes through.
	if err := ShortPickLine(tenantID, taskID, sku, ensureShortPickReason(t, schema)); err != nil {
		t.Fatalf("short pick: %v", err)
	}
	if err := MoveBinStock(tenantID, BinMoveInput{FromBin: from, ToBin: to, SKU: sku, Qty: 2, UserID: "system"}); err != nil {
		t.Fatalf("move of released stock: %v", err)
	}
}
