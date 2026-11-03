package engines

import (
	"testing"

	"custom_erp/db"
)

// Stage 146.3.3: the handheld work queue. Tasks come from the places that
// already know the work (a posted receipt, a released wave), the next task
// is served by type priority from an open pool, starting it locks it to one
// worker, a task can be split in two, and the work done any other way
// (a desktop put-away) or the source being cancelled closes the task.

func wqTasks(t *testing.T, schema, source string) []WarehouseTaskInfo {
	t.Helper()
	rows, err := db.DB.Query("SELECT id, data::text, COALESCE(status,'') FROM "+schema+".documents WHERE doctype = 'WarehouseTask' AND data->>'source_doc_id' = $1 ORDER BY created_at, id", source)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []WarehouseTaskInfo
	for rows.Next() {
		var id, raw, st string
		if err := rows.Scan(&id, &raw, &st); err != nil {
			t.Fatal(err)
		}
		out = append(out, warehouseTaskFromData(id, raw, st))
	}
	return out
}

func TestStage14633PriorityOrderAndLock(t *testing.T) {
	tenantID, _, _ := stage140Tenant(t)
	const loc = "WQ1"
	mk := func(typ string) string {
		id, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: typ, LocationCode: loc, Item: "X", Qty: 3}, "system")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	// Created in the opposite order to the priority, so only the rank can
	// put them right.
	mk("Count")
	mk("Move")
	mk("Putaway")
	mk("Replenish")
	pick := mk("Pick")

	got, err := NextWorkTask(tenantID, "w1", loc, "")
	if err != nil || got == nil || got.DocID != pick {
		t.Fatalf("next task = %+v, %v; want the Pick task %s", got, err, pick)
	}
	// Looking is not taking: another worker is offered the same task.
	if other, _ := NextWorkTask(tenantID, "w2", loc, ""); other == nil || other.DocID != pick {
		t.Fatalf("an unstarted task must stay in the pool, w2 saw %+v", other)
	}
	if _, err := StartWorkTask(tenantID, pick, "w1"); err != nil {
		t.Fatalf("w1 start: %v", err)
	}
	if _, err := StartWorkTask(tenantID, pick, "w2"); err == nil {
		t.Fatal("a second worker started a task w1 already holds")
	}
	// w1 is handed back what they hold; w2 moves on to the next type.
	if again, _ := NextWorkTask(tenantID, "w1", loc, ""); again == nil || again.DocID != pick {
		t.Fatalf("w1 should resume their started task, got %+v", again)
	}
	if w2, _ := NextWorkTask(tenantID, "w2", loc, ""); w2 == nil || w2.TaskType != "Replenish" {
		t.Fatalf("w2 next = %+v; want Replenish", w2)
	}
	if err := CompleteWorkTask(tenantID, pick, "w2", ""); err == nil {
		t.Fatal("w2 completed a task w1 holds")
	}
	if err := CompleteWorkTask(tenantID, pick, "w1", ""); err != nil {
		t.Fatalf("w1 done: %v", err)
	}
	var order []string
	for i := 0; i < 4; i++ {
		n, err := NextWorkTask(tenantID, "w1", loc, "")
		if err != nil || n == nil {
			t.Fatalf("step %d: %+v %v", i, n, err)
		}
		order = append(order, n.TaskType)
		if _, err := StartWorkTask(tenantID, n.DocID, "w1"); err != nil {
			t.Fatal(err)
		}
		if n.TaskType == "Putaway" { // a put-away needs a bin and stock; its Done is tested on its own
			if err := CompleteWorkTask(tenantID, n.DocID, "w1", ""); err == nil {
				t.Fatal("a put-away was finished without a bin")
			}
			if err := TransitionWarehouseTaskStatus(tenantID, n.DocID, WTStatusCompleted, "", "w1", "w1"); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := CompleteWorkTask(tenantID, n.DocID, "w1", ""); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"Replenish", "Putaway", "Move", "Count"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v; want %v", order, want)
		}
	}
	if n, _ := NextWorkTask(tenantID, "w1", loc, ""); n != nil {
		t.Fatalf("queue should be empty, got %+v", n)
	}
}

func TestStage14633SplitAddsUp(t *testing.T) {
	tenantID, schema, _ := stage140Tenant(t)
	const loc = "WQ2"
	id, _ := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Pick", LocationCode: loc, Item: "X", Qty: 10, SourceDocType: "Wave", SourceDocID: "WAVE-SPLIT"}, "system")
	for _, bad := range []float64{0, -1, 10, 11} {
		if _, err := SplitWorkTask(tenantID, id, bad, "w1"); err == nil {
			t.Fatalf("split of %v out of 10 was allowed", bad)
		}
	}
	if _, err := StartWorkTask(tenantID, id, "w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := SplitWorkTask(tenantID, id, 4, "w2"); err == nil {
		t.Fatal("w2 split a task w1 holds")
	}
	sib, err := SplitWorkTask(tenantID, id, 4, "w1")
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	a, _ := GetWarehouseTask(tenantID, id)
	b, _ := GetWarehouseTask(tenantID, sib)
	if a.Qty != 6 || b.Qty != 4 || a.Qty+b.Qty != 10 {
		t.Fatalf("parts = %v and %v; want 6 + 4", a.Qty, b.Qty)
	}
	if a.Status != WTStatusInProgress || b.Status != WTStatusPending {
		t.Fatalf("held part %s, new part %s; want In Progress and Pending", a.Status, b.Status)
	}
	if b.Item != "X" || b.SourceDocID != "WAVE-SPLIT" || b.TaskType != "Pick" {
		t.Fatalf("the new part lost its source or type: %+v", b)
	}
	// The new part is in the pool for anyone.
	if n, _ := NextWorkTask(tenantID, "w2", loc, ""); n == nil || n.DocID != sib {
		t.Fatalf("w2 next = %+v; want the split part %s", n, sib)
	}
	if len(wqTasks(t, schema, "WAVE-SPLIT")) != 2 {
		t.Fatal("expected exactly two tasks after one split")
	}
}

func TestStage14633FilterBySourceDocument(t *testing.T) {
	tenantID, _, _ := stage140Tenant(t)
	const loc = "WQ3"
	for _, src := range []string{"GRN-A", "GRN-B"} {
		if _, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Putaway", LocationCode: loc, Item: "X", Qty: 1, SourceDocType: "GRN", SourceDocID: src}, "system"); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := ListWorkTasks(tenantID, "w1", loc, "")
	one, _ := ListWorkTasks(tenantID, "w1", loc, "GRN-B")
	if len(all) != 2 || len(one) != 1 || one[0].SourceDocID != "GRN-B" {
		t.Fatalf("all=%d one=%+v", len(all), one)
	}
	if n, _ := NextWorkTask(tenantID, "w1", loc, "GRN-B"); n == nil || n.SourceDocID != "GRN-B" {
		t.Fatalf("next for GRN-B = %+v", n)
	}
}

func TestStage14633DesktopPutawayClosesTask(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc, sku, bin = "WQ4", "WQ4-SKU", "WQ4-BIN"
	insert("ITEM-"+sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Q"})
	insert("BIN-"+bin, "Bin", "Active", "system", map[string]interface{}{"bin_code": bin, "location": loc})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", sku, loc); err != nil {
		t.Fatal(err)
	}
	id, _ := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Putaway", LocationCode: loc, Item: sku, Qty: 5, SourceDocType: "GRN", SourceDocID: "GRN-WQ4"}, "system")
	other, _ := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Putaway", LocationCode: loc, Item: "ELSE", Qty: 5, SourceDocType: "GRN", SourceDocID: "GRN-WQ4"}, "system")

	// Part of the stock: the task shrinks, it does not close.
	if err := PutawayToBin(tenantID, bin, sku, 2, "desk1"); err != nil {
		t.Fatal(err)
	}
	if tk, _ := GetWarehouseTask(tenantID, id); tk.Status == WTStatusCompleted || tk.Qty != 3 {
		t.Fatalf("after 2 of 5 the task is %s qty %v; want open with 3", tk.Status, tk.Qty)
	}
	if err := PutawayToBin(tenantID, bin, sku, 3, "desk1"); err != nil {
		t.Fatal(err)
	}
	if tk, _ := GetWarehouseTask(tenantID, id); tk.Status != WTStatusCompleted {
		t.Fatalf("after the rest the task is %s; want Completed", tk.Status)
	}
	if tk, _ := GetWarehouseTask(tenantID, other); tk.Status != WTStatusPending {
		t.Fatalf("another item's task changed: %s", tk.Status)
	}
}

func TestStage14633HandheldPutawayDone(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc, sku, bin = "WQ5", "WQ5-SKU", "WQ5-BIN"
	insert("ITEM-"+sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Q"})
	insert("BIN-"+bin, "Bin", "Active", "system", map[string]interface{}{"bin_code": bin, "location": loc})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 4, 4)", sku, loc); err != nil {
		t.Fatal(err)
	}
	id, _ := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Putaway", LocationCode: loc, Item: sku, Qty: 4}, "system")
	if _, err := StartWorkTask(tenantID, id, "w1"); err != nil {
		t.Fatal(err)
	}
	if err := CompleteWorkTask(tenantID, id, "w1", ""); err == nil {
		t.Fatal("a put-away finished with no bin")
	}
	if err := CompleteWorkTask(tenantID, id, "w1", bin); err != nil {
		t.Fatalf("done with bin: %v", err)
	}
	if tk, _ := GetWarehouseTask(tenantID, id); tk.Status != WTStatusCompleted {
		t.Fatalf("status %s; want Completed", tk.Status)
	}
	var qty int
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(qty),0) FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2", bin, sku).Scan(&qty)
	if qty != 4 {
		t.Fatalf("bin holds %d; want 4", qty)
	}
}

func TestStage14633SourceCancelCancelsTasks(t *testing.T) {
	tenantID, schema, _ := stage140Tenant(t)
	const loc = "WQ6"
	open, _ := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Putaway", LocationCode: loc, Item: "X", Qty: 1, SourceDocType: "GRN", SourceDocID: "GRN-WQ6"}, "system")
	held, _ := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Putaway", LocationCode: loc, Item: "Y", Qty: 1, SourceDocType: "GRN", SourceDocID: "GRN-WQ6"}, "system")
	done, _ := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Putaway", LocationCode: loc, Item: "Z", Qty: 1, SourceDocType: "GRN", SourceDocID: "GRN-WQ6"}, "system")
	elsewhere, _ := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Putaway", LocationCode: loc, Item: "X", Qty: 1, SourceDocType: "GRN", SourceDocID: "GRN-OTHER"}, "system")
	_, _ = StartWorkTask(tenantID, held, "w1")
	_ = TransitionWarehouseTaskStatus(tenantID, done, WTStatusCompleted, "", "w1", "w1")

	if n := CancelWorkTasksForSource(tenantID, "GRN", "GRN-WQ6", "desk1"); n != 2 {
		t.Fatalf("cancelled %d tasks; want 2 (the open one and the held one)", n)
	}
	for id, want := range map[string]string{open: WTStatusCancelled, held: WTStatusCancelled, done: WTStatusCompleted, elsewhere: WTStatusPending} {
		if tk, _ := GetWarehouseTask(tenantID, id); tk.Status != want {
			t.Fatalf("task %s is %s; want %s", id, tk.Status, want)
		}
	}
	_ = schema
}

func TestStage14633ReceiptCreatesPutawayTasksOnlyWhenOn(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc, sku = "WQ7", "WQ7-SKU"
	insert("ITEM-"+sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Q"})
	items := func(qty float64) []interface{} {
		return []interface{}{map[string]interface{}{"sku": sku, "qty": qty, "accepted_qty": qty - 1, "rejected_qty": 1.0, "rejection_reason": "x"}}
	}
	if _, err := PostGRNReceiptWithQC(tenantID, loc, items(6), "system", "GRN-WQ7-OFF"); err != nil {
		t.Fatal(err)
	}
	if n := len(wqTasks(t, schema, "GRN-WQ7-OFF")); n != 0 {
		t.Fatalf("the queue is off but %d task(s) were created", n)
	}
	if err := SetSetting(tenantID, "wms.work_queue", "true", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := PostGRNReceiptWithQC(tenantID, loc, items(6), "system", "GRN-WQ7-ON"); err != nil {
		t.Fatal(err)
	}
	ts := wqTasks(t, schema, "GRN-WQ7-ON")
	if len(ts) != 1 || ts[0].TaskType != "Putaway" || ts[0].Qty != 5 || ts[0].Item != sku || ts[0].Status != WTStatusPending || ts[0].SourceDocType != "GRN" {
		t.Fatalf("tasks = %+v; want one pending Putaway of the 5 accepted pieces", ts)
	}
}

func TestStage14633ReleasedWaveCreatesPickTasks(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc, sku, bin, wave = "WQ8", "WQ8-SKU", "WQ8-BIN", "WAVE-WQ8"
	insert(loc, "Location", "Active", "system", map[string]interface{}{"code": loc, "name": "WQ8", "type": "Warehouse"})
	insert("BINDOC-"+bin, "Bin", "Active", "system", map[string]interface{}{"bin_code": bin, "location": loc})
	insert("ITEM-"+sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Q"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 6.0}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', 6)", bin, sku, loc); err != nil {
		t.Fatal(err)
	}
	insert("FT-WQ8", "FulfillmentTask", "Pending", "system", map[string]interface{}{"location_code": loc,
		"items": []interface{}{map[string]interface{}{"sku": sku, "qty": 4}}})
	wid, _, err := CreateWave(tenantID, loc, "", []string{"FT-WQ8"}, "Manual", "system")
	if err != nil {
		t.Fatal(err)
	}
	_ = wave
	if err := TransitionWaveStatus(tenantID, wid, "Released", "system"); err != nil {
		t.Fatal(err)
	}
	if n := len(wqTasks(t, schema, "FT-WQ8")); n != 0 {
		t.Fatalf("the queue is off but the release made %d pick task(s)", n)
	}
	// Same again with the queue on, on a second wave.
	if err := SetSetting(tenantID, "wms.work_queue", "true", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE " + schema + ".documents SET data = data - 'wave_id' WHERE id = 'FT-WQ8'"); err != nil {
		t.Fatal(err)
	}
	wid2, _, err := CreateWave(tenantID, loc, "", []string{"FT-WQ8"}, "Manual", "system")
	if err != nil {
		t.Fatal(err)
	}
	if err := TransitionWaveStatus(tenantID, wid2, "Released", "system"); err != nil {
		t.Fatal(err)
	}
	ts := wqTasks(t, schema, "FT-WQ8")
	if len(ts) != 1 || ts[0].TaskType != "Pick" || ts[0].Qty != 4 || ts[0].FromBin != bin || ts[0].WaveID != wid2 || ts[0].Item != sku {
		t.Fatalf("tasks = %+v; want one Pick of 4 from %s on %s", ts, bin, wid2)
	}
	// A second pick list for the same wave does not double the work.
	if _, _, err := GenerateWavePickList(tenantID, wid2); err != nil {
		t.Fatal(err)
	}
	if n := len(wqTasks(t, schema, "FT-WQ8")); n != 1 {
		t.Fatalf("a repeat pick list made %d tasks; want still 1", n)
	}
}
