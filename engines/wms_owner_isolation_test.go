package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// TestStage59MixedOwnerIsolation is 59.5's adversarial two-owner run on a
// provisioned throwaway client: owners A and B share a warehouse and even a
// bin, and every step of the lifecycle - receive, allocate, pick, pack, ship,
// adjust, move, count, bill - must keep A's units for A and B's for B.
func TestStage59MixedOwnerIsolation(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	const loc, sku, ownerA, ownerB = "WH-3PL", "SKU-3PL", "OWN-A", "OWN-B"
	if err := SetSetting(tenantID, StockOwnershipModeSetting, OwnershipMixed, "system"); err != nil {
		t.Fatalf("mixed mode: %v", err)
	}
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('ITEM-3PL', 'Item', $1, 'Active', 'system')",
		fmt.Sprintf(`{"code":%q,"name":"3PL widget"}`, sku))
	bin := func(code, owner string) {
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')",
			"BIN-"+code, fmt.Sprintf(`{"bin_code":%q,"location":%q,"zone":"","status":"Active","owner_id":%q}`, code, loc, owner))
	}
	bin("B1", "")     // shared bin
	bin("B2", ownerB) // B's own bin
	bin("B3", "")     // empty, for a move
	// Receive: the goods receipt posted 16 units at the location.
	exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 16, 16)", sku, loc)
	binQty := func(b, cond string) (n int) {
		db.DB.QueryRow("SELECT COALESCE(SUM(qty),0) FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = $3", b, sku, cond).Scan(&n)
		return
	}
	slice := func(b, cond, owner string) (n int) {
		db.DB.QueryRow("SELECT COALESCE(SUM(qty),0) FROM "+schema+".bin_stock_owner WHERE bin_code = $1 AND sku = $2 AND condition = $3 AND owner_id = $4", b, sku, cond, owner).Scan(&n)
		return
	}

	// --- receive / putaway: each owner's units are recorded as theirs ------
	if err := PutawayToBinForOwner(tenantID, "B1", sku, 6, ownerA, "rcv"); err != nil {
		t.Fatalf("putaway A into B1: %v", err)
	}
	if err := PutawayToBinForOwner(tenantID, "B1", sku, 4, ownerB, "rcv"); err != nil {
		t.Fatalf("putaway B into B1: %v", err)
	}
	if err := PutawayToBinForOwner(tenantID, "B2", sku, 5, ownerB, "rcv"); err != nil {
		t.Fatalf("putaway B into its own bin: %v", err)
	}
	if slice("B1", "Good", ownerA) != 6 || slice("B1", "Good", ownerB) != 4 || slice("B2", "Good", ownerB) != 0 || binQty("B2", "Good") != 5 {
		t.Fatalf("after putaway: B1 A=%d B=%d, B2 slice=%d qty=%d", slice("B1", "Good", ownerA), slice("B1", "Good", ownerB), slice("B2", "Good", ownerB), binQty("B2", "Good"))
	}
	if err := PutawayToBinForOwner(tenantID, "B2", sku, 1, ownerA, "rcv"); code(err) != "INVENT-0495" {
		t.Fatalf("A's stock into B's bin: want INVENT-0495, got %v", err)
	}
	if err := PutawayToBinForOwner(tenantID, "B1", sku, 1, "", "rcv"); code(err) != "INVENT-0496" {
		t.Fatalf("ownerless putaway in a mixed warehouse: want INVENT-0496, got %v", err)
	}
	// A bin's owner cannot be changed under its stock.
	if err := validateBinOwnerChange(tenantID, "BIN-B2", map[string]interface{}{"owner_id": ownerA}); code(err) != "INVENT-0494" {
		t.Fatalf("re-owning a stocked bin: want INVENT-0494, got %v", err)
	}

	// --- availability: each owner sees only their own ---------------------
	if a, _ := OwnerAvailableToSell(tenantID, sku, loc, ownerA); a.Available != 6 {
		t.Fatalf("A's ATS %+v, want 6", a)
	}
	if b, _ := OwnerAvailableToSell(tenantID, sku, loc, ownerB); b.Available != 9 {
		t.Fatalf("B's ATS %+v, want 9", b)
	}
	plan, err := OwnerCheckedPlan(tenantID, ownerA, []SalesOrderLineInput{{SKU: sku, Qty: 7}}, uniformPlan(loc, "Default", 1))
	if err != nil || plan != nil {
		t.Fatalf("A cannot be sourced 7 from %s (holds 6): plan %+v err %v", loc, plan, err)
	}

	// --- release: the task inherits the order's owner; ownerless refused ---
	order := func(id, owner string, qty int) {
		data := map[string]interface{}{"order_status": "Reserved", "channel": "Manual"}
		if owner != "" {
			data["owner_id"] = owner
		}
		b, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesOrder', $2, 'Reserved', 'system')", id, b)
		lb, _ := json.Marshal(map[string]interface{}{"order_id": id, "sku": sku, "qty": qty, "line_status": "Reserved", "location_code": loc})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesOrderLine', $2, 'Reserved', 'system')", id+"-L1", lb)
	}
	order("SO-NONE", "", 1)
	if _, _, err := ReleaseOrderToFulfillment(tenantID, "SO-NONE", "ops"); code(err) != "INVENT-0492" {
		t.Fatalf("ownerless order at a mixed location: want INVENT-0492, got %v", err)
	}
	if err := SetOrderOwner(tenantID, "SO-NONE", ownerB, "ops"); err != nil {
		t.Fatalf("SetOrderOwner: %v", err)
	}
	order("SO-A", ownerA, 6)
	order("SO-A2", ownerA, 1)
	order("SO-B", ownerB, 7)
	release := func(id string) string {
		t.Helper()
		ids, _, err := ReleaseOrderToFulfillment(tenantID, id, "ops")
		if err != nil || len(ids) != 1 {
			t.Fatalf("release %s: %v %v", id, ids, err)
		}
		return ids[0]
	}
	taskA, taskA2, taskB := release("SO-A"), release("SO-A2"), release("SO-B")
	if o, _ := taskOwnerQ(db.DB, schema, taskB); o != ownerB {
		t.Fatalf("task for SO-B has owner %q", o)
	}
	if err := SetOrderOwner(tenantID, "SO-A", ownerB, "ops"); code(err) != "INVENT-0493" {
		t.Fatalf("changing a released order's owner: want INVENT-0493, got %v", err)
	}

	// --- allocate: A gets only A's units; A2 finds none left; B never touches A's
	pick := func(task string) (got, short int) {
		lines, err := GenerateBinPickList(tenantID, task)
		if err != nil {
			t.Fatalf("pick list %s: %v", task, err)
		}
		for _, l := range lines {
			got += l.PickQty
			short += l.Shortfall
		}
		return
	}
	if got, short := pick(taskA); got != 6 || short != 0 {
		t.Fatalf("A's pick list %d/%d short, want 6/0", got, short)
	}
	if got, short := pick(taskA2); got != 0 || short != 1 {
		t.Fatalf("A2 must find none of A's left (B's 9 do not count): got %d short %d", got, short)
	}
	if got, short := pick(taskB); got != 7 || short != 0 {
		t.Fatalf("B's pick list %d/%d short, want 7/0", got, short)
	}
	var wrongOwner int
	db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.bin_allocation WHERE (task_id = $1 AND owner_id <> $2) OR (task_id = $3 AND owner_id <> $4)`,
		taskA, ownerA, taskB, ownerB).Scan(&wrongOwner)
	if wrongOwner != 0 {
		t.Fatalf("%d reservation rows carry the wrong owner", wrongOwner)
	}
	var bFromB1 int
	db.DB.QueryRow(`SELECT COALESCE(SUM(qty),0) FROM `+schema+`.bin_allocation WHERE task_id = $1 AND bin_code = 'B1'`, taskB).Scan(&bFromB1)
	if bFromB1 > 4 {
		t.Fatalf("B reserved %d in B1 where it owns 4", bFromB1)
	}

	// --- pick, pack, ship ---------------------------------------------------
	if _, _, err := ScanPickItem(tenantID, taskA, sku); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if slice("B1", "Good", ownerA) != 5 {
		t.Fatalf("a scan takes one of A's units: A slice %d, want 5", slice("B1", "Good", ownerA))
	}
	for _, st := range []string{"Packed", "Dispatched"} {
		if err := TransitionTaskStatus(tenantID, taskA, st); err != nil {
			t.Fatalf("task A -> %s: %v", st, err)
		}
		if err := TransitionTaskStatus(tenantID, taskB, st); err != nil {
			t.Fatalf("task B -> %s: %v", st, err)
		}
	}
	bLeftB1 := 4 - bFromB1
	if slice("B1", "Good", ownerA) != 0 || slice("B1", "Good", ownerB) != bLeftB1 || binQty("B1", "Good") != bLeftB1 {
		t.Fatalf("after ship: B1 A=%d B=%d qty=%d, want 0/%d/%d", slice("B1", "Good", ownerA), slice("B1", "Good", ownerB), binQty("B1", "Good"), bLeftB1, bLeftB1)
	}
	if binQty("B2", "Good") != 5-(7-bFromB1) {
		t.Fatalf("B2 after ship %d, want %d", binQty("B2", "Good"), 5-(7-bFromB1))
	}

	// --- adjust (condition) and move keep the owner ------------------------
	exec("UPDATE "+schema+".documents SET status = 'Rejected' WHERE id = $1", taskA2) // A2 holds nothing; clear it
	// A second receipt: 3 for A and 1 for B, both into the shared bin.
	exec("UPDATE "+schema+".inventory_availability SET on_hand = on_hand + 4, available = available + 4 WHERE sku = $1 AND location_code = $2", sku, loc)
	if err := PutawayToBinForOwner(tenantID, "B1", sku, 3, ownerA, "rcv"); err != nil {
		t.Fatalf("second receipt for A: %v", err)
	}
	if err := PutawayToBinForOwner(tenantID, "B1", sku, 1, ownerB, "rcv"); err != nil {
		t.Fatalf("second receipt for B: %v", err)
	}
	bLeftB1++
	if err := TransitionBinStockConditionForOwner(tenantID, "B1", sku, 1, "Good", "Damaged", "", "ops"); code(err) != "INVENT-0496" {
		t.Fatalf("condition change in a shared bin without an owner: want INVENT-0496, got %v", err)
	}
	if err := TransitionBinStockConditionForOwner(tenantID, "B1", sku, 1, "Good", "Damaged", ownerA, "ops"); err != nil {
		t.Fatalf("condition change for A: %v", err)
	}
	if slice("B1", "Good", ownerA) != 2 || slice("B1", "Damaged", ownerA) != 1 {
		t.Fatalf("A's slices after damage: good %d damaged %d", slice("B1", "Good", ownerA), slice("B1", "Damaged", ownerA))
	}
	if err := MoveBinStock(tenantID, BinMoveInput{FromBin: "B1", ToBin: "B2", SKU: sku, Qty: 1, OwnerID: ownerA, UserID: "ops"}); code(err) != "INVENT-0495" {
		t.Fatalf("A's units into B's bin: want INVENT-0495, got %v", err)
	}
	if err := MoveBinStock(tenantID, BinMoveInput{FromBin: "B1", ToBin: "B3", SKU: sku, Qty: 2, OwnerID: ownerA, UserID: "ops"}); err != nil {
		t.Fatalf("move A's units to B3: %v", err)
	}
	if slice("B1", "Good", ownerA) != 0 || slice("B3", "Good", ownerA) != 2 {
		t.Fatalf("after move: B1 A=%d B3 A=%d", slice("B1", "Good", ownerA), slice("B3", "Good", ownerA))
	}

	// --- count: the posted variance carries the owner -----------------------
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('CCL-3PL', 'CycleCountLine', $1, 'Approved', 'counter')",
		fmt.Sprintf(`{"sku":%q,"location":%q,"variance":-1,"variance_reason_code":"RC","owner_id":%q}`, sku, loc, ownerB))
	if err := PostCycleCountAdjustment(tenantID, "CCL-3PL", "manager"); err != nil {
		t.Fatalf("post count: %v", err)
	}
	var ledgerOwner string
	db.DB.QueryRow(`SELECT COALESCE(data->>'owner_id','') FROM ` + schema + `.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = 'CCL-3PL'`).Scan(&ledgerOwner)
	if ledgerOwner != ownerB {
		t.Fatalf("count ledger owner %q, want %s", ledgerOwner, ownerB)
	}

	// --- bill: each owner is charged for their own stock --------------------
	aQty, _ := OwnerStockQty(tenantID, ownerA, loc, sku)
	bQty, _ := OwnerStockQty(tenantID, ownerB, loc, sku)
	if aQty != 2 || bQty != bLeftB1+binQty("B2", "Good") {
		t.Fatalf("billing quantities A=%d (want 2: B3's two) B=%d (want %d)", aQty, bQty, bLeftB1+binQty("B2", "Good"))
	}

	// --- exceptions: a bin that lost stock behind the owners' backs ---------
	exec("UPDATE "+schema+".bin_stock SET qty = 0 WHERE bin_code = 'B3' AND sku = $1 AND condition = 'Good'", sku)
	ex, err := OwnerStockExceptions(tenantID)
	if err != nil {
		t.Fatalf("exceptions: %v", err)
	}
	found := false
	for _, e := range ex {
		if e.BinCode == "B3" && e.Kind == "Owner slices exceed the bin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want an exception for B3, got %+v", ex)
	}
	if err := SetOwnerSliceQty(tenantID, "B3", sku, "Good", ownerA, 1, "", "manager"); err == nil {
		t.Fatalf("a correction needs a reason")
	}
	if err := SetOwnerSliceQty(tenantID, "B3", sku, "Good", ownerA, 1, "counted", "manager"); code(err) != "INVENT-0494" {
		t.Fatalf("crediting A with more than the empty bin holds: want INVENT-0494, got %v", err)
	}
	if err := SetOwnerSliceQty(tenantID, "B3", sku, "Good", ownerA, 0, "counted: bin empty", "manager"); err != nil {
		t.Fatalf("correct: %v", err)
	}
	ex, _ = OwnerStockExceptions(tenantID)
	for _, e := range ex {
		if e.BinCode == "B3" {
			t.Fatalf("B3 still an exception after the correction: %+v", e)
		}
	}

	// --- one-owner mode: a dedicated warehouse refuses another owner's order
	if err := SetSetting(tenantID, StockOwnershipModeSetting, OwnershipSingleOwner, "system"); err != nil {
		t.Fatalf("single mode: %v", err)
	}
	exec("INSERT INTO "+schema+".warehouse_owner (location_code, owner_id, assigned_by) VALUES ('WH-DED', $1, 'system')", ownerA)
	if _, err := ResolveTaskOwnerQ(db.DB, tenantID, schema, "WH-DED", ownerB); code(err) != "INVENT-0491" {
		t.Fatalf("B's order at A's dedicated warehouse: want INVENT-0491, got %v", err)
	}
	if o, err := ResolveTaskOwnerQ(db.DB, tenantID, schema, "WH-DED", ""); err != nil || o != ownerA {
		t.Fatalf("an ownerless order at A's warehouse works for A: %q %v", o, err)
	}
}
