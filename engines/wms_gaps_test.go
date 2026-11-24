package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/internal/testfault"
)

// 250.1: a wave pick confirm is all or nothing. With the second order's save
// failing, the first order must not be picked and the line must not be kept as
// confirmed; the next confirm then picks both orders. Before 250.1 the row and
// the first order committed on their own, and the retry returned OK without
// picking the second order.
func TestWavePickConfirmIsAllOrNothing(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const loc, wave, sku = "WH250", "WAVE-250", "TEE250"
	insert(loc, "Location", "Active", "system", map[string]interface{}{"code": loc, "name": "WH 250", "type": "Warehouse"})
	insert("BIN-250", "Bin", "Active", "system", map[string]interface{}{"code": "BIN-250", "location": loc})
	insert(sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Tee"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 5.0}}, false); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"FT-250A", "FT-250B"} {
		insert(id, "FulfillmentTask", "Pending", "system", map[string]interface{}{"location_code": loc, "wave_id": wave,
			"items": []interface{}{map[string]interface{}{"sku": sku, "qty": 2}}})
	}
	picked := func(id string) int {
		t.Helper()
		task, err := loadPickPackTask(schema, id)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, it := range task.items {
			n += it.PickedQty
		}
		return n
	}
	key := WavePickLineKey(sku, "BIN-250", "")

	disarm := testfault.Arm(t, db.DB, schema, testfault.Step{Label: "second-order", Table: "documents", Doctype: "FulfillmentTask", On: "UPDATE"}, "FT-250B")
	err := ConfirmWavePickLine(tenantID, wave, key, 4, "picker1")
	if err == nil || !strings.Contains(err.Error(), "second-order") {
		t.Fatalf("confirm with the second order failing returned %v, want the injected failure", err)
	}
	if a := picked("FT-250A"); a != 0 {
		t.Fatalf("after the failed confirm the first order has %d picked, want 0 - the confirm is not all or nothing", a)
	}
	if c, _ := WavePickConfirmations(tenantID, wave); c[key] != "" {
		t.Fatalf("after the failed confirm the line is kept as confirmed by %q, want no row", c[key])
	}
	disarm()

	if err := ConfirmWavePickLine(tenantID, wave, key, 4, "picker1"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if a, b := picked("FT-250A"), picked("FT-250B"); a != 2 || b != 2 {
		t.Fatalf("after the retry the orders have %d and %d picked, want 2 and 2", a, b)
	}
	if c, _ := WavePickConfirmations(tenantID, wave); c[key] != "picker1" {
		t.Fatalf("after the retry the line is confirmed by %q, want picker1", c[key])
	}
	// A repeat is still a no-op: nothing more is picked.
	if err := ConfirmWavePickLine(tenantID, wave, key, 4, "picker2"); err != nil {
		t.Fatal(err)
	}
	if a, b := picked("FT-250A"), picked("FT-250B"); a != 2 || b != 2 {
		t.Fatalf("a repeat confirm changed the picks to %d and %d", a, b)
	}
}
