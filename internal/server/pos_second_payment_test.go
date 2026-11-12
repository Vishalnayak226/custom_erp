package server

import (
	"fmt"
	"net/http"
	"testing"

	"custom_erp/engines"
)

// 49.5.4: the cashier's "yes, this is a second payment" has to reach the check.
// The refusal alone is tested in engines; this is the retry through the real route.
func TestStage4954TillRetryAfterSecondPaymentAnswer(t *testing.T) {
	f := newPOSPricingFixture(t, 1000)
	ref := "SLIP-" + f.suffix
	sale := func(cart string, extra map[string]interface{}) (int, map[string]interface{}) {
		body := map[string]interface{}{
			"cart_number": cart, "location": f.location, "payment_mode": "Card",
			"items": []map[string]interface{}{{"sku": f.sku, "qty": 1}},
		}
		for k, v := range extra {
			body[k] = v
		}
		return f.checkout(f.cashierToken, body)
	}
	refused := func(status int, resp map[string]interface{}) {
		t.Helper()
		if status == http.StatusOK || resp["code"] != engines.DuplicateTenderCode {
			t.Fatalf("want %s, got %d %v", engines.DuplicateTenderCode, status, resp)
		}
	}
	went := func(status int, resp map[string]interface{}) {
		t.Helper()
		if status != http.StatusOK || resp["status"] != "completed" {
			t.Fatalf("the answered retry must complete, got %d %v", status, resp)
		}
	}

	single := map[string]interface{}{"payment_reference": ref}
	first := fmt.Sprintf("P4954-%s-1", f.suffix)
	went(sale(first, single))
	second := fmt.Sprintf("P4954-%s-2", f.suffix)
	refused(sale(second, single))
	went(sale(second, map[string]interface{}{"payment_reference": ref, "second_payment": true}))
	if f.storedCart(second)["second_payment"] != true {
		t.Fatal("the stored cart must keep the cashier's answer")
	}

	// A split part: its own flag changes the request, and must still be accepted.
	split := func(answered bool) map[string]interface{} {
		part := map[string]interface{}{"mode": "Card", "amount": 400, "reference": ref + "-S"}
		if answered {
			part["second_payment"] = true
		}
		return map[string]interface{}{"payment_mode": "Cash", "tenders": []map[string]interface{}{part}}
	}
	went(sale(fmt.Sprintf("P4954-%s-3", f.suffix), split(false)))
	fourth := fmt.Sprintf("P4954-%s-4", f.suffix)
	refused(sale(fourth, split(false)))
	went(sale(fourth, split(true)))
}
