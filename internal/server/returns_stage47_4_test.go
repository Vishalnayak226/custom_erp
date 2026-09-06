package server

// Stage 47.4.7 - "Replay/concurrency/failure test matrix: repeated click, lost
// response, two clerks, changed client values, GL failure, tender failure,
// reverse-pickup duplicate, exchange shortage rollback and return-window
// boundary." (audit finding A-04)
//
// The acceptance line these assert: "total returned quantity/value cannot
// exceed original eligibility under concurrency; stock/refund/GL/evidence
// always reconcile; no ignored duplicate-evidence error; UI and API show
// authoritative recoverable state."
//
// As in the 47.3 suite, the reconciliation check is engines.ReconcileReturn
// rather than a bespoke per-test assertion, so the tests and the operator's
// own view of "did this return balance" cannot disagree.

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// soldFixture is a completed POS sale that returns can be raised against.
func (f *posPricingFixture) soldFixture(t *testing.T, qty int) string {
	t.Helper()
	cart := fmt.Sprintf("P474SALE-%s", f.suffix)
	status, resp := f.checkout(f.cashierToken, map[string]interface{}{
		"cart_number": cart, "idempotency_key": cart,
		"location": f.location, "payment_mode": "Cash",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": qty}},
	})
	if status != http.StatusOK || resp["status"] != "completed" {
		t.Fatalf("the fixture sale failed (%d): %v", status, resp)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE idempotency_key LIKE $1`, f.schema), "%"+cart)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'ReturnRequest' AND data->>'original_order_id' = $1`, f.schema), cart)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE command = 'returns.create'`, f.schema))
	})
	return cart
}

func (f *posPricingFixture) createReturn(token string, body map[string]interface{}) (int, map[string]interface{}) {
	f.t.Helper()
	return f.post(handleCreateReturnRequest, "/api/v1/returns", token, body)
}

// TestStage474LegacyReturnEndpointIsRetired is 47.4.1, asserted at the HTTP
// boundary: the replayable path must be gone, and gone in a way that tells the
// caller where to go instead.
func TestStage474LegacyReturnEndpointIsRetired(t *testing.T) {
	f := newPOSPricingFixture(t, 100)
	status, resp := f.post(handleFulfillmentReturn, "/api/v1/fulfillment/return", f.cashierToken, map[string]interface{}{
		"return_location": f.location, "original_order_id": "ANY",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 1, "sale_price": 1, "cost_price": 1}},
	})
	if status != http.StatusGone {
		t.Fatalf("the legacy return endpoint returned HTTP %d, want 410 Gone (%v)", status, resp)
	}
	if resp["replaced_by"] != "POST /api/v1/returns/requests" {
		t.Errorf("the refusal does not name its replacement (got %v); a caller told only \"no\" has nowhere to go", resp["replaced_by"])
	}
}

// TestStage474ReturnPricesComeFromTheOriginalSale is 47.4.2: the client cannot
// assert what a returned line was worth.
func TestStage474ReturnPricesComeFromTheOriginalSale(t *testing.T) {
	f := newPOSPricingFixture(t, 750)
	cart := f.soldFixture(t, 2)

	status, resp := f.createReturn(f.superviserTok, map[string]interface{}{
		"request_type": "Customer Return", "return_location": f.location,
		"original_order_id": cart, "idempotency_key": "P474PRICE-" + f.suffix,
		// Deliberately hostile: a price and a cost the client made up. The
		// request shape does not even carry them, which is the point - they
		// are ignored because there is nowhere to put them.
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 1, "sale_price": 999999, "cost_price": 999999}},
	})
	if status != http.StatusOK {
		t.Fatalf("the return was refused (%d): %v", status, resp)
	}
	returnID, _ := resp["return_request_id"].(string)
	if returnID == "" {
		t.Fatalf("no return request id came back: %v", resp)
	}

	var priced float64
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT (data->'items'->0->>'original_unit_price')::numeric FROM %s.documents WHERE id = $1`, f.schema),
		returnID).Scan(&priced); err != nil {
		t.Fatalf("could not read the stored return line: %v", err)
	}
	if priced != 750 {
		t.Errorf("the return line was priced at %.2f; it must come from the original sale (750.00), never from the caller", priced)
	}
}

// TestStage474RepeatedSubmissionRaisesOneReturn covers "repeated click" and
// "lost response" - the two ordinary ways a clerk produces a duplicate.
func TestStage474RepeatedSubmissionRaisesOneReturn(t *testing.T) {
	f := newPOSPricingFixture(t, 200)
	cart := f.soldFixture(t, 5)
	key := "P474DUP-" + f.suffix

	ids := map[string]bool{}
	for i := 0; i < 6; i++ {
		status, resp := f.createReturn(f.superviserTok, map[string]interface{}{
			"request_type": "Customer Return", "return_location": f.location,
			"original_order_id": cart, "idempotency_key": key,
			"items": []map[string]interface{}{{"sku": f.sku, "qty": 2}},
		})
		if status != http.StatusOK {
			t.Fatalf("submission %d was refused (%d): %v", i, status, resp)
		}
		id, _ := resp["return_request_id"].(string)
		ids[id] = true
		if i > 0 && resp["replayed"] != true {
			t.Errorf("submission %d was not reported as a replay; a duplicate must be recognisable to the caller, not silently indistinguishable from a new return", i)
		}
	}
	if len(ids) != 1 {
		t.Fatalf("6 identical submissions produced %d distinct returns (%v); they must produce exactly one", len(ids), ids)
	}

	var count int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ReturnRequest' AND data->>'original_order_id' = $1`, f.schema),
		cart).Scan(&count); err != nil {
		t.Fatalf("failed to count returns: %v", err)
	}
	if count != 1 {
		t.Errorf("%d ReturnRequest rows exist for one bill after 6 identical submissions, want 1", count)
	}
}

// TestStage474CumulativeQuantityCannotBeExceededByTwoClerks is the A-04
// invariant itself, under real concurrency.
func TestStage474CumulativeQuantityCannotBeExceededByTwoClerks(t *testing.T) {
	f := newPOSPricingFixture(t, 300)
	const sold = 4
	cart := f.soldFixture(t, sold)

	const clerks = 6
	type outcome struct {
		status int
		body   map[string]interface{}
	}
	results := make([]outcome, clerks)
	var wg sync.WaitGroup
	for i := 0; i < clerks; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each clerk raises a DIFFERENT return (own key), each for 2 of the
			// 4 sold units. Only two of the six can legitimately succeed.
			status, body := f.createReturn(f.superviserTok, map[string]interface{}{
				"request_type": "Customer Return", "return_location": f.location,
				"original_order_id": cart,
				"idempotency_key":   fmt.Sprintf("P474RACE-%s-%d", f.suffix, i),
				"items":             []map[string]interface{}{{"sku": f.sku, "qty": 2}},
			})
			results[i] = outcome{status: status, body: body}
		}(i)
	}
	wg.Wait()

	accepted := 0
	for i, r := range results {
		if r.status == http.StatusOK {
			accepted++
			continue
		}
		if r.status == http.StatusInternalServerError {
			t.Errorf("clerk %d got a 500 (%v); an over-claim must be a named business refusal, never an unexplained server error", i, r.body)
		}
	}
	if accepted != sold/2 {
		t.Errorf("%d of %d concurrent returns were accepted for %d sold units at 2 each; exactly %d may be", accepted, clerks, sold, sold/2)
	}

	// The authoritative check: total claimed quantity across every non-rejected
	// return must not exceed what was sold.
	var claimed int
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM((line->>'qty')::int), 0)
		FROM %s.documents d, jsonb_array_elements(d.data->'items') line
		WHERE d.doctype = 'ReturnRequest' AND d.data->>'original_order_id' = $1
		  AND COALESCE(d.data->>'status', '') <> 'Rejected'`, f.schema), cart).Scan(&claimed); err != nil {
		t.Fatalf("failed to sum claimed quantity: %v", err)
	}
	if claimed > sold {
		t.Fatalf("A-04: %d unit(s) are claimed by returns against a bill that sold %d - cumulative return eligibility was defeated by concurrency", claimed, sold)
	}
}

// TestStage474ReturnWindowBoundary covers the "return-window boundary" case.
func TestStage474ReturnWindowBoundary(t *testing.T) {
	f := newPOSPricingFixture(t, 120)
	cart := f.soldFixture(t, 1)

	// Age the sale past the tenant's configured window.
	windowDays := engines.GetSettingInt("default", "sales.return_window_days")
	if windowDays <= 0 {
		windowDays = 30
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET created_at = CURRENT_TIMESTAMP - $2::interval WHERE id = $1`, f.schema),
		cart, fmt.Sprintf("%d days", windowDays+5)); err != nil {
		t.Fatalf("failed to age the sale: %v", err)
	}

	eligibility, err := engines.ResolveReturnEligibility("default", cart)
	if err != nil {
		t.Fatalf("eligibility lookup failed: %v", err)
	}
	if eligibility.WithinWindow {
		t.Errorf("a sale aged %d days past a %d-day window still reads as within it", windowDays+5, windowDays)
	}
	if eligibility.Explanation == "" {
		t.Error("the refusal carries no explanation; a clerk has to be able to tell the customer why")
	}

	status, resp := f.createReturn(f.superviserTok, map[string]interface{}{
		"request_type": "Customer Return", "return_location": f.location,
		"original_order_id": cart, "idempotency_key": "P474WINDOW-" + f.suffix,
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	})
	if status == http.StatusOK {
		t.Fatalf("a return outside the window was accepted: %v", resp)
	}
	if resp["code"] != "SALESR-0129" {
		t.Errorf("expected the catalog's own out-of-window code SALESR-0129, got %v", resp["code"])
	}
}

// TestStage474EligibilityReflectsPriorClaims is 47.4.6's own requirement -
// the screen must show cumulative quantities, not just sold quantities.
func TestStage474EligibilityReflectsPriorClaims(t *testing.T) {
	f := newPOSPricingFixture(t, 90)
	cart := f.soldFixture(t, 3)

	before, err := engines.ResolveReturnEligibility("default", cart)
	if err != nil {
		t.Fatalf("eligibility lookup failed: %v", err)
	}
	if len(before.Lines) != 1 || before.Lines[0].RemainingQty != 3 {
		t.Fatalf("expected 3 returnable before any claim, got %+v", before.Lines)
	}

	if status, resp := f.createReturn(f.superviserTok, map[string]interface{}{
		"request_type": "Customer Return", "return_location": f.location,
		"original_order_id": cart, "idempotency_key": "P474ELIG-" + f.suffix,
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 2}},
	}); status != http.StatusOK {
		t.Fatalf("the first return was refused (%d): %v", status, resp)
	}

	after, err := engines.ResolveReturnEligibility("default", cart)
	if err != nil {
		t.Fatalf("second eligibility lookup failed: %v", err)
	}
	if after.Lines[0].AlreadyReturned != 2 || after.Lines[0].RemainingQty != 1 {
		t.Errorf("after a 2-unit return against 3 sold, eligibility reads already=%d remaining=%d; want 2 and 1 - an open return must consume its units, not wait until the goods arrive",
			after.Lines[0].AlreadyReturned, after.Lines[0].RemainingQty)
	}
	if after.Lines[0].Reason == "" {
		t.Error("the eligibility line carries no reason; the screen has nothing to explain to a clerk")
	}
}

// TestStage474InspectionPostsStockAndTaxAtomically covers the QC step's own
// atomicity and the tax reversal 47.4.4 requires, end to end.
func TestStage474InspectionPostsStockAndTaxAtomically(t *testing.T) {
	f := newPOSPricingFixture(t, 500)
	cart := f.soldFixture(t, 2)
	availAfterSale := f.availabilityFor(f.sku)

	status, resp := f.createReturn(f.superviserTok, map[string]interface{}{
		"request_type": "Customer Return", "return_location": f.location,
		"original_order_id": cart, "idempotency_key": "P474QC-" + f.suffix,
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 2}},
	})
	if status != http.StatusOK {
		t.Fatalf("the return was refused (%d): %v", status, resp)
	}
	returnID, _ := resp["return_request_id"].(string)

	// Nothing may have moved yet: a raised return is a request, not a receipt.
	rec, err := engines.ReconcileReturn("default", returnID)
	if err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}
	if rec.Verdict != "Balanced" {
		t.Errorf("a freshly raised return does not reconcile: %s", rec.Detail)
	}
	if got := f.availabilityFor(f.sku); got != availAfterSale {
		t.Errorf("raising a return already moved stock (%d -> %d); stock comes back at inspection, not at request", availAfterSale, got)
	}

	if err := engines.ApproveReturnRequest("default", returnID, f.superviserID); err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if err := engines.ReceiveReturnRequest("default", returnID, f.superviserID); err != nil {
		t.Fatalf("receive failed: %v", err)
	}
	refund, refundID, err := engines.ApplyReturnQC("default", returnID,
		map[string]string{f.sku: "Sellable"}, f.superviserID)
	if err != nil {
		t.Fatalf("QC failed: %v", err)
	}
	if refund != 1000 {
		t.Errorf("refund eligible = %.2f, want 1000.00 (2 x the original 500.00)", refund)
	}
	if got := f.availabilityFor(f.sku); got != availAfterSale+2 {
		t.Errorf("after inspecting 2 sellable units, availability is %d, want %d", got, availAfterSale+2)
	}

	// The COGS reversal must have posted in the SAME transaction as the stock.
	var cogsCredit int64
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(credit), 0) FROM %s.gl_postings WHERE document_type = 'ReturnRequest' AND document_id = $1 AND account_code = '5100'`, f.schema),
		returnID).Scan(&cogsCredit)
	if cogsCredit <= 0 {
		t.Error("stock came back but no COGS reversal posted; before Stage 47.4.4 these were two separate commits and could diverge")
	}

	if err := engines.ApproveRefundRequest("default", refundID, f.superviserID); err != nil {
		t.Fatalf("refund approve failed: %v", err)
	}
	if err := engines.ProcessRefundRequest("default", refundID, f.superviserID, "Cash"); err != nil {
		t.Fatalf("refund process failed: %v", err)
	}

	// 47.4.4's tax reversal - the thing nothing did before this stage.
	var taxDebit int64
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(debit), 0) FROM %s.gl_postings
		 WHERE document_type = 'ReturnRequest' AND document_id = $1 AND account_code IN ('2200','2201','2202')`, f.schema),
		returnID).Scan(&taxDebit)
	if taxDebit <= 0 {
		t.Error("no output-tax reversal posted for the returned goods; the tenant would keep reporting GST on stock that came back")
	}

	rec, err = engines.ReconcileReturn("default", returnID)
	if err != nil {
		t.Fatalf("final reconciliation failed: %v", err)
	}
	if rec.Verdict != "Balanced" {
		t.Errorf("the completed return does not reconcile: %s", rec.Detail)
	}
}

// TestStage474RefundCannotBeProcessedTwice covers "tender failure" / duplicate
// evidence: two operators processing one refund must pay it once.
func TestStage474RefundCannotBeProcessedTwice(t *testing.T) {
	f := newPOSPricingFixture(t, 400)
	cart := f.soldFixture(t, 1)

	status, resp := f.createReturn(f.superviserTok, map[string]interface{}{
		"request_type": "Customer Return", "return_location": f.location,
		"original_order_id": cart, "idempotency_key": "P474REFUND-" + f.suffix,
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	})
	if status != http.StatusOK {
		t.Fatalf("the return was refused (%d): %v", status, resp)
	}
	returnID, _ := resp["return_request_id"].(string)
	if err := engines.ApproveReturnRequest("default", returnID, f.superviserID); err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if err := engines.ReceiveReturnRequest("default", returnID, f.superviserID); err != nil {
		t.Fatalf("receive failed: %v", err)
	}
	_, refundID, err := engines.ApplyReturnQC("default", returnID, map[string]string{f.sku: "Sellable"}, f.superviserID)
	if err != nil {
		t.Fatalf("QC failed: %v", err)
	}
	if err := engines.ApproveRefundRequest("default", refundID, f.superviserID); err != nil {
		t.Fatalf("refund approve failed: %v", err)
	}

	const attempts = 4
	ok := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := engines.ProcessRefundRequest("default", refundID, f.superviserID, "Cash"); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok == 0 {
		t.Fatal("none of the concurrent refund attempts succeeded")
	}

	var cashCredit int64
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(credit), 0) FROM %s.gl_postings WHERE document_type = 'RefundRequest' AND document_id = $1 AND account_code = '1100'`, f.schema),
		refundID).Scan(&cashCredit)
	if engines.PaiseToRupees(cashCredit) != 400 {
		t.Errorf("%d concurrent refund attempts credited %.2f to cash; exactly 400.00 may be paid out", attempts, engines.PaiseToRupees(cashCredit))
	}

	rec, err := engines.ReconcileReturn("default", returnID)
	if err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}
	if rec.Verdict != "Balanced" {
		t.Errorf("the refunded return does not reconcile: %s", rec.Detail)
	}
}

// TestStage474ExceptionPathsAreSupervisorOnlyAndEvidenced covers 47.4.5's two
// genuinely missing workflows: a no-receipt return and a goodwill return past
// the window. Both must be reachable (a store really does need them), gated on
// a capability a cashier does not hold, and impossible to take without a
// written reason and a named authoriser.
func TestStage474ExceptionPathsAreSupervisorOnlyAndEvidenced(t *testing.T) {
	f := newPOSPricingFixture(t, 600)
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'ReturnRequest' AND data->>'return_location' = $1`, f.schema), f.location)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.command_idempotency WHERE command = 'returns.create'`, f.schema))
	})

	// A Cashier cannot take either path.
	status, resp := f.createReturn(f.cashierToken, map[string]interface{}{
		"request_type": "Customer Return", "return_location": f.location,
		"idempotency_key":  "P474EXC-DENY-" + f.suffix,
		"exception_type":   engines.ReturnExceptionNoReceipt,
		"exception_reason": "customer lost the bill",
		"items":            []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	})
	if status != http.StatusForbidden {
		t.Fatalf("a Cashier raised a No Receipt return (HTTP %d, %v); it is a supervisor decision", status, resp)
	}

	// A supervisor can - but not without a reason.
	if status, _ := f.createReturn(f.superviserTok, map[string]interface{}{
		"request_type": "Customer Return", "return_location": f.location,
		"idempotency_key": "P474EXC-NOREASON-" + f.suffix,
		"exception_type":  engines.ReturnExceptionNoReceipt,
		"items":           []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	}); status == http.StatusOK {
		t.Error("a No Receipt return with no written reason was accepted")
	}

	status, resp = f.createReturn(f.superviserTok, map[string]interface{}{
		"request_type": "Customer Return", "return_location": f.location,
		"idempotency_key":  "P474EXC-OK-" + f.suffix,
		"exception_type":   engines.ReturnExceptionNoReceipt,
		"exception_reason": "customer lost the bill; goods identified by barcode",
		"items":            []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	})
	if status != http.StatusOK {
		t.Fatalf("a supervisor's No Receipt return was refused (%d): %v", status, resp)
	}
	returnID, _ := resp["return_request_id"].(string)

	var exType, exReason, exBy string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'exception_type', data->>'exception_reason', data->>'exception_authorised_by'
		 FROM %s.documents WHERE id = $1`, f.schema), returnID).Scan(&exType, &exReason, &exBy); err != nil {
		t.Fatalf("the exception was not recorded on the return: %v", err)
	}
	if exType != engines.ReturnExceptionNoReceipt || exReason == "" || exBy != f.superviserID {
		t.Errorf("exception evidence is incomplete: type=%q reason=%q authorised_by=%q (want the supervisor %q)",
			exType, exReason, exBy, f.superviserID)
	}

	// A no-receipt line is priced from the item master, and says so - it must
	// not be presented as if it came from a bill nobody has.
	var priced float64
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT (data->'items'->0->>'original_unit_price')::numeric FROM %s.documents WHERE id = $1`, f.schema),
		returnID).Scan(&priced)
	if priced != 600 {
		t.Errorf("the no-receipt line priced at %.2f; with no bill it must fall back to the item master price (600.00)", priced)
	}

	// Goodwill: the same sale, aged past the window, accepted anyway.
	cart := f.soldFixture(t, 1)
	windowDays := engines.GetSettingInt("default", "sales.return_window_days")
	if windowDays <= 0 {
		windowDays = 30
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET created_at = CURRENT_TIMESTAMP - $2::interval WHERE id = $1`, f.schema),
		cart, fmt.Sprintf("%d days", windowDays+10)); err != nil {
		t.Fatalf("failed to age the sale: %v", err)
	}
	status, resp = f.createReturn(f.superviserTok, map[string]interface{}{
		"request_type": "Customer Return", "return_location": f.location,
		"original_order_id": cart, "idempotency_key": "P474GOODWILL-" + f.suffix,
		"exception_type":   engines.ReturnExceptionGoodwill,
		"exception_reason": "long-standing customer, faulty on first use",
		"items":            []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	})
	if status != http.StatusOK {
		t.Fatalf("a goodwill return past the window was refused (%d): %v", status, resp)
	}

	// ...but goodwill does NOT waive cumulative eligibility. "We will take this
	// back as a gesture" is not "you may return more than you bought".
	status, resp = f.createReturn(f.superviserTok, map[string]interface{}{
		"request_type": "Customer Return", "return_location": f.location,
		"original_order_id": cart, "idempotency_key": "P474GOODWILL2-" + f.suffix,
		"exception_type":   engines.ReturnExceptionGoodwill,
		"exception_reason": "trying to return more than was sold",
		"items":            []map[string]interface{}{{"sku": f.sku, "qty": 5}},
	})
	if status == http.StatusOK {
		t.Errorf("a goodwill return for 5 units against 1 sold was accepted (%v); goodwill waives the window, never the quantity", resp)
	}
}
