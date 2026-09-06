package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// Stage 47.0.1 / audit finding A-04 ("Legacy POS return is replayable and can
// separate stock from finance" - docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md
// lines 93-99; traced line-by-line in
// docs/audits/STAGE47_ENDPOINT_MUTATION_MAP_2026-09-03.md section 2.2).
//
// Exact mechanism reproduced here: ProcessReturnAnywhere (engines/fulfillment.go:297)
// checks a PARTIAL return's cumulative quantity against sumPriorReturns, which
// reads the total from the one SalesReturn document keyed by the deterministic
// ID "RET-<originalOrderID>" (handlers_operations.go:462). That document is
// written once, by the FIRST return call. Every subsequent call against the
// same original order hits a primary-key conflict on that same insert, and
// handlers_operations.go:476 discards the resulting error - so the recorded
// "already returned" total never advances past what the first call wrote,
// even though stock and GL postings for every later call still go through
// (fulfillment.go:415-425 increments inventory_availability unconditionally
// once SALESR-0130's check passes, and the two PostDoubleEntry reversal calls
// at :442/:449 run after that with no idempotency key at all, as the
// function's own comment at :434-439 states). A cashier (or a duplicated
// network retry, or two browser tabs) can therefore replay the same partial
// return repeatedly and inflate returned stock/refund liability arbitrarily
// far past what was ever actually sold.
//
// This test asserts the SECURE/CORRECT outcome - cumulative returns against
// one original order can never exceed what was sold.
//
// CLOSED by Stage 47.4.1 (2026-09-06) and PROMOTED out of the stage47redteam
// build tag per 47.0.1's own closure note. The closure taken was the first of
// the two the audit offered: the legacy path is REMOVED in favour of the
// ReturnRequest aggregate, whose eligibility check runs inside a transaction
// holding the original sale locked and which carries a real per-return
// idempotency key. ProcessReturnAnywhere is now a named refusal, so the four
// replayed calls below are refused four times over and nothing is returned at
// all - which is a stronger outcome than the "at most 10 of 10" this test was
// written to demand, and the assertion below still holds it to that.
//
// The replacement path's own concurrency, replay and reconciliation behaviour
// is covered by internal/server/returns_stage47_4_test.go.
func TestA04PartialReturnReplayInflatesStockBeyondSoldQty(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("failed to resolve tenant schema: %v", err)
	}

	sku := NewDocID("A04SKU")
	location := NewDocID("A04LOC")
	origOrderID := NewDocID("A04ORD")
	const soldQty = 10
	const returnQtyPerCall = 3 // 4 calls x 3 = 12 > soldQty (10)

	// Seed the "original sale" directly as a Paid POSCart - resolveOriginalSale
	// (fulfillment.go:26) reads exactly this shape, so this stands in for a
	// real completed checkout without needing FinalizePOSCheckout's full
	// GST/session/loyalty machinery, none of which this finding is about.
	cartData, _ := json.Marshal(map[string]interface{}{
		"items": []map[string]interface{}{
			{"sku": sku, "qty": soldQty},
		},
	})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')`, schema),
		origOrderID, cartData); err != nil {
		t.Fatalf("failed to seed original POSCart: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'POSCart'`, schema), origOrderID)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'SalesReturn'`, schema), "RET-"+origOrderID)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), sku, location)
	})

	returnItems := []interface{}{
		map[string]interface{}{"sku": sku, "qty": returnQtyPerCall, "sale_price": 100.0, "cost_price": 60.0},
	}

	successfulReturns := 0
	var totalReturnedQty int
	for i := 0; i < 4; i++ {
		_, err := ProcessReturnAnywhere(tenantID, location, origOrderID, returnItems)
		if err == nil {
			successfulReturns++
			totalReturnedQty += returnQtyPerCall
		}
	}

	if successfulReturns != 0 {
		t.Fatalf("the retired ProcessReturnAnywhere accepted %d of 4 replayed partial returns; Stage 47.4.1 makes every call a refusal", successfulReturns)
	}
	if totalReturnedQty > soldQty {
		t.Fatalf("A-04: %d partial-return call(s) of qty %d each against an order that sold only %d succeeded, returning %d total (%d call(s) accepted) - cumulative returns exceeded what was ever sold. This must be rejected once 47.4 replaces the legacy path's stale-document replay check (sumPriorReturns reading a SalesReturn row that later calls fail to update, see this file's header comment) with a real cumulative/idempotent guard.",
			4, returnQtyPerCall, soldQty, totalReturnedQty, successfulReturns)
	}
}
