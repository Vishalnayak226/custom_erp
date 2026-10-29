package engines

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"custom_erp/db"
)

// Stage 49.5.7 - adversarial invariant tests on the real sale path.
//
// Already proven elsewhere, and not repeated here:
//   - a lost response retried 100 times posts one sale -
//     TestStage473RepeatedIdenticalRequestsPostExactlyOneSale
//   - one idempotency key with a different payload is refused -
//     TestStage473IdempotencyKeyReuseWithDifferentPayloadIsRefused
//   - eight tills selling one SKU never oversell - TestStage473ConcurrentTillsSellingTheSameSKU
//   - client-sent prices and costs never reach the sale - TestStage472ClientSubmittedPricesNeverReachTheSale
//   - price tampering cannot bypass discount approval -
//     TestA02DiscountApprovalBypassedByPriceTamperingInsteadOfDiscountPct
//   - a failure part-way leaves nothing posted, and its retry posts once -
//     TestStage473FailureAfterStockLeavesNothingPosted, TestA03FailedCheckoutRetryDoubleDecrementsAvailability
//   - a direct record-API write cannot make a bill Paid -
//     TestStage47164GenericSaveCannotUnpostOrFakeASale
//
// What this file adds, at the engine (below the HTTP layer, where a second
// caller of the finalizer would land): two devices finalizing the same cart
// at once, and two tills racing for the last unit. Its HTTP twin, a second
// tab replaying a Paid cart under its own key, is
// TestStage4957SecondTabReplayReportsTheChargedTotal (internal/server).

type adversarialSaleFixture struct {
	t                *testing.T
	tenantID, schema string
	loc, sku         string
}

func newAdversarialSaleFixture(t *testing.T, price float64, stock float64) *adversarialSaleFixture {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	f := &adversarialSaleFixture{t: t, tenantID: tenantID, schema: schema, loc: "STORE-ADV", sku: "ADV-TEE"}
	f.insert(f.loc, "Location", "Active", map[string]interface{}{"code": f.loc, "name": "Store ADV", "type": "Store", "sellable": "Yes"})
	f.insert(f.sku, "Item", "Active", map[string]interface{}{"code": f.sku, "name": "Tee", "sale_price": price,
		"hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 5.0})
	if _, err := PostInventoryLedger(tenantID, f.loc, []interface{}{map[string]interface{}{"sku": f.sku, "qty": stock}}, false); err != nil {
		t.Fatalf("stock: %v", err)
	}
	return f
}

func (f *adversarialSaleFixture) insert(id, doctype, status string, data map[string]interface{}) {
	f.t.Helper()
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
		f.t.Fatalf("insert %s: %v", id, err)
	}
}

// cart prices a cart the way checkout does (ResolvePOSQuote) and stores it
// as Processing, ready to finalize.
func (f *adversarialSaleFixture) cart(id string, qty int) {
	f.t.Helper()
	q, err := ResolvePOSQuote(f.tenantID, QuoteRequest{Location: f.loc, Lines: []QuoteLineRequest{{Sku: f.sku, Qty: qty}}})
	if err != nil {
		f.t.Fatalf("quote: %v", err)
	}
	f.insert(id, "POSCart", "Processing", map[string]interface{}{"location": f.loc, "payment_mode": "Cash",
		"items": POSCartItemsFromQuote(q), "gst_breakdown": q.GSTBreakdown})
}

func (f *adversarialSaleFixture) available() float64 {
	f.t.Helper()
	var n float64
	if err := db.DB.QueryRow("SELECT available FROM "+f.schema+".inventory_availability WHERE sku = $1 AND location_code = $2", f.sku, f.loc).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *adversarialSaleFixture) count(q string, args ...interface{}) int {
	f.t.Helper()
	var n int
	if err := db.DB.QueryRow(strings.ReplaceAll(q, "{s}", f.schema), args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	return n
}

// Two tabs or devices submit the same cart at the same moment (a double tap,
// a retry racing the original). The cart row's lock makes the second wait
// and then see a Paid cart: one stock deduction, one stock-ledger line, one
// revenue posting, the sale reconciles, and every caller is told the same
// total - including a fractional-rupee price.
func TestStage4957TwoDevicesFinalizingOneCartSellOnce(t *testing.T) {
	f := newAdversarialSaleFixture(t, 99.50, 10)
	const cart, devices = "CART-ADV-TWIN", 6
	f.cart(cart, 2)
	before := f.available()

	totals := make([]float64, devices)
	errs := make([]error, devices)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < devices; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			totals[i], _, errs[i] = FinalizePOSCheckout(f.tenantID, cart, "")
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("device %d: %v", i, err)
		}
		if totals[i] != 199.00 {
			t.Errorf("device %d was told %.2f, want 199.00 - every caller sees the charged total", i, totals[i])
		}
	}
	if got := f.available(); got != before-2 {
		t.Errorf("availability %.0f -> %.0f, want exactly one deduction of 2", before, got)
	}
	if n := f.count("SELECT COUNT(*) FROM {s}.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", cart); n != 1 {
		t.Errorf("%d stock-ledger lines for the sale, want 1", n)
	}
	// One sale posts each of its entries once: the revenue entry (cash
	// against 4100) and the GST entry (4100 against output GST) - never a
	// second copy of either, whoever finalized it.
	if n := f.count("SELECT COUNT(*) FROM {s}.gl_postings WHERE document_type = 'POSCart' AND document_id = $1 AND idempotency_key = $2 AND account_code = '4100'", cart, "POSCart:"+cart+":SALE_REVENUE"); n != 1 {
		t.Errorf("%d revenue postings for the sale, want 1", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM (SELECT idempotency_key, account_code FROM {s}.gl_postings WHERE document_id = $1
		GROUP BY 1, 2 HAVING COUNT(*) > 1) d`, cart); n != 0 {
		t.Errorf("%d ledger entries were posted twice", n)
	}
	if n := f.count("SELECT (SUM(debit) - SUM(credit))::int FROM {s}.gl_postings WHERE document_id = $1 AND account_code = '1100'", cart); n != 19900 {
		t.Errorf("cash debited %d paise for the sale, want 19900 once", n)
	}
	if n := f.count("SELECT COUNT(*) FROM {s}.documents WHERE doctype = 'POSCart' AND id = $1 AND status = 'Paid'", cart); n != 1 {
		t.Errorf("the cart is not Paid exactly once")
	}
	rec, err := ReconcileSale(f.tenantID, cart)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Verdict != "Balanced" || rec.LedgerQty != 2 {
		t.Errorf("the sale does not reconcile: %s (%s), ledger qty %d", rec.Verdict, rec.Detail, rec.LedgerQty)
	}
}

// Two tills, two carts, one unit left, finalized at the same instant: the
// stock row's lock lets exactly one through; the other is refused as a
// shortage (not a 500, not a negative balance) and posts nothing.
func TestStage4957LastUnitGoesToExactlyOneTill(t *testing.T) {
	f := newAdversarialSaleFixture(t, 250, 1)
	carts := []string{"CART-ADV-A", "CART-ADV-B"}
	for _, c := range carts {
		f.cart(c, 1)
	}
	errs := make([]error, len(carts))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, c := range carts {
		wg.Add(1)
		go func(i int, c string) {
			defer wg.Done()
			<-start
			_, _, errs[i] = FinalizePOSCheckout(f.tenantID, c, "")
		}(i, c)
	}
	close(start)
	wg.Wait()

	won, lost := -1, -1
	for i, err := range errs {
		if err == nil {
			if won >= 0 {
				t.Fatalf("both tills sold the last unit")
			}
			won = i
			continue
		}
		var short *InsufficientStockError
		if !errors.As(err, &short) && !strings.Contains(strings.ToLower(err.Error()), "insufficient") {
			t.Errorf("till %d was refused for something other than the shortage: %v", i, err)
		}
		lost = i
	}
	if won < 0 || lost < 0 {
		t.Fatalf("exactly one till sells the last unit: errors %v", errs)
	}
	if got := f.available(); got != 0 {
		t.Errorf("availability %.0f after selling the last unit, want 0 (never negative)", got)
	}
	loser := carts[lost]
	if n := f.count("SELECT COUNT(*) FROM {s}.gl_postings WHERE document_id = $1", loser); n != 0 {
		t.Errorf("the refused sale %s posted %d ledger lines", loser, n)
	}
	if n := f.count("SELECT COUNT(*) FROM {s}.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", loser); n != 0 {
		t.Errorf("the refused sale %s wrote %d stock-ledger lines", loser, n)
	}
	if n := f.count("SELECT COUNT(*) FROM {s}.documents WHERE id = $1 AND status = 'Paid'", loser); n != 0 {
		t.Errorf("the refused sale %s is marked Paid", loser)
	}
	if rec, err := ReconcileSale(f.tenantID, carts[won]); err != nil || rec.Verdict != "Balanced" {
		t.Errorf("the winning sale does not reconcile: %+v %v", rec, err)
	}
}
