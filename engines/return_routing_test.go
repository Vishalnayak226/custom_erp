package engines

import (
	"strings"
	"testing"
)

// Stage 140.14.6: a return with the store left blank routes to the nearest
// active location by PIN with no stock condition, falls back to the store that
// shipped the order, and names the cause when neither works. Order sourcing
// keeps its stock condition.
func TestStage140146ReturnRoutesWithoutStockCondition(t *testing.T) {
	_, schema, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	insert("RR-A", "Location", "Active", map[string]interface{}{"code": "RR-A", "name": "A", "type": "Store", "status": "Active", "pincode": "560001"})
	insert("RR-B", "Location", "Active", map[string]interface{}{"code": "RR-B", "name": "B", "type": "Store", "status": "Active", "pincode": "110001"})
	insert("RR-C", "Location", "Inactive", map[string]interface{}{"code": "RR-C", "name": "C", "type": "Store", "status": "Inactive", "pincode": "560002"})
	insert("RR-NOPIN", "Location", "Active", map[string]interface{}{"code": "RR-NOPIN", "name": "N", "type": "Store", "status": "Active"})

	// No stock anywhere: the nearest active store by PIN still wins; an inactive one never does.
	loc, cause := routeReturnLocation(schema, "560002", "")
	if loc != "RR-A" || cause != "" {
		t.Fatalf("nearest active by PIN: got %q cause %q", loc, cause)
	}

	// Order sourcing is unchanged: it still needs stock, so with none it does not route.
	items := []map[string]interface{}{{"sku": "RR-SKU", "qty": 1}}
	if got, ok, err := singleLocationNearestPincode(schema, "560002", items, nil); err != nil || ok || got != "" {
		t.Fatalf("order sourcing must keep its stock condition: %q %v %v", got, ok, err)
	}

	// No PIN: the store that shipped the order (its fulfilment task), by either order id.
	insert("TSK-RR1", "FulfillmentTask", "Dispatched", map[string]interface{}{"code": "TSK-RR1", "order_id": "SO-RR1", "location_code": "RR-B", "status": "Dispatched"})
	if loc, cause = routeReturnLocation(schema, "", "SO-RR1"); loc != "RR-B" || cause != "" {
		t.Fatalf("shipping store fallback: got %q cause %q", loc, cause)
	}
	// A till bill carries its store on itself.
	insert("CART-RR1", "POSCart", "Completed", map[string]interface{}{"code": "CART-RR1", "location": "RR-A"})
	if loc, cause = routeReturnLocation(schema, "", "CART-RR1"); loc != "RR-A" || cause != "" {
		t.Fatalf("cart store fallback: got %q cause %q", loc, cause)
	}
	// A PIN no active location can serve falls back to the shipping store.
	if loc, _ = routeReturnLocation(schema, "999999", "SO-RR1"); loc == "" {
		t.Fatalf("PIN present: still routes")
	}

	// Neither works: the cause is named.
	if loc, cause = routeReturnLocation(schema, "", "SO-NONE"); loc != "" || !strings.Contains(cause, "shipping") {
		t.Fatalf("cause: got %q %q", loc, cause)
	}
	if loc, cause = routeReturnLocation(schema, "", ""); loc != "" || cause == "" {
		t.Fatalf("no order, no PIN: %q %q", loc, cause)
	}
}
