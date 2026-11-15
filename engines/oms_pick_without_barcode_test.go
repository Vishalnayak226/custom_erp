package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// 197.1: an item with no barcode is picked and packed by its typed SKU, several
// units in one entry, never more than the line has left; a piece's own code
// still counts one unit.
func TestPickPackWithoutBarcodeByTypedSKUAndQty(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		orderID = "TEST-NOBC-SO"
		lineID  = "TEST-NOBC-SO-L1"
		sku     = "TEST-NOBC-SKU"
		loc     = "TEST-NOBC-LOC"
	)
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id IN (SELECT id FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND data->>'order_id' = $1)", orderID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE data->>'order_id' = $1 OR data->>'sales_order_id' = $1", orderID)
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'TEST-NOBC-%'")
		db.DB.Exec("DELETE FROM "+schema+".inventory_reservation WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
	}
	cleanup()
	defer cleanup()
	seed := func(id, doctype, status string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, b, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	// No barcode on the item: the SKU (its Combination ID) is all there is.
	seed(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "No Barcode Item", "status": "Active", "standard_cost": 40})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available, reserved) VALUES ($1, $2, 10, 10, 0)", sku, loc); err != nil {
		t.Fatalf("seed stock: %v", err)
	}
	seed(orderID, "SalesOrder", "Reserved", map[string]interface{}{"code": orderID, "order_status": "Reserved", "total_amount": 300, "customer_name": "No Barcode Test"})
	seed(lineID, "SalesOrderLine", "Reserved", map[string]interface{}{"code": lineID, "order_id": orderID, "sku": sku, "qty": 3, "unit_price": 100, "location_code": loc, "line_status": "Reserved"})
	if _, err := CreateReservation(tenantID, sku, loc, 3, "Online", 0, ReservationAttribution{OrderID: orderID, LineID: lineID}); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	taskIDs, _, err := ReleaseOrderToFulfillment(tenantID, orderID, "admin")
	if err != nil || len(taskIDs) != 1 {
		t.Fatalf("release: %v %v", taskIDs, err)
	}
	taskID := taskIDs[0]

	if _, _, err := ScanPickItemQty(tenantID, taskID, sku, 0); err == nil {
		t.Fatal("qty 0 was accepted")
	}
	if _, _, err := ScanPickItemQty(tenantID, taskID, sku, 4); err == nil || !strings.Contains(err.Error(), "only 3") {
		t.Fatalf("picking 4 of 3: want 'only 3 ... left', got %v", err)
	}
	if _, n, err := ScanPickItemQty(tenantID, taskID, sku, 2); err != nil || n != 2 {
		t.Fatalf("pick 2 by typed SKU: n=%d err=%v", n, err)
	}
	if _, n, err := ScanPickItem(tenantID, taskID, sku); err != nil || n != 3 {
		t.Fatalf("one more by the scan box: n=%d err=%v", n, err)
	}
	if _, _, err := ScanPickItemQty(tenantID, taskID, sku, 1); err == nil {
		t.Fatal("a fully picked line took another unit")
	}

	if _, _, err := ScanPackItemQty(tenantID, taskID, sku, 4); err == nil || !strings.Contains(err.Error(), "only 3") {
		t.Fatalf("packing 4 of 3 picked: want 'only 3 ... picked', got %v", err)
	}
	if _, n, err := ScanPackItemQty(tenantID, taskID, sku, 3); err != nil || n != 3 {
		t.Fatalf("pack 3 by typed SKU: n=%d err=%v", n, err)
	}
	task, err := loadPickPackTask(schema, taskID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if it := task.items[0]; it.PickedQty != 3 || it.PackedQty != 3 {
		t.Fatalf("saved line: picked %d packed %d, want 3 and 3", it.PickedQty, it.PackedQty)
	}
	if err := CompletePackTask(tenantID, taskID); err != nil {
		t.Fatalf("complete pack: %v", err)
	}

	if err := checkScanQty(2, "PIECE-1"); err == nil {
		t.Fatal("a piece's own code counted 2 units")
	}
	if err := checkScanQty(1, "PIECE-1"); err != nil {
		t.Fatalf("a piece's own code, one unit: %v", err)
	}
}
