package engines

import (
	"custom_erp/db"
	"strconv"
	"testing"
)

// Stage 146.3.5 (D7): one wave may mix sales-order pick tasks and store
// transfers from the same location. A transfer's pick lines come from bins
// like an order's, they reach the work queue through the wave pick-task
// emitter, and dispatch consumes what the wave reserved. Off (default) = a
// transfer never joins a wave and dispatches as today.
func TestStage14635TransfersInWaves(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc, sku, bin, store = "WT5", "WT5-SKU", "WT5-BIN", "WT5-STORE"
	insert(loc, "Location", "Active", "system", map[string]interface{}{"code": loc, "name": "WT5", "type": "Warehouse"})
	insert("BINDOC-"+bin, "Bin", "Active", "system", map[string]interface{}{"bin_code": bin, "location": loc})
	insert("ITEM-"+sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Q"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', 10)", bin, sku, loc); err != nil {
		t.Fatal(err)
	}
	insert("FT-WT5", "FulfillmentTask", "Pending", "system", map[string]interface{}{"location_code": loc,
		"items": []interface{}{map[string]interface{}{"sku": sku, "qty": 4}}})
	transfer := func(id string, qty int) {
		insert(id, "TransferOrder", "Approved", "system", map[string]interface{}{
			"transfer_number": id, "from_warehouse": loc, "to_warehouse": store, "status": "Approved",
			"items": `[{"sku":"` + sku + `","qty":` + strconv.Itoa(qty) + `}]`,
		})
	}
	transfer("TO-WT5-A", 3)
	transfer("TO-WT5-B", 2)
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		return ""
	}
	openFor := func(id string) (n int) {
		_ = db.DB.QueryRow("SELECT COALESCE(SUM(qty - picked_qty),0) FROM "+schema+".bin_allocation WHERE task_id = $1 AND status = 'Open'", id).Scan(&n)
		return
	}
	for _, s := range []struct{ k, v string }{{"wms.work_queue", "true"}} {
		if err := SetSetting(tenantID, s.k, s.v, "test"); err != nil {
			t.Fatal(err)
		}
	}

	// Off: a transfer cannot join a wave.
	wid, _, err := CreateWave(tenantID, loc, "", []string{"FT-WT5"}, "Manual", "system")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AssignTransfersToWave(tenantID, wid, []string{"TO-WT5-A"}, "system"); code(err) != "STOCKT-0491" {
		t.Fatalf("setting off: assign should be STOCKT-0491, got %v", err)
	}

	// On: it joins, a transfer from another location or one not Approved does not.
	if err := SetSetting(tenantID, TransferWavesSetting, "true", "test"); err != nil {
		t.Fatal(err)
	}
	insert("TO-WT5-DRAFT", "TransferOrder", "Draft", "system", map[string]interface{}{"from_warehouse": loc, "to_warehouse": store, "items": "[]"})
	insert("TO-WT5-ELSE", "TransferOrder", "Approved", "system", map[string]interface{}{"from_warehouse": "OTHER", "to_warehouse": store, "items": "[]"})
	if _, err := AssignTransfersToWave(tenantID, wid, []string{"TO-WT5-DRAFT"}, "system"); code(err) != "STOCKT-0491" {
		t.Fatalf("a Draft transfer should be refused (STOCKT-0491), got %v", err)
	}
	if _, err := AssignTransfersToWave(tenantID, wid, []string{"TO-WT5-ELSE"}, "system"); code(err) != "STOCKT-0491" {
		t.Fatalf("a transfer from another location should be refused (STOCKT-0491), got %v", err)
	}
	n, err := AssignTransfersToWave(tenantID, wid, []string{"TO-WT5-A", "TO-WT5-B"}, "system")
	if err != nil || n != 2 {
		t.Fatalf("assign = %d, %v; want 2", n, err)
	}

	// Releasing the wave reserves bins for the order and both transfers and
	// hands the transfers' picks to the work queue as TransferOrder-sourced tasks.
	if err := TransitionWaveStatus(tenantID, wid, "Released", "system"); err != nil {
		t.Fatal(err)
	}
	if openFor("FT-WT5") != 4 || openFor("TO-WT5-A") != 3 || openFor("TO-WT5-B") != 2 {
		t.Fatalf("reserved order/A/B = %d/%d/%d, want 4/3/2", openFor("FT-WT5"), openFor("TO-WT5-A"), openFor("TO-WT5-B"))
	}
	ts := wqTasks(t, schema, "TO-WT5-A")
	if len(ts) != 1 || ts[0].TaskType != "Pick" || ts[0].Qty != 3 || ts[0].FromBin != bin || ts[0].SourceDocType != "TransferOrder" || ts[0].WaveID != wid {
		t.Fatalf("transfer pick tasks = %+v; want one Pick of 3 from %s, source TransferOrder, on %s", ts, bin, wid)
	}
	lines, allocs, err := GenerateWavePickList(tenantID, wid)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, l := range lines {
		total += l.PickQty
	}
	if total != 9 {
		t.Fatalf("wave walk picks %d, want 9 (4 order + 3 + 2 transfers)", total)
	}
	gotAlloc := map[string]int{}
	for _, a := range allocs {
		gotAlloc[a.TaskID] += a.AllocatedQty
	}
	if gotAlloc["FT-WT5"] != 4 || gotAlloc["TO-WT5-A"] != 3 || gotAlloc["TO-WT5-B"] != 2 {
		t.Fatalf("allocations = %v, want order 4, A 3, B 2", gotAlloc)
	}
	if r, _ := BinReservedQty(db.DB, schema, bin, sku, ""); r != 9 {
		t.Fatalf("bin reserved = %d, want 9: a transfer's reservation must count as live", r)
	}

	// Dispatch consumes the transfer's own reservation: the bin falls by 3,
	// the other holds stay, and nothing is left open for the dispatched transfer.
	if err := DispatchTransferOrder(tenantID, "TO-WT5-A", "system"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var binQty int
	_ = db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2", bin, sku).Scan(&binQty)
	if binQty != 7 {
		t.Fatalf("bin stock = %d, want 7 (10 - 3 dispatched)", binQty)
	}
	if openFor("TO-WT5-A") != 0 {
		t.Fatalf("a dispatched transfer must hold nothing, still %d", openFor("TO-WT5-A"))
	}
	if r, _ := BinReservedQty(db.DB, schema, bin, sku, ""); r != 6 {
		t.Fatalf("bin reserved after dispatch = %d, want 6 (order 4 + B 2)", r)
	}
	if rows, err := BinStockReconciliation(tenantID, sku, loc, true); err != nil || len(rows) != 0 {
		t.Fatalf("reconciliation = %v, %v; want clean", rows, err)
	}

	// Cancelling B frees its bins and cancels its pick task.
	reason := "RC-WT5-CANCEL"
	insert(reason, "ReasonCode", "Active", "system", map[string]interface{}{"code": reason, "description": "x", "category": "Cancellation"})
	if err := CancelTransferOrder(tenantID, "TO-WT5-B", "system", reason, ""); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if openFor("TO-WT5-B") != 0 {
		t.Fatalf("a cancelled transfer must release its bins, still %d", openFor("TO-WT5-B"))
	}
	if ts := wqTasks(t, schema, "TO-WT5-B"); len(ts) != 1 || ts[0].Status != WTStatusCancelled {
		t.Fatalf("cancelled transfer's pick task = %+v; want cancelled", ts)
	}
}
