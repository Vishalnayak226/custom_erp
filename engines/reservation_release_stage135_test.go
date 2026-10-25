package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 135.6: one reservation is released once. Cancelling an order lowered
// reserved but left the row, so the sweeper lowered it again (eating another
// order's reservation); a POS authorization's rows were deleted without
// lowering reserved at all, so the units stayed reserved for good.
func TestStage135ReservationReleasedExactlyOnce(t *testing.T) {
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
	doc := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw)
	}
	doc("ITEM-RSV-A", "Item", map[string]interface{}{"code": "RSV-A", "name": "RSV-A"})
	doc("RC-CANCEL", "ReasonCode", map[string]interface{}{"code": "RC-CANCEL", "category": "Cancellation"})
	exec("INSERT INTO " + schema + ".inventory_availability (sku, location_code, on_hand, available) VALUES ('RSV-A', 'HO', 10, 10)")
	reserved := func() int {
		t.Helper()
		var n int
		if err := db.DB.QueryRow("SELECT reserved FROM " + schema + ".inventory_availability WHERE sku = 'RSV-A' AND location_code = 'HO'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	order := func(ref string, qty int) string {
		t.Helper()
		id, err := CreateSalesOrder(tenantID, SalesOrderInput{
			Channel: "RsvTest", ChannelOrderID: ref, CustomerName: "Reserve Customer",
			ShippingAddress: "1 Reserve Road, Pune 411001", PaymentStatus: "Confirmed", PreferredLocation: "HO",
			Lines: []SalesOrderLineInput{{SKU: "RSV-A", Qty: qty, UnitPrice: 100}},
		})
		if err != nil {
			t.Fatalf("create order %s: %v", ref, err)
		}
		return id
	}

	order("RSV-KEEP", 3) // the reservation an over-release would eat
	cancelled := order("RSV-GONE", 2)
	if got := reserved(); got != 5 {
		t.Fatalf("two orders reserve 5, got %d", got)
	}
	if err := CancelOrder(tenantID, cancelled, "RC-CANCEL", "desk"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := reserved(); got != 3 {
		t.Fatalf("after the cancel reserved = %d, want 3", got)
	}
	if _, err := SweepExpiredReservations(tenantID); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := reserved(); got != 3 {
		t.Fatalf("after the sweep reserved = %d, want 3 - the cancelled order was released twice", got)
	}

	hold, err := CreateReservation(tenantID, "RSV-A", "HO", 2, "POSAuthorization", 600)
	if err != nil {
		t.Fatalf("POS hold: %v", err)
	}
	if got := reserved(); got != 5 {
		t.Fatalf("with the POS hold reserved = %d, want 5", got)
	}
	releasePOSReservations(schema, []string{hold})
	if got := reserved(); got != 3 {
		t.Fatalf("after the POS release reserved = %d, want 3 - the units stayed reserved", got)
	}
	releasePOSReservations(schema, []string{hold})
	if got := reserved(); got != 3 {
		t.Fatalf("releasing a released hold again changed reserved to %d", got)
	}
}
