package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestStage14613PlainItemLeavesBinsOnDispatch: an untracked line leaves its
// bins on dispatch, so a binned warehouse never shows bin stock above on-hand.
func TestStage14613PlainItemLeavesBinsOnDispatch(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		sku  = "SKU-S14613"
		bin1 = "BIN-S14613-1"
		bin2 = "BIN-S14613-2"
		from = "WH-S14613-FROM"
		to   = "WH-S14613-TO"
		toID = "TO-S14613-1"
	)
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = 'TO-S14613-1' OR (doctype = 'Bin' AND data->>'bin_code' LIKE 'BIN-S14613-%')")
	}
	cleanup()
	defer cleanup()
	seedBinWithStock(t, schema, bin1, sku, from, 5)
	seedBinWithStock(t, schema, bin2, sku, from, 4)

	items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 6}})
	data, _ := json.Marshal(map[string]interface{}{
		"id": toID, "transfer_number": toID, "from_warehouse": from, "to_warehouse": to,
		"status": "Approved", "items": string(items),
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'TransferOrder', $2, 'Approved', 'system')", toID, data); err != nil {
		t.Fatalf("seed transfer: %v", err)
	}
	if err := DispatchTransferOrder(tenantID, toID, "system"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var onHand, binned int
	_ = db.DB.QueryRow("SELECT on_hand FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, from).Scan(&onHand)
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(qty),0) FROM "+schema+".bin_stock WHERE sku = $1 AND location_code = $2", sku, from).Scan(&binned)
	if onHand != 3 {
		t.Fatalf("on_hand want 3, got %d", onHand)
	}
	if binned != 3 {
		t.Fatalf("bins want 3 (9 binned - 6 dispatched), got %d: bin stock above on-hand", binned)
	}
	rows, err := BinStockReconciliation(tenantID, sku, from, true)
	if err != nil || len(rows) != 0 {
		t.Fatalf("reconciliation should list nothing, got %v err %v", rows, err)
	}
}
