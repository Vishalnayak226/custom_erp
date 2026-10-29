package server

import (
	"net/http"
	"testing"
)

// Stage 49.5.7: a second tab submits a cart number that is already Paid under
// its own idempotency key - so the command claim is new and the cart-row
// claim answers. It must sell nothing more and report the total that was
// actually charged, paise included (it reported int(99.50) x 2 = 198).
func TestStage4957SecondTabReplayReportsTheChargedTotal(t *testing.T) {
	f := newPOSPricingFixture(t, 99.50)
	cart := "P4957TAB-" + f.suffix
	body := func(key string) map[string]interface{} {
		b := map[string]interface{}{"cart_number": cart, "location": f.location, "payment_mode": "Cash",
			"items": []map[string]interface{}{{"sku": f.sku, "qty": 2}}}
		if key != "" {
			b["idempotency_key"] = key
		}
		return b
	}
	before := f.availabilityFor(f.sku)
	status, first := f.checkout(f.cashierToken, body(""))
	if status != http.StatusOK || first["status"] != "completed" {
		t.Fatalf("first checkout (%d): %v", status, first)
	}
	charged, _ := first["sale_total"].(float64)
	if charged != 199 {
		t.Fatalf("first checkout charged %v, want 199", first["sale_total"])
	}

	status, second := f.checkout(f.cashierToken, body("second-tab-"+f.suffix))
	if status != http.StatusOK || second["status"] != "completed" {
		t.Fatalf("second tab (%d): %v", status, second)
	}
	if got, _ := second["sale_total"].(float64); got != charged {
		t.Errorf("the second tab was told %v, but %v was charged", second["sale_total"], charged)
	}
	if got := f.availabilityFor(f.sku); got != before-2 {
		t.Errorf("availability %d -> %d, want one deduction of 2", before, got)
	}
	if rec := f.reconcile(cart); rec.Verdict != "Balanced" {
		t.Errorf("the sale does not reconcile: %s - %s", rec.Verdict, rec.Detail)
	}
}
