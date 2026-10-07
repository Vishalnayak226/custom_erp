package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// FA-20261005-03: a Reserved order had no way to become warehouse work.
func TestReleaseOrderToFulfillment(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const orderID = "TEST-REL-SO"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id LIKE 'TEST-REL-%' OR (doctype = 'FulfillmentTask' AND data->>'order_id' LIKE 'TEST-REL-%')")
	}
	cleanup()
	defer cleanup()

	seed := func(id, doctype, status string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, b, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed(orderID, "SalesOrder", "Reserved", map[string]interface{}{"code": orderID, "order_status": "Reserved"})
	seed(orderID+"-L1", "SalesOrderLine", "Reserved", map[string]interface{}{"order_id": orderID, "sku": "SKU-A", "qty": 2, "location_code": "LOC-A", "line_status": "Reserved"})
	seed(orderID+"-L2", "SalesOrderLine", "Reserved", map[string]interface{}{"order_id": orderID, "sku": "SKU-B", "qty": 1, "location_code": "LOC-B", "line_status": "Reserved"})
	seed(orderID+"-L3", "SalesOrderLine", "On Hold", map[string]interface{}{"order_id": orderID, "sku": "SKU-C", "qty": 1, "location_code": "LOC-A", "line_status": "On Hold"})

	ids, created, err := ReleaseOrderToFulfillment(tenantID, orderID, "admin")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if !created || len(ids) != 2 {
		t.Fatalf("expected 2 new tasks (one per location), got created=%v ids=%v", created, ids)
	}
	var items string
	db.DB.QueryRow("SELECT data->>'items' FROM "+schema+".documents WHERE id = $1", ids[0]).Scan(&items)
	var parsed []map[string]interface{}
	json.Unmarshal([]byte(items), &parsed)
	if len(parsed) != 1 || parsed[0]["sku"] != "SKU-A" {
		t.Fatalf("LOC-A task should carry only the reserved SKU-A line (held line excluded), got %s", items)
	}
	if status, _, _ := GetOrderStatus(tenantID, orderID); status != OrderStatusReleased {
		t.Fatalf("expected order Released, got %s", status)
	}

	again, created, err := ReleaseOrderToFulfillment(tenantID, orderID, "admin")
	if err != nil || created || len(again) != 2 {
		t.Fatalf("a repeat release must return the same tasks and create none: created=%v ids=%v err=%v", created, again, err)
	}

	if err := orderMutationAllowed(tenantID, OrderStatusReleased, "Split"); err == nil {
		t.Fatalf("Split must be blocked on a released order")
	}
	if err := orderMutationAllowed(tenantID, OrderStatusReleased, "Set Priority"); err != nil {
		t.Fatalf("Set Priority must stay allowed on a released order: %v", err)
	}
	if blocked, _ := isCancellationBlocked(tenantID, OrderStatusReleased); !blocked {
		t.Fatalf("cancellation must be blocked once released")
	}

	seed("TEST-REL-SO2", "SalesOrder", "On Hold", map[string]interface{}{"code": "TEST-REL-SO2", "order_status": "On Hold"})
	if _, _, err := ReleaseOrderToFulfillment(tenantID, "TEST-REL-SO2", "admin"); err == nil {
		t.Fatalf("an On Hold order must not be released")
	}
}
