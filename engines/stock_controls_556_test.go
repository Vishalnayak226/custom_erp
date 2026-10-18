package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestStockControlsStage556 covers 55.6's read paths on a provisioned
// throwaway client: the Cycle Count work table shows no book quantity before
// a line is reconciled (blind), and the book-vs-ledger report finds a
// location whose books were never reduced by an old transfer dispatch.
func TestStockControlsStage556(t *testing.T) {
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
	exec("INSERT INTO " + schema + ".documents (id, doctype, data, status, created_by) VALUES ('ITEM-SC-1', 'Item', '{\"code\":\"SC-1\",\"name\":\"Stock control item\"}', 'Active', 'system')")
	exec("INSERT INTO " + schema + ".inventory_availability (sku, location_code, on_hand, available) VALUES ('SC-1', 'SC-LOC', 10, 10)")
	exec("INSERT INTO " + schema + ".approval_rules (doctype, min_amount, max_amount, required_role) VALUES ('CycleCountLine', 0, NULL, 'Store Manager') ON CONFLICT (doctype, min_amount) DO NOTHING")

	// Blind until reconciled.
	data, _ := json.Marshal(map[string]interface{}{"count_session": "SC-S", "location": "SC-LOC", "sku": "SC-1", "counted_qty": 7, "status": "Draft"})
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('SC-L1', 'CycleCountLine', $1, 'Draft', 'counter')", data)
	lines, err := CycleCountSessionLines(tenantID, "SC-S")
	if err != nil || len(lines) != 1 || lines[0].SystemQty != nil || lines[0].Variance != nil || lines[0].ItemName != "Stock control item" {
		t.Fatalf("before reconcile: %v %+v", err, lines)
	}
	if _, err := ReconcileCycleCountDetailed(tenantID, "SC-S", "counter", "Store Manager"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	lines, _ = CycleCountSessionLines(tenantID, "SC-S")
	if lines[0].SystemQty == nil || *lines[0].SystemQty != 10 || *lines[0].Variance != -3 || lines[0].Status != "Pending Approval" {
		t.Fatalf("after reconcile: %+v", lines[0])
	}
	newID, err := RequestRecount(tenantID, "SC-L1", "manager")
	if err != nil {
		t.Fatalf("recount: %v", err)
	}
	lines, _ = CycleCountSessionLines(tenantID, "SC-S")
	for _, l := range lines {
		if l.ID == newID && (l.CountedQty != nil || l.FirstCountQty != nil || l.SystemQty != nil) {
			t.Fatalf("the recount line must be blind: %+v", l)
		}
		if l.ID == "SC-L1" && l.RecountLineID != newID {
			t.Fatalf("the original line should name its recount: %+v", l)
		}
	}

	// Book vs ledger: 10 on the books, but the ledger says 12 received and 5
	// dispatched (the old transfer that never left the books) = 7.
	ledger := func(id string, qty float64, voucher string) {
		b, _ := json.Marshal(map[string]interface{}{"item_id": "SC-1", "warehouse_id": "SC-LOC", "qty": qty, "voucher_type": voucher})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'StockLedgerEntry', $2, 'Active', 'system')", id, b)
	}
	ledger("SC-LE1", 12, "GRN")
	ledger("SC-LE2", -5, "TransferOrder")
	ledger("SC-LE3", -2, "ConditionChange") // an available-bucket move: not counted
	rows, err := StockBookVsLedger(tenantID, "SC-1", "", true)
	if err != nil || len(rows) != 1 || rows[0].OnHand != 10 || rows[0].LedgerSum != 7 || rows[0].Difference != 3 || rows[0].Lines != 2 {
		t.Fatalf("drift: %v %+v", err, rows)
	}
	ledger("SC-LE4", 3, "CycleCount")
	if rows, _ := StockBookVsLedger(tenantID, "SC-1", "", true); len(rows) != 0 {
		t.Fatalf("after a counted correction the row agrees, got %+v", rows)
	}
}
