package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func step6Code(err error) string {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Code
	}
	return ""
}

// 73.B.6: an owner's order moved to a store by hand needs that owner's own
// stock there - another owner's units at the store do not count.
func TestStage73B6OwnerOnReallocation(t *testing.T) {
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
	const sku, ownerA, ownerB = "SKU-B6", "OWN-A", "OWN-B"
	if err := SetSetting(tenantID, StockOwnershipModeSetting, OwnershipMixed, "system"); err != nil {
		t.Fatalf("mixed mode: %v", err)
	}
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('ITEM-B6', 'Item', $1, 'Active', 'system')",
		fmt.Sprintf(`{"code":%q,"name":"B6 widget"}`, sku))
	for _, s := range []struct{ loc, bin string }{{"WH-A", "BA1"}, {"WH-B", "BB1"}} {
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')",
			s.loc, fmt.Sprintf(`{"code":%q,"name":%q}`, s.loc, s.loc))
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')",
			"BIN-"+s.bin, fmt.Sprintf(`{"bin_code":%q,"location":%q,"zone":"","status":"Active"}`, s.bin, s.loc))
	}
	exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, 'WH-A', 6, 6), ($1, 'WH-B', 5, 5)", sku)
	if err := PutawayToBinForOwner(tenantID, "BA1", sku, 6, ownerA, "rcv"); err != nil {
		t.Fatalf("A's putaway: %v", err)
	}
	if err := PutawayToBinForOwner(tenantID, "BB1", sku, 5, ownerB, "rcv"); err != nil {
		t.Fatalf("B's putaway: %v", err)
	}
	data, _ := json.Marshal(map[string]interface{}{"order_status": "Reserved", "channel": "Manual", "owner_id": ownerA})
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('SO-B6', 'SalesOrder', $1, 'Reserved', 'system')", data)
	line, _ := json.Marshal(map[string]interface{}{"order_id": "SO-B6", "sku": sku, "qty": 2, "line_status": "Reserved", "location_code": "WH-A"})
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('SO-B6-L1', 'SalesOrderLine', $1, 'Reserved', 'system')", line)

	if _, err := SwitchOrderFacility(tenantID, "SO-B6", "WH-B", "ops"); step6Code(err) != "ORDERM-0970" {
		t.Fatalf("A's order to WH-B (only B's stock there): want ORDERM-0970, got %v", err)
	}
}

// 73.B.10: a putaway refusal carries its own code and reason, not the
// field-format bucket.
func TestStage73B10PutawayRefusalCode(t *testing.T) {
	db.InitDB(testConnStr())
	err := PutawayToBin("default", "NO-SUCH-BIN-73B10", "SKU-73B10", 1, "ops")
	if step6Code(err) != "INVENT-0971" {
		t.Fatalf("unknown bin: want INVENT-0971, got %v", err)
	}
	if err := PutawayToBin("default", "NO-SUCH-BIN-73B10", "SKU-73B10", 0, "ops"); step6Code(err) != "INVENT-0971" {
		t.Fatalf("zero quantity: want INVENT-0971, got %v", err)
	}
}
