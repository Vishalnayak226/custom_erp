package server

// Stage 47.3.5 - "Add failure injection after every boundary (availability,
// ledger, loyalty, GST, each GL side, outbox, response loss), process
// kill/restart and 100 identical/concurrent retries." (audit A-03)
//
// Every test here asserts the item's acceptance line directly: "every injected
// failure produces either zero result or one complete, explainable result; no
// failed/retried cart can double-decrement stock; all financial entries balance
// and reconcile to the receipt."
//
// The reconciliation check is engines.ReconcileSale (47.3.6) rather than a
// bespoke assertion per test, so the test and the report a real operator runs
// agree on what "balanced" means. A test that passed while the report called
// the same sale broken would be worth nothing.

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// availabilityFor reads the live availability the sale is supposed to move.
func (f *posPricingFixture) availabilityFor(sku string) int {
	f.t.Helper()
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT available FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, f.schema),
		sku, f.location).Scan(&n); err != nil {
		f.t.Fatalf("failed to read availability for %s: %v", sku, err)
	}
	return n
}

func (f *posPricingFixture) reconcile(cartNumber string) *engines.SaleReconciliationRow {
	f.t.Helper()
	row, err := engines.ReconcileSale("default", cartNumber)
	if err != nil {
		f.t.Fatalf("reconciliation of %s failed: %v", cartNumber, err)
	}
	return row
}

// TestStage473CompletedSaleReconciles is the baseline the failure tests are
// measured against: a sale that works must balance across every ledger.
func TestStage473CompletedSaleReconciles(t *testing.T) {
	f := newPOSPricingFixture(t, 500)
	before := f.availabilityFor(f.sku)
	cart := "P473OK-" + f.suffix

	status, resp := f.checkout(f.cashierToken, map[string]interface{}{
		"cart_number": cart, "location": f.location, "payment_mode": "Cash",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 3}},
	})
	if status != http.StatusOK || resp["status"] != "completed" {
		t.Fatalf("checkout failed (%d): %v", status, resp)
	}
	if got := f.availabilityFor(f.sku); got != before-3 {
		t.Errorf("availability %d -> %d, want a decrement of exactly 3", before, got)
	}
	rec := f.reconcile(cart)
	if rec.Verdict != "Balanced" {
		t.Fatalf("a clean sale did not reconcile: %s - %s", rec.Verdict, rec.Detail)
	}
	if rec.LedgerQty != 3 {
		t.Errorf("stock ledger recorded %d unit(s), want 3", rec.LedgerQty)
	}
	if !rec.OutboxPublished {
		t.Error("no sale event reached the outbox, so a downstream consumer would never learn about this sale")
	}
}

// TestStage473FailureAfterStockLeavesNothingPosted is the failure-injection
// case that matters most, and the exact shape of A-03: the availability
// decrement succeeds and a LATER step fails.
//
// The injected failure is a loyalty redemption the customer cannot cover -
// deterministic, reachable through the ordinary API, and positioned after the
// stock decrement inside FinalizePOSCheckout, which is precisely where the
// pre-47.3 code committed and then could not unwind.
func TestStage473FailureAfterStockLeavesNothingPosted(t *testing.T) {
	f := newPOSPricingFixture(t, 500)
	customer := "P473CUST-" + f.suffix
	seedPOSCustomer(t, f.schema, customer)
	before := f.availabilityFor(f.sku)
	cart := "P473FAIL-" + f.suffix

	// The pre-check in handleCheckout rejects an over-redemption before the
	// cart is even claimed, which is correct behaviour but tests nothing about
	// atomicity. Earning a point first gets past that pre-check and puts the
	// failure where it belongs: inside the transaction, after the decrement.
	if err := engines.EarnLoyaltyPoints("default", customer, 100000, "P473SEED-"+f.suffix); err != nil {
		t.Fatalf("failed to seed a loyalty balance: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.loyalty_point_ledger WHERE customer_id = $1`, f.schema), customer)
	})
	balance, _ := engines.GetLoyaltyBalance("default", customer)
	if balance <= 0 {
		t.Skip("the tenant's loyalty earn rate produced no points for this fixture; cannot position the failure")
	}
	// Burn the balance out from under the sale AFTER the handler's pre-check
	// has read it is not something an HTTP test can time reliably, so the
	// failure is injected at the engine boundary instead - the same boundary,
	// reached the same way, with the transaction already open past the stock
	// decrement.
	cartData := fmt.Sprintf(`{"location":%q,"payment_mode":"Cash","customer_id":%q,"redeem_points":%d,
		"items":[{"sku":%q,"qty":2,"sale_price":500}]}`, f.location, customer, balance+50_000, f.sku)
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2::jsonb, 'Processing', 'system')`, f.schema),
		cart, cartData); err != nil {
		t.Fatalf("failed to seed the cart: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1`, f.schema), cart)
	})

	if _, _, err := engines.FinalizePOSCheckout("default", cart, ""); err == nil {
		t.Fatal("the injected loyalty failure did not fail the sale - the fixture assumption is broken")
	}

	if got := f.availabilityFor(f.sku); got != before {
		t.Errorf("a sale that FAILED after the stock decrement left availability at %d instead of the untouched %d - the decrement did not roll back with the rest of the transaction", got, before)
	}
	rec := f.reconcile(cart)
	if rec.Verdict == "BROKEN" {
		t.Errorf("the failed sale left the ledgers inconsistent: %s", rec.Detail)
	}
	if rec.RevenuePosted != 0 || rec.COGSPosted != 0 || rec.LedgerQty != 0 {
		t.Errorf("a failed sale posted revenue %.2f / COGS %.2f / %d ledger unit(s); it must post nothing at all",
			rec.RevenuePosted, rec.COGSPosted, rec.LedgerQty)
	}

	// ...and retrying it fails the same way, without compounding anything.
	if _, _, err := engines.FinalizePOSCheckout("default", cart, ""); err == nil {
		t.Fatal("the retry unexpectedly succeeded")
	}
	if got := f.availabilityFor(f.sku); got != before {
		t.Errorf("retrying the failed sale moved stock (%d, want %d) - a retry must not compound a failure", got, before)
	}
}

// TestStage473RepeatedIdenticalRequestsPostExactlyOneSale is the item's "100
// identical retries" case, run through the real HTTP handler with the same
// idempotency key - which is what a till does when the response is lost.
func TestStage473RepeatedIdenticalRequestsPostExactlyOneSale(t *testing.T) {
	f := newPOSPricingFixture(t, 250)
	before := f.availabilityFor(f.sku)
	key := "P473IDEM-" + f.suffix

	const attempts = 100
	replayed, rateLimited := 0, 0
	for i := 0; i < attempts; i++ {
		// A NEW cart number every time - deliberately. The pre-47.3 guard was
		// the cart-number claim, so reusing one would test the old mechanism
		// rather than the new one. The idempotency key is what must hold here,
		// and it does because an explicit key excludes the cart number from
		// the request digest (see handleCheckout's own note on why).
		cart := fmt.Sprintf("P473IDEM-%s-%d", f.suffix, i)
		status, resp := f.checkout(f.cashierToken, map[string]interface{}{
			"cart_number": cart, "idempotency_key": key,
			"location": f.location, "payment_mode": "Cash",
			"items": []map[string]interface{}{{"sku": f.sku, "qty": 1}},
		})
		// Stage 24's per-session rate limiter (60 req/min) cuts in partway
		// through a burst this size. That is a refusal, not a sale, and it is
		// the correct answer to a till hammering the endpoint - so it is
		// counted rather than treated as a failure. What must hold either way
		// is that nothing beyond the first attempt POSTED anything.
		if status == http.StatusTooManyRequests {
			rateLimited++
			continue
		}
		if status != http.StatusOK {
			t.Fatalf("attempt %d returned HTTP %d: %v", i, status, resp)
		}
		if resp["status"] != "completed" {
			t.Fatalf("attempt %d returned status %v, want completed (a replay must return the ORIGINAL outcome, not an error)", i, resp["status"])
		}
		if i > 0 {
			replayed++
			if resp["cart_number"] != fmt.Sprintf("P473IDEM-%s-0", f.suffix) {
				t.Fatalf("attempt %d replayed cart %v; a duplicate must be answered with the ORIGINAL sale, not its own cart number", i, resp["cart_number"])
			}
		}
	}
	if replayed < 10 {
		t.Fatalf("only %d attempt(s) reached the idempotency check before the rate limiter took over (%d rate-limited); the test needs to actually exercise replay", replayed, rateLimited)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE idempotency_key LIKE $1`, f.schema), "%"+key)
	})

	if got := f.availabilityFor(f.sku); got != before-1 {
		t.Fatalf("%d identical submissions (1 real + %d replays) moved availability %d -> %d; exactly ONE unit must have been sold",
			attempts, replayed, before, got)
	}
	rec := f.reconcile(fmt.Sprintf("P473IDEM-%s-0", f.suffix))
	if rec.Verdict != "Balanced" {
		t.Errorf("the one real sale does not reconcile after %d replays: %s", replayed, rec.Detail)
	}
}

// TestStage473ConcurrentTillsSellingTheSameSKU is 47.3.4's case: two tills,
// one SKU, deterministic lock order. The invariant is not "both succeed" or
// "one fails" - it is that stock never goes below what was there, and every
// sale that reports success actually posted.
func TestStage473ConcurrentTillsSellingTheSameSKU(t *testing.T) {
	f := newPOSPricingFixture(t, 100)
	// Trim availability so the concurrency has something to contend over.
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.inventory_availability SET available = 10, on_hand = 10 WHERE sku = $1 AND location_code = $2`, f.schema),
		f.sku, f.location); err != nil {
		t.Fatalf("failed to set availability: %v", err)
	}

	const tills = 8
	const qtyEach = 2
	type result struct {
		status int
		body   map[string]interface{}
		cart   string
	}
	results := make([]result, tills)
	var wg sync.WaitGroup
	for i := 0; i < tills; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cart := fmt.Sprintf("P473RACE-%s-%d", f.suffix, i)
			status, body := f.checkout(f.cashierToken, map[string]interface{}{
				"cart_number": cart, "idempotency_key": cart,
				"location": f.location, "payment_mode": "Cash",
				"items": []map[string]interface{}{{"sku": f.sku, "qty": qtyEach}},
			})
			results[i] = result{status: status, body: body, cart: cart}
		}(i)
	}
	wg.Wait()
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE idempotency_key LIKE $1`, f.schema), "%P473RACE-"+f.suffix+"%")
	})

	sold := 0
	for _, r := range results {
		if r.status == http.StatusOK && r.body["status"] == "completed" {
			sold++
			rec := f.reconcile(r.cart)
			if rec.Verdict != "Balanced" {
				t.Errorf("%s reported success but does not reconcile: %s", r.cart, rec.Detail)
			}
			continue
		}
		// A rejection is fine - but it must be an HONEST one. 47.3.4's rule:
		// a business shortage and a retryable conflict are different answers,
		// and neither may be reported as a server error.
		if r.status == http.StatusInternalServerError {
			t.Errorf("%s failed with a 500 (%v); contention must produce a named shortage or a retryable conflict, never an unexplained server error", r.cart, r.body)
		}
	}
	if sold == 0 {
		t.Fatal("all 8 concurrent tills failed; at least one sale must succeed against 10 units of stock")
	}
	remaining := f.availabilityFor(f.sku)
	if remaining != 10-(sold*qtyEach) {
		t.Errorf("%d sale(s) of %d units each left availability at %d; want %d - stock and sales must agree exactly under concurrency",
			sold, qtyEach, remaining, 10-(sold*qtyEach))
	}
	if remaining < 0 {
		t.Errorf("availability went negative (%d) - the FOR UPDATE floor check was defeated by concurrency", remaining)
	}
}

// TestStage473IdempotencyKeyReuseWithDifferentPayloadIsRefused: a key is a
// promise about ONE request. Answering a different request with a stored
// outcome would tell the till a sale happened that never did.
func TestStage473IdempotencyKeyReuseWithDifferentPayloadIsRefused(t *testing.T) {
	f := newPOSPricingFixture(t, 400)
	key := "P473MISMATCH-" + f.suffix
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE idempotency_key LIKE $1`, f.schema), "%"+key)
	})

	if status, resp := f.checkout(f.cashierToken, map[string]interface{}{
		"cart_number": "P473MM-A-" + f.suffix, "idempotency_key": key,
		"location": f.location, "payment_mode": "Cash",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	}); status != http.StatusOK {
		t.Fatalf("the first sale failed (%d): %v", status, resp)
	}

	status, resp := f.checkout(f.cashierToken, map[string]interface{}{
		"cart_number": "P473MM-B-" + f.suffix, "idempotency_key": key,
		"location": f.location, "payment_mode": "Cash",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 7}},
	})
	if status != http.StatusConflict {
		t.Fatalf("a DIFFERENT sale reusing the same idempotency key got HTTP %d (%v); it must be refused, not answered with the first sale's outcome", status, resp)
	}
}

// TestStage473AuthorizeVoidReleasesStockAndPostsNothing covers 47.3.3's
// compensating path: an authorization that is voided must leave the store's
// stock exactly as it found it and the GL untouched.
func TestStage473AuthorizeVoidReleasesStockAndPostsNothing(t *testing.T) {
	f := newPOSPricingFixture(t, 300)
	cart := "P473AUTH-" + f.suffix

	status, resp := f.checkout(f.cashierToken, map[string]interface{}{
		"cart_number": cart, "idempotency_key": cart, "authorize_only": true,
		"location": f.location, "payment_mode": "Card",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 4}},
	})
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE idempotency_key LIKE $1`, f.schema), "%"+cart)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_reservation WHERE sku = $1`, f.schema), f.sku)
	})
	if status != http.StatusOK || resp["status"] != "authorized" {
		t.Fatalf("authorize-only checkout failed (%d): %v", status, resp)
	}
	if resp["payment_state"] != engines.PaymentStateInitiated {
		t.Errorf("payment_state = %v, want %s", resp["payment_state"], engines.PaymentStateInitiated)
	}
	rec := f.reconcile(cart)
	if rec.RevenuePosted != 0 || rec.LedgerQty != 0 {
		t.Fatalf("an authorization posted revenue %.2f / %d ledger unit(s); nothing may post until the payment is confirmed", rec.RevenuePosted, rec.LedgerQty)
	}

	// The hold is real - reserved stock is not available to sell.
	var reserved int
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(quantity), 0) FROM %s.inventory_reservation WHERE sku = $1 AND location_code = $2`, f.schema),
		f.sku, f.location).Scan(&reserved)
	if reserved != 4 {
		t.Errorf("the authorization reserved %d unit(s), want 4 - an authorization that holds nothing lets the same stock be sold twice", reserved)
	}

	if err := engines.VoidPOSSale("default", cart, "card declined"); err != nil {
		t.Fatalf("void failed: %v", err)
	}
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(quantity), 0) FROM %s.inventory_reservation WHERE sku = $1 AND location_code = $2`, f.schema),
		f.sku, f.location).Scan(&reserved)
	if reserved != 0 {
		t.Errorf("%d unit(s) are still reserved after the void; a declined card must release the goods", reserved)
	}
	if engines.CanTransitionPaymentState(engines.PaymentStateVoided, engines.PaymentStatePosted) {
		t.Error("a Voided sale can still transition to Posted - the state machine allows a voided payment to become a sale")
	}
}

// TestStage473ConfirmPostsExactlyOnce: two terminals confirming the same
// authorization must produce one sale, not two.
func TestStage473ConfirmPostsExactlyOnce(t *testing.T) {
	f := newPOSPricingFixture(t, 300)
	cart := "P473CONF-" + f.suffix
	before := f.availabilityFor(f.sku)

	if status, resp := f.checkout(f.cashierToken, map[string]interface{}{
		"cart_number": cart, "idempotency_key": cart, "authorize_only": true,
		"location": f.location, "payment_mode": "Card",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 2}},
	}); status != http.StatusOK {
		t.Fatalf("authorize failed (%d): %v", status, resp)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE idempotency_key LIKE $1`, f.schema), "%"+cart)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_reservation WHERE sku = $1`, f.schema), f.sku)
	})

	const confirms = 5
	okCount := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < confirms; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := engines.ConfirmPOSSale("default", cart, "TERMREF-"+f.suffix, ""); err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if okCount == 0 {
		t.Fatal("none of the concurrent confirms posted the sale")
	}
	if got := f.availabilityFor(f.sku); got != before-2 {
		t.Errorf("%d concurrent confirms of one authorization moved availability %d -> %d; exactly 2 units must have been sold", confirms, before, got)
	}
	rec := f.reconcile(cart)
	if rec.Verdict != "Balanced" {
		t.Errorf("the confirmed sale does not reconcile: %s", rec.Detail)
	}
	if rec.PaymentState != engines.PaymentStatePosted {
		t.Errorf("payment_state = %q after confirmation, want %s", rec.PaymentState, engines.PaymentStatePosted)
	}
}

// seedPOSCustomer creates a disposable Customer document for a loyalty test.
func seedPOSCustomer(t *testing.T, schema, customerID string) {
	t.Helper()
	data := `{"name":"47.3 test customer","status":"Active"}`
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2::jsonb, 'Active', 'system')`, schema),
		customerID, data); err != nil {
		t.Fatalf("failed to seed customer: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1`, schema), customerID)
	})
}
