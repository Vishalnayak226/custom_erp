package engines

import "testing"

// Stage 73.4.2.2: orders taken for stock not in hand wait as
// ALLOCATION_FAILED; when stock lands the sweep reserves the oldest first and
// leaves the rest waiting.
func TestStage73422ReleaseBackordersOldestFirst(t *testing.T) {
	f := newCreditFixture(t) // 100 of ITEM-CC at HO
	first := f.order("Asha", "", 120)
	second := f.order("Bala", "", 120)
	for _, id := range []string{first, second} {
		if s, h := f.field("SalesOrder", id, "order_status"), f.field("SalesOrder", id, "hold_reason"); s != "On Hold" || h != HoldAllocationFailed {
			t.Fatalf("%s: want On Hold/%s, got %s/%s", id, HoldAllocationFailed, s, h)
		}
	}

	// Nothing landed yet: both still wait.
	if rel, wait, err := ReleaseBackorders(f.tenantID, "system"); err != nil || rel != 0 || wait != 2 {
		t.Fatalf("before stock: released %d waiting %d err %v", rel, wait, err)
	}

	// 50 more lands: 150 covers the older order only.
	if _, err := PostInventoryLedger(f.tenantID, "HO", []interface{}{map[string]interface{}{"sku": "ITEM-CC", "qty": 50.0}}, false); err != nil {
		t.Fatal(err)
	}
	rel, wait, err := ReleaseBackorders(f.tenantID, "system")
	if err != nil || rel != 1 || wait != 1 {
		t.Fatalf("after stock: released %d waiting %d err %v", rel, wait, err)
	}
	if s := f.field("SalesOrder", first, "order_status"); s != "Reserved" {
		t.Fatalf("oldest order: want Reserved, got %s", s)
	}
	if s := f.field("SalesOrder", second, "order_status"); s != "On Hold" {
		t.Fatalf("newer order: want On Hold, got %s", s)
	}
}
