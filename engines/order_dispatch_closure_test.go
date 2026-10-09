package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// A task dispatched straight from the Fulfillment screen (no courier
// manifest) must still close its order: Shipped, with a draft invoice.
func TestDispatchFromFulfillmentClosesOrder(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const orderID, lineID, sku, loc = "TEST-DSP-SO", "TEST-DSP-SO-L1", "TEST-DSP-SKU", "TEST-DSP-LOC"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id IN (SELECT id FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND data->>'order_id' = $1)", orderID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE data->>'order_id' = $1 OR data->>'sales_order_id' = $1", orderID)
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'TEST-DSP-%'")
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
	seed(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "Dispatch Test", "status": "Active", "standard_cost": 10})
	db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available, reserved) VALUES ($1, $2, 5, 5, 0)", sku, loc)
	seed(orderID, "SalesOrder", "Reserved", map[string]interface{}{"code": orderID, "order_status": "Reserved", "total_amount": 50})
	seed(lineID, "SalesOrderLine", "Reserved", map[string]interface{}{"order_id": orderID, "sku": sku, "qty": 1, "unit_price": 50, "location_code": loc, "line_status": "Reserved"})
	if _, err := CreateReservation(tenantID, sku, loc, 1, "Online", 0, ReservationAttribution{OrderID: orderID, LineID: lineID}); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	taskIDs, _, err := ReleaseOrderToFulfillment(tenantID, orderID, "admin")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	for _, st := range []string{"Picking", "Packed", "Dispatched"} {
		if err := TransitionTaskStatus(tenantID, taskIDs[0], st); err != nil {
			t.Fatalf("%s: %v", st, err)
		}
	}
	if err := EvaluateTaskOrderShipment(tenantID, taskIDs[0], "admin"); err != nil {
		t.Fatalf("closure: %v", err)
	}
	if status, _, _ := GetOrderStatus(tenantID, orderID); status != "Shipped" {
		t.Fatalf("order status %q after a Fulfillment-screen dispatch, want Shipped", status)
	}
	var invoices int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'SalesInvoice' AND data->>'sales_order_id' = $1", orderID).Scan(&invoices)
	if invoices != 1 {
		t.Fatalf("expected one draft invoice, got %d", invoices)
	}
}
