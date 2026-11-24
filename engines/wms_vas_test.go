package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
	"custom_erp/internal/testfault"
)

// 252.1: completing a VAS task consumes its components, produces its output
// and bins it in one transaction - a failure part-way leaves no components
// consumed without their output - and a retry never posts the stock twice.
func TestVASCompletionIsAllOrNothing(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc, comp, kit, bom, toBin = "WH2521", "COMP2521", "KIT2521", "BOM-2521", "BIN-2521"
	onHand := func(sku string) int {
		t.Helper()
		var n int
		_ = db.DB.QueryRow("SELECT COALESCE(SUM(on_hand), 0) FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&n)
		return n
	}
	count := func(q string, args ...interface{}) int {
		t.Helper()
		var n int
		if err := db.DB.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}

	components, _ := json.Marshal([]bomComponent{{Sku: comp, Qty: 2}})
	insert(bom, "BOM", "Active", "tester", map[string]interface{}{"parent_item": kit, "components": string(components)})
	if _, err := PostInventoryLedgerWithVoucher(tenantID, loc, []interface{}{map[string]interface{}{"sku": comp, "qty": 10.0}}, false, "Test", "T252-1", "tester"); err != nil {
		t.Fatal(err)
	}
	taskID, err := CreateVASTask(tenantID, loc, "BIN-A", toBin, kit, 2, bom, "", "tester")
	if err != nil {
		t.Fatalf("CreateVASTask: %v", err)
	}

	// The output's bin refuses: nothing of the task may stay posted.
	disarm := testfault.Arm(t, db.DB, schema, testfault.Step{Label: "output-bin", Table: "bin_stock", On: "INSERT"}, toBin)
	if err := CompleteVASTask(tenantID, taskID, "", "tester"); err == nil {
		t.Fatal("CompleteVASTask succeeded with its output bin refusing")
	}
	if got := onHand(comp); got != 10 {
		t.Fatalf("components on hand after the refused completion = %d, want 10 - consumed with no output", got)
	}
	if got := onHand(kit); got != 0 {
		t.Fatalf("output on hand after the refused completion = %d, want 0", got)
	}
	if n := count("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", taskID); n != 0 {
		t.Fatalf("%d stock ledger line(s) kept for the refused completion", n)
	}
	disarm()

	// Now it completes: 4 components consumed, 2 kits made and binned.
	if err := CompleteVASTask(tenantID, taskID, "", "tester"); err != nil {
		t.Fatalf("CompleteVASTask: %v", err)
	}
	if c, k := onHand(comp), onHand(kit); c != 6 || k != 2 {
		t.Fatalf("after completion components=%d kits=%d, want 6 and 2", c, k)
	}
	if n := count("SELECT COALESCE(SUM(qty), 0)::int FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2", toBin, kit); n != 2 {
		t.Fatalf("output bin holds %d, want 2", n)
	}

	// A retry of the stock step (as after a failed status change) posts nothing.
	parts, err := explodeBOMComponents(tenantID, bom, 2, map[string]bool{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := postVASStock(schema, taskID, "tester", loc, toBin, kit, 2, parts); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if c, k := onHand(comp), onHand(kit); c != 6 || k != 2 {
		t.Fatalf("after a retry components=%d kits=%d, want 6 and 2 - posted twice", c, k)
	}
}

// 252.2: the wave monitor and wave detail count the wave's tasks live, so a
// task added after the wave was made shows in its total.
func TestWaveTaskCountIsLive(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc = "WH2522"
	waveID, _, err := CreateWave(tenantID, loc, "", nil, "Manual", "tester")
	if err != nil {
		t.Fatalf("CreateWave: %v", err)
	}
	for _, id := range []string{"FT-2522-A", "FT-2522-B"} {
		insert(id, "FulfillmentTask", "Picking", "tester", map[string]interface{}{"wave_id": waveID, "location_code": loc})
	}
	insert("FT-2522-C", "FulfillmentTask", "Packed", "tester", map[string]interface{}{"wave_id": waveID, "location_code": loc})

	info, err := getWave(schema, waveID)
	if err != nil || info == nil {
		t.Fatalf("getWave: %v %v", info, err)
	}
	if info.TaskCount != 3 {
		t.Fatalf("wave detail task count = %d, want 3 counted live", info.TaskCount)
	}
	rows, err := GetWaveMonitor(tenantID, loc)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.WaveID == waveID {
			if r.TaskCount != 3 || r.OpenTasks != 2 {
				t.Fatalf("wave monitor total=%d open=%d, want 3 and 2", r.TaskCount, r.OpenTasks)
			}
			return
		}
	}
	t.Fatalf("wave %s not on the monitor: %+v", waveID, rows)
}
