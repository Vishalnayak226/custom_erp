package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// Stage 47.0.1 / audit finding A-03 ("Failed checkout can leave stock posted
// and can deduct it again on retry" -
// docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md lines 85-91).
//
// Exact mechanism (engines/pos_checkout.go, FinalizePOSCheckout):
// PostInventoryLedgerWithVoucher commits the availability decrement in its
// own transaction (inventory.go:283-293) BEFORE loyalty redemption or any
// finance/GST posting runs. If a later step fails, FinalizePOSCheckout's
// markFailed() flips the cart to 'Failed' - but never reverses the
// already-committed availability decrement. handlers_pim_pos_finance.go's
// checkout-claim query explicitly allows re-claiming a 'Failed' cart
// (`WHERE %s.documents.status = 'Failed'`, line 554), so the identical
// cart_number can be resubmitted and FinalizePOSCheckout runs again end to
// end - decrementing availability a second time. The only idempotency guard
// in the whole path is the StockLedgerEntry's idempotency_key
// (inventory.go:87-96, keyed on voucher_type:voucher_id:location:sku), which
// silently no-ops the SECOND ledger row - it protects the evidence, not the
// mutation, exactly as the audit states.
//
// This test forces the failure deterministically via loyalty redemption
// (RedeemLoyaltyPoints rejects points > balance, engines/loyalty.go:106) on
// a customer with a zero balance, so the same failure is 100% reproducible
// on both the "original" and "retry" call without needing a real network
// race - matching the audit's "forced-failure tests after each boundary"
// requirement text.
//
// This test asserts the SECURE/CORRECT outcome - a retried checkout cannot
// mutate stock a second time.
//
// CLOSED by Stage 47.3.2 (2026-09-06) and PROMOTED out of the stage47redteam
// build tag per 47.0.1's own closure note, so it now runs on every ordinary
// `go test ./...`. What closes it: FinalizePOSCheckout runs the availability
// decrement, the loyalty burn, revenue/COGS, GST and the exempt reclass in ONE
// transaction, so the forced loyalty failure below rolls the decrement back
// instead of leaving it committed.
//
// The original assertion is unchanged and still has teeth (a retry must not
// decrement further). The SANITY CHECK above it was rewritten: it used to
// require availability to have DROPPED after a failed attempt, because that
// was the bug - it encoded the vulnerable intermediate state as a
// precondition. Asserting the fixed behaviour there (a failed sale changes
// nothing) is strictly stronger: it now fails if the transaction is ever
// broken back apart, which the old wording could not detect.
func TestA03FailedCheckoutRetryDoubleDecrementsAvailability(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("failed to resolve tenant schema: %v", err)
	}

	sku := NewDocID("A03SKU")
	location := NewDocID("A03LOC")
	cartNumber := NewDocID("A03CART")
	fakeCustomer := NewDocID("A03CUST") // never earned a point - GetLoyaltyBalance is 0
	const startingAvailable = 100
	const saleQty = 5

	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)`, schema),
		sku, location, startingAvailable); err != nil {
		t.Fatalf("failed to seed inventory: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), sku, location)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'POSCart'`, schema), cartNumber)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1`, schema), cartNumber)
	})

	// redeem_points (999999) deliberately exceeds fakeCustomer's real balance
	// (0), so RedeemLoyaltyPoints rejects it deterministically on EVERY call -
	// this is the forced failure point, reached only after the inventory
	// decrement above it in FinalizePOSCheckout has already committed.
	cartData, _ := json.Marshal(map[string]interface{}{
		"location":      location,
		"payment_mode":  "Cash",
		"customer_id":   fakeCustomer,
		"redeem_points": 999999,
		"items": []map[string]interface{}{
			{"sku": sku, "qty": saleQty, "sale_price": 100.0, "cost_price": 60.0},
		},
	})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Processing', 'system')`, schema),
		cartNumber, cartData); err != nil {
		t.Fatalf("failed to seed cart: %v", err)
	}

	availability := func() int {
		var a int
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT available FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema),
			sku, location).Scan(&a); err != nil {
			t.Fatalf("failed to read availability: %v", err)
		}
		return a
	}
	ledgerRowCount := func() int {
		var n int
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1`, schema),
			cartNumber).Scan(&n); err != nil {
			t.Fatalf("failed to count ledger rows: %v", err)
		}
		return n
	}

	// "Original" attempt.
	if _, _, err := FinalizePOSCheckout(tenantID, cartNumber, ""); err == nil {
		t.Fatalf("sanity check failed: expected the first FinalizePOSCheckout call to fail at loyalty redemption (fixture assumption broken - fakeCustomer must have a real 0 balance)")
	}
	afterFirst := availability()
	if afterFirst != startingAvailable {
		t.Fatalf("A-03: a checkout that FAILED at loyalty redemption still moved stock (%d -> %d). 47.3.2 puts the availability decrement in the same transaction as the loyalty burn and every GL posting, so a failure at any of them must leave availability exactly as it was.", startingAvailable, afterFirst)
	}

	// The "retry" - handlers_pim_pos_finance.go's own cart-claim query
	// explicitly permits re-claiming a cart left in 'Failed' by a prior
	// attempt, so this is a faithful stand-in for a cashier/network retry
	// against the identical cart_number.
	if _, _, err := FinalizePOSCheckout(tenantID, cartNumber, ""); err == nil {
		t.Fatalf("sanity check failed: expected the retried FinalizePOSCheckout call to also fail at loyalty redemption")
	}
	afterRetry := availability()
	ledgerRows := ledgerRowCount()

	if afterRetry != startingAvailable {
		t.Fatalf("A-03: after two failed attempts on cart %q, availability is %d instead of the untouched %d - a failed sale is leaking stock.", cartNumber, afterRetry, startingAvailable)
	}
	if afterRetry < afterFirst {
		t.Fatalf("A-03: retrying a failed checkout for cart %q decremented available stock a SECOND time (%d -> %d, a further -%d) while the stock ledger recorded only %d row(s) for this voucher (idempotency_key silently swallowed the duplicate) - the retry mutated real stock a second time with no corresponding evidence trail. Must not happen once 47.3 makes the inventory mutation part of the same idempotent transaction/outcome as the rest of checkout.",
			cartNumber, afterFirst, afterRetry, afterFirst-afterRetry, ledgerRows)
	}
}
