package engines

import (
	"testing"

	"custom_erp/db"
)

// Stage 146.3.3 (the Left part): replenish, move and count tasks are created
// where the work is born - a pick-face bin below its min, a move request, a
// count session - only when wms.work_queue is on, once each, and they close
// or cancel with their source.

func wqeSeed(t *testing.T, schema string, insert func(id, doctype, status, createdBy string, data map[string]interface{}), loc string) (sku string) {
	t.Helper()
	sku = loc + "-SKU"
	insert("ITEM-"+sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Q"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 40, 40)", sku, loc); err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"PF", "RES"} {
		insert("BIN-"+loc+b, "Bin", "Active", "system", map[string]interface{}{"bin_code": loc + b, "location": loc, "status": "Active"})
	}
	return
}

func wqeStock(t *testing.T, schema, bin, sku, loc string, qty int) {
	t.Helper()
	if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', $4)", bin, sku, loc, qty); err != nil {
		t.Fatal(err)
	}
}

func wqeOpen(schema, taskType, item string) (n int) {
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'WarehouseTask' AND data->>'task_type' = $1 AND data->>'item' = $2 AND status IN ('Pending','Assigned','In Progress')", taskType, item).Scan(&n)
	return
}

func TestStage14633LeftReplenishTask(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc = "WQE1"
	sku := wqeSeed(t, schema, insert, loc)
	insert("RULE-WQE1", "BinReplenishmentRule", "Active", "system", map[string]interface{}{"bin_code": loc + "PF", "sku": sku, "min_qty": 5, "max_qty": 20, "status": "Active"})
	wqeStock(t, schema, loc+"PF", sku, loc, 2)
	wqeStock(t, schema, loc+"RES", sku, loc, 38)

	EmitReplenishTasks(tenantID, loc)
	if n := wqeOpen(schema, "Replenish", sku); n != 0 {
		t.Fatalf("the queue is off but %d replenish task(s) were made", n)
	}
	if err := SetSetting(tenantID, "wms.work_queue", "true", "test"); err != nil {
		t.Fatal(err)
	}
	EmitReplenishTasks(tenantID, loc)
	EmitReplenishTasks(tenantID, loc) // idempotent
	if n := wqeOpen(schema, "Replenish", sku); n != 1 {
		t.Fatalf("want exactly 1 open replenish task, got %d", n)
	}
	ts, _ := ListWorkTasks(tenantID, "w1", loc, "")
	if len(ts) != 1 || ts[0].TaskType != "Replenish" || ts[0].FromBin != loc+"RES" || ts[0].ToBin != loc+"PF" || ts[0].Qty != 18 {
		t.Fatalf("queue = %+v; want Replenish of 18 from %sRES to %sPF", ts, loc, loc)
	}
	// The worker finishes it: the stock moves and the task closes.
	if _, err := StartWorkTask(tenantID, ts[0].DocID, "w1"); err != nil {
		t.Fatal(err)
	}
	if err := CompleteWorkTask(tenantID, ts[0].DocID, "w1", ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	var pf int
	_ = db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2", loc+"PF", sku).Scan(&pf)
	if pf != 20 || wqeOpen(schema, "Replenish", sku) != 0 {
		t.Fatalf("pick face holds %d, open tasks %d; want 20 and none", pf, wqeOpen(schema, "Replenish", sku))
	}
	// Above min: nothing is made.
	EmitReplenishTasks(tenantID, loc)
	if n := wqeOpen(schema, "Replenish", sku); n != 0 {
		t.Fatalf("pick face is full but %d task(s) made", n)
	}
}

func TestStage14633LeftReplenishAfterPick(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc = "WQE2"
	sku := wqeSeed(t, schema, insert, loc)
	insert("RULE-WQE2", "BinReplenishmentRule", "Active", "system", map[string]interface{}{"bin_code": loc + "PF", "sku": sku, "min_qty": 5, "max_qty": 20, "status": "Active"})
	wqeStock(t, schema, loc+"PF", sku, loc, 6)
	wqeStock(t, schema, loc+"RES", sku, loc, 30)
	if err := SetSetting(tenantID, "wms.work_queue", "true", "test"); err != nil {
		t.Fatal(err)
	}
	taskID, err := CreateFulfillmentTasks(tenantID, "ORD-WQE2", loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ReserveTaskBinStock(tenantID, taskID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // 6 -> 4 in the pick face, below the min of 5
		if _, _, err := ScanPickItem(tenantID, taskID, sku); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}
	if n := wqeOpen(schema, "Replenish", sku); n != 1 {
		t.Fatalf("a pick took the face below its min: want 1 replenish task, got %d", n)
	}
}

func TestStage14633LeftMoveRequest(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc = "WQE3"
	sku := wqeSeed(t, schema, insert, loc)
	wqeStock(t, schema, loc+"RES", sku, loc, 10)
	req := MoveRequest{FromBin: loc + "RES", ToBin: loc + "PF", SKU: sku, Qty: 4, Reason: "re-slot"}
	if _, err := RequestBinMove(tenantID, req, "boss"); err == nil {
		t.Fatal("a move request with the queue off was accepted")
	}
	if err := SetSetting(tenantID, "wms.work_queue", "true", "test"); err != nil {
		t.Fatal(err)
	}
	id1, err := RequestBinMove(tenantID, req, "boss")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := RequestBinMove(tenantID, req, "boss")
	if err != nil || id2 != id1 {
		t.Fatalf("a repeat request made a second task: %s vs %s (%v)", id1, id2, err)
	}
	if n := wqeOpen(schema, "Move", sku); n != 1 {
		t.Fatalf("open move tasks = %d", n)
	}
	// The move done any other way closes the task.
	if err := MoveBinStock(tenantID, BinMoveInput{FromBin: loc + "RES", ToBin: loc + "PF", SKU: sku, Qty: 4, UserID: "x", TaskType: "Move"}); err != nil {
		t.Fatal(err)
	}
	if n := wqeOpen(schema, "Move", sku); n != 0 {
		t.Fatalf("the move was made but %d task(s) stay open", n)
	}
}

func TestStage14633LeftCountTasksFromCountSession(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc = "WQE4"
	sku := wqeSeed(t, schema, insert, loc)
	insert(loc, "Location", "Active", "system", map[string]interface{}{"code": loc, "name": loc, "type": "Warehouse"})
	wqeStock(t, schema, loc+"PF", sku, loc, 5)
	wqeStock(t, schema, loc+"RES", sku, loc, 5)

	open := func(src string) int {
		var n int
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'WarehouseTask' AND data->>'task_type' = 'Count' AND data->>'source_doc_id' = $1 AND status IN ('Pending','Assigned','In Progress')", src).Scan(&n)
		return n
	}
	pi1, _, err := StartPhysicalInventory(tenantID, loc, "", "boss")
	if err != nil {
		t.Fatal(err)
	}
	if open(pi1) != 0 {
		t.Fatal("the queue is off but count tasks were made")
	}
	if err := CancelPhysicalInventory(tenantID, pi1, "boss"); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(tenantID, "wms.work_queue", "true", "test"); err != nil {
		t.Fatal(err)
	}
	pi2, _, err := StartPhysicalInventory(tenantID, loc, "", "boss")
	if err != nil {
		t.Fatal(err)
	}
	if n := open(pi2); n != 2 {
		t.Fatalf("want one count task per frozen bin (2), got %d", n)
	}
	if err := CancelPhysicalInventory(tenantID, pi2, "boss"); err != nil {
		t.Fatal(err)
	}
	if n := open(pi2); n != 0 {
		t.Fatalf("the count session was cancelled but %d task(s) stay open", n)
	}
	// Closed after its lines post: the tasks complete.
	pi3, _, err := StartPhysicalInventory(tenantID, loc, "", "boss")
	if err != nil {
		t.Fatal(err)
	}
	if open(pi3) != 2 {
		t.Fatal("third session made no tasks")
	}
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET status = 'Posted', data = data || '{\"status\":\"Posted\"}' WHERE doctype = 'CycleCountLine' AND data->>'count_session' = $1", pi3); err != nil {
		t.Fatal(err)
	}
	if err := ClosePhysicalInventory(tenantID, pi3, "boss"); err != nil {
		t.Fatal(err)
	}
	if n := open(pi3); n != 0 {
		t.Fatalf("closed session: %d task(s) stay open", n)
	}
	var done int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'WarehouseTask' AND data->>'source_doc_id' = $1 AND status = 'Completed'", pi3).Scan(&done)
	if done != 2 {
		t.Fatalf("closed session: want 2 completed tasks, got %d", done)
	}
}

func TestStage14633LeftShortPickCountIsServed(t *testing.T) {
	tenantID, _, _ := stage140Tenant(t)
	const loc = "WQE5"
	if err := SetSetting(tenantID, "wms.work_queue", "true", "test"); err != nil {
		t.Fatal(err)
	}
	raiseShortPickCount(tenantID, "HOLD-WQE5", "FT-WQE5", "WQE5-SKU", loc, "WQE5-BIN", 2, "system")
	ts, err := ListWorkTasks(tenantID, "w1", loc, "")
	if err != nil || len(ts) != 1 || ts[0].TaskType != "Count" || ts[0].FromBin != "WQE5-BIN" {
		t.Fatalf("queue = %+v %v; want the short pick's Count task for WQE5-BIN", ts, err)
	}
}
