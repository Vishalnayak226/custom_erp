package server

import (
	"custom_erp/db"
	"fmt"
	"net/http"
	"testing"
)

// TestStage555ShortageRefusalThenCorrectedCartCompletes: the till pins one
// cart number per cart and sends it as the checkout's key. A cart refused for
// a stock shortage is corrected by the cashier (the line lowered or removed),
// so the corrected cart must complete under the same cart number - it used to
// be answered "already used for a different sale" (409) and could not be sold
// without a reset. A completed sale's key still refuses a different cart.
func TestStage555ShortageRefusalThenCorrectedCartCompletes(t *testing.T) {
	f := newPOSPricingFixture(t, 250)
	cart := "P555SHORT-" + f.suffix
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE idempotency_key LIKE $1`, f.schema), "%"+cart)
	})
	sale := func(qty int) (int, map[string]interface{}) {
		return f.checkout(f.cashierToken, map[string]interface{}{
			"cart_number": cart, "location": f.location, "payment_mode": "Cash",
			"items": []map[string]interface{}{{"sku": f.sku, "qty": qty}},
		})
	}

	if status, resp := sale(600); status != http.StatusUnprocessableEntity || resp["code"] != "INVENT-0101" {
		t.Fatalf("600 of 500 in stock must be refused as a shortage, got %d %v", status, resp)
	}
	if status, resp := sale(2); status != http.StatusOK || resp["status"] != "completed" {
		t.Fatalf("the corrected cart must complete under the same cart number, got %d %v", status, resp)
	}
	if status, resp := sale(3); status != http.StatusConflict {
		t.Fatalf("a different cart under a completed sale's number must still be refused, got %d %v", status, resp)
	}
}
