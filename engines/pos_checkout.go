package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// FinalizePOSCheckout runs the side effects of a completed sale - inventory
// decrement, GL/GST posting, loyalty burn and earn - against a POSCart document
// that already exists in the documents table, and marks it Paid. Reads the
// cart's own stored data (captured by handleCheckout at request time) as the
// single source of truth, rather than taking items/totals as parameters, so it
// can be called identically from two places: handleCheckout's normal
// synchronous path, and handleDecideApproval once a manager approves a
// discount-gated cart (Stage 20.10) - the two can never compute different
// totals this way.
//
// Stage 47.3.2 (audit A-03) made all of it ONE transaction. Before that, this
// function ran five independently-committed steps in sequence - availability +
// stock ledger, loyalty burn, revenue/COGS, GST, exempt reclass - and unwound
// failures with hand-written compensations (markFailed, failAndRefundPoints)
// that could themselves fail and only log. A crash or a killed process between
// any two steps left a sale genuinely half-posted: stock gone with no revenue,
// or revenue with no COGS. The item's acceptance line is "every injected
// failure produces either zero result or one complete, explainable result",
// which sequential commits cannot deliver at any level of care.
//
// What is deliberately still OUTSIDE the transaction, and why:
//
//   - The stock LEDGER rows (WriteStockLedgerLines). They are append-only and
//     idempotency-keyed, so a crash between commit and write leaves entries a
//     replay writes exactly once. Inside the transaction they would be correct
//     too, but WriteStockLedgerEntry is called from ~20 other places with its
//     own transaction, and forking it for this one caller buys nothing the
//     idempotency key does not already guarantee.
//   - The loyalty EARN, the offline-sync variance and the costing gaps. All
//     three are purely additive records about a sale that has already
//     completed; none may undo it, which is exactly what including them would
//     risk.
func FinalizePOSCheckout(tenantID, cartNumber, correlationID string) (saleTotal float64, costTotal float64, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}
	outcome, err := finalizePOSCheckoutTx(tenantID, schema, cartNumber, correlationID, nil, "", nil)
	if err != nil {
		return 0, 0, err
	}
	return outcome.SaleTotal, outcome.CostTotal, nil
}

// posCart is the stored cart shape every finalization path reads.
type posCart struct {
	Location    string `json:"location"`
	PaymentMode string `json:"payment_mode"`
	CustomerID  string `json:"customer_id"`
	// Stage 47.2: sale_price on a stored cart is the SERVER's own resolved
	// price (handleCheckout overwrites the request's items with
	// ResolvePOSQuote's resolved lines before storing), not the browser's.
	// cost_price is gone from the shape entirely - see the cost resolution in
	// finalizePOSCheckoutTx.
	Items []struct {
		Sku       string  `json:"sku"`
		Qty       int     `json:"qty"`
		SalePrice float64 `json:"sale_price"`
	} `json:"items"`
	GSTBreakdown GSTBreakdown `json:"gst_breakdown"`
	// RedeemPoints (Stage 30.2.5) is the loyalty redemption the cashier
	// applied to this cart. It is an INTENT recorded on the cart, not an
	// already-burned balance: the burn happens at finalization, once the sale
	// is actually going through. Before this, "Redeem Points" burned the
	// points the instant it was clicked and left the cashier to type the
	// discount into a line's price by hand - so abandoning the cart lost the
	// customer's points outright, and forgetting to type the discount charged
	// them full price for a sale they had already paid for in points.
	RedeemPoints int `json:"redeem_points"`
	// 20.13: stamped by handleCheckout from the client's own "offline_synced"
	// request field - true only when this cart is being replayed from a
	// cashier's offline queue after reconnecting, never for a normal live
	// sale. Read back here (rather than threaded as a parameter) so every
	// finalization path gets the same behavior for free with no call-site
	// change.
	OfflineSynced bool `json:"offline_synced"`
}

// FinalizeOutcome is what one committed sale produced. Returned so the caller
// can build its response from the same numbers that were posted, and so the
// idempotency record can store that response verbatim for replay (47.3.1).
type FinalizeOutcome struct {
	SaleTotal       float64
	CostTotal       float64
	LoyaltyDiscount int
	NegativeEvents  []NegativeStockEvent
	CostingGaps     []CostingGapLine
}

// CostingGapLine is one sold line the server could not cost (47.2.2).
type CostingGapLine struct {
	SKU       string
	Qty       int
	SaleValue float64
}

// FinalizePOSCheckoutCommitted is FinalizePOSCheckout with the command-
// idempotency completion folded into the SAME transaction as the sale
// (47.3.1/47.3.2). idempotencyKey is the claim ClaimCommand acquired; response
// is what the caller intends to return and what a duplicate will be replayed.
//
// The response map is completed with the posted totals before it is stored, so
// a replay reports the figures that were really posted rather than the ones the
// caller guessed before posting them.
func FinalizePOSCheckoutCommitted(tenantID, cartNumber, correlationID, idempotencyKey string, response map[string]interface{}) (*FinalizeOutcome, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return finalizePOSCheckoutTx(tenantID, schema, cartNumber, correlationID, nil, idempotencyKey, response)
}

// finalizePOSCheckoutTx is the whole sale, in one transaction.
//
// tx may be nil, in which case one is opened and committed here; a caller that
// already holds a transaction (none today, but the returns engine's exchange
// path is the obvious future one) passes its own.
func finalizePOSCheckoutTx(tenantID, schema, cartNumber, correlationID string, tx *sql.Tx, idempotencyKey string, response map[string]interface{}) (*FinalizeOutcome, error) {
	ownTx := tx == nil
	if ownTx {
		begun, err := db.DB.Begin()
		if err != nil {
			return nil, err
		}
		tx = begun
		defer tx.Rollback()
	}
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}

	// Lock the cart first, before anything else is read or written. This is
	// the outermost lock in the deterministic order 47.3.4 requires (cart ->
	// availability rows in SKU order -> customer loyalty ledger), so two
	// requests racing on the same cart serialize here rather than deeper in,
	// where they would already have taken conflicting row locks.
	var dataStr, cashier, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, created_by, status FROM %s.documents WHERE doctype = 'POSCart' AND id = $1 FOR UPDATE`, schema),
		cartNumber).Scan(&dataStr, &cashier, &status); err != nil {
		return nil, fmt.Errorf("cart not found: %v", err)
	}
	if status == "Paid" {
		// Someone else finalized it while this request waited on the lock.
		// Not an error: the sale exists exactly once, which is the guarantee.
		return outcomeFromStoredCart(dataStr)
	}

	var cart posCart
	if err := json.Unmarshal([]byte(dataStr), &cart); err != nil {
		return nil, err
	}

	itemsInterface := make([]interface{}, len(cart.Items))
	// Stage 45: paise totals for the ledger/receipt, computed straight from
	// each line's own float64 price - not from a whole-rupee int() truncated
	// before the qty multiply, which is the exact leak the 2026-07-31
	// durability audit's finding #7 named (a fractional-rupee item lost its
	// fraction on every unit sold, not just once per sale).
	//
	// totalSaleRupees/totalCostRupees stay the OLD truncating computation,
	// unchanged, because the loyalty points economy (redemption cap,
	// EarnLoyaltyPoints below) is denominated in whole rupees by its own
	// rupees_per_point setting and is not part of this migration -
	// recomputing it from the precise paise total instead would shift
	// point-earning behavior, which is a separate decision.
	totalSalePaise, totalCostPaise := int64(0), int64(0)
	totalSaleRupees, totalCostRupees := 0, 0
	var costingGaps []CostingGapLine
	for i, item := range cart.Items {
		itemsInterface[i] = map[string]interface{}{"sku": item.Sku, "qty": -item.Qty}
		totalSalePaise += RupeesToPaise(item.SalePrice) * int64(item.Qty)
		// Stage 47.2.2: cost is resolved entirely server-side - a real
		// moving-average cost (engines/costing.go), then Item.standard_cost,
		// then zero. The client's cost_price, which Stage 37.3.3 had left as
		// the last-resort fallback, is gone: "never fall back to a client cost
		// when valuation history is absent" is the whole point of A-02's cost
		// half. A line the server genuinely cannot cost posts zero COGS and is
		// recorded as a POSCostingGap after the commit, so the understatement
		// is a visible, reviewable fact instead of a number the till invented.
		resolvedCostPaise, costResolved := ResolveQuoteUnitCostPaise(tenantID, item.Sku)
		if !costResolved {
			costingGaps = append(costingGaps, CostingGapLine{SKU: item.Sku, Qty: item.Qty, SaleValue: item.SalePrice * float64(item.Qty)})
		}
		totalCostPaise += resolvedCostPaise * int64(item.Qty)
		totalSaleRupees += int(item.SalePrice) * item.Qty
		totalCostRupees += int(PaiseToRupees(resolvedCostPaise)) * item.Qty
	}

	negativeEvents, ledgerLines, err := PostInventoryLedgerWithVoucherTx(tx, schema, cart.Location, itemsInterface, cart.OfflineSynced)
	if err != nil {
		return nil, fmt.Errorf("inventory decrement failed: %w", err)
	}

	// Loyalty redemption (Stage 30.2.5), now inside the sale's transaction.
	// RedeemLoyaltyPointsTx re-checks the balance against the ledger under a
	// row lock, so a balance that changed between adding the points to the
	// cart and completing the sale is caught rather than overdrawn - and a
	// rolled-back sale un-burns the points by definition, which is what
	// removed the compensating-reversal branches this function used to carry.
	loyaltyDiscount := 0
	if cart.RedeemPoints > 0 && cart.CustomerID != "" {
		loyaltyDiscount, err = RedeemLoyaltyPointsTx(tx, schema, tenantID, cart.CustomerID, cart.RedeemPoints, cartNumber)
		if err != nil {
			return nil, err
		}
		if loyaltyDiscount > totalSaleRupees {
			// Cap rather than reject: the points were already accepted, and a
			// customer covering more than the bill simply pays nothing. The
			// unused remainder goes straight back - in the same transaction,
			// so it can no longer be lost to a failed compensating write.
			if _, err := tx.Exec(fmt.Sprintf(`
				INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id)
				VALUES ($1, 'Earn', $2, 'POSCart', $3)`, schema),
				cart.CustomerID, loyaltyDiscount-totalSaleRupees, cartNumber+":REDEMPTION-REVERSAL"); err != nil {
				return nil, err
			}
			loyaltyDiscount = totalSaleRupees
		}
	}

	loyaltyDiscountPaise := RupeesToPaise(float64(loyaltyDiscount))
	if err := PostSalesFinanceBookingTx(tx, schema, tenantID, cartNumber, totalSalePaise, totalCostPaise, cart.PaymentMode, loyaltyDiscountPaise); err != nil {
		return nil, fmt.Errorf("GL booking failed: %v", err)
	}
	if err := PostSalesGSTBookingTx(tx, schema, tenantID, cartNumber, cart.GSTBreakdown); err != nil {
		return nil, fmt.Errorf("GST booking failed: %v", err)
	}
	// Stage 26.6.11: move any exempt/nil-rated/zero-rated turnover out of 4100
	// so it is not later reported as taxable value. A wholly taxable cart
	// no-ops here.
	if err := PostExemptSalesReclassTx(tx, schema, tenantID, cartNumber, cart.GSTBreakdown); err != nil {
		return nil, fmt.Errorf("exempt-sales reclass failed: %v", err)
	}

	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET status = 'Paid',
		   data = jsonb_set(data, '{payment_state}', to_jsonb('Posted'::text)),
		   updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber); err != nil {
		return nil, err
	}

	// Stage 47.2.3: an override is spent by the sale it priced, so a cart
	// number replayed later cannot silently re-use the same authorisation.
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.documents
		SET data = jsonb_set(data, '{status}', to_jsonb('Consumed'::text)),
		    status = 'Consumed', updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'POSPriceOverride' AND status = 'Approved' AND data->>'cart_number' = $1`, schema),
		cartNumber); err != nil {
		return nil, err
	}

	outcome := &FinalizeOutcome{
		SaleTotal:       PaiseToRupees(totalSalePaise),
		CostTotal:       PaiseToRupees(totalCostPaise),
		LoyaltyDiscount: loyaltyDiscount,
		NegativeEvents:  negativeEvents,
		CostingGaps:     costingGaps,
	}

	// 47.3.1: the idempotency record completes in the SAME transaction. This
	// is the whole mechanism - a committed sale always has a committed
	// Completed record, and an abandoned InProgress claim therefore proves
	// nothing was posted, which is what makes taking over an expired lease
	// safe rather than a gamble.
	if idempotencyKey != "" {
		if response == nil {
			response = map[string]interface{}{}
		}
		response["sale_total"] = outcome.SaleTotal
		response["loyalty_discount"] = outcome.LoyaltyDiscount
		if err := CompleteCommandTx(tx, schema, idempotencyKey, cartNumber, response); err != nil {
			return nil, err
		}
	}

	// 47.3.2's "audit/outbox evidence in the same transaction". PublishEvent
	// already takes a *sql.Tx (Stage 30.2.2's outbox), so a downstream
	// consumer can never see a sale event for a transaction that rolled back,
	// nor miss one for a sale that committed.
	if err := PublishEvent(tx, schema, "pos.sale.completed", map[string]interface{}{
		"cart_number": cartNumber, "location": cart.Location, "customer_id": cart.CustomerID,
		"sale_total": outcome.SaleTotal, "payment_mode": cart.PaymentMode,
	}); err != nil {
		return nil, fmt.Errorf("sale event could not be recorded: %v", err)
	}

	if ownTx {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}

	// --- everything below is post-commit and must never undo the sale -----

	WriteStockLedgerLines(tenantID, cart.Location, "POSInvoice", cartNumber, cashier, ledgerLines)

	if len(negativeEvents) > 0 {
		// 20.13 decision: an offline-synced sale already physically happened
		// (goods left the store, payment was taken) before the server could be
		// asked whether stock covered it - so the sale always posts, and any
		// resulting negative stock is recorded here for a manager to
		// review/reconcile (e.g. against the next GRN), never silently lost.
		recordOfflineSyncVariance(tenantID, cartNumber, negativeEvents)
	}
	for _, gap := range costingGaps {
		RecordCostingGap(tenantID, cartNumber, gap.SKU, gap.Qty, gap.SaleValue)
	}
	// Loyalty earn stays outside the transaction, same as before this refactor:
	// it is purely additive and must never undo an already-completed sale. The
	// earn base nets off anything paid for with points.
	if cart.CustomerID != "" {
		if errEarn := EarnLoyaltyPoints(tenantID, cart.CustomerID, totalSaleRupees-loyaltyDiscount, cartNumber); errEarn != nil {
			LogSystemError(tenantID, correlationID, "LOYALTY_EARN_FAILED", "/api/v1/checkout", errEarn.Error(), "")
		}
	}
	return outcome, nil
}

// outcomeFromStoredCart rebuilds the totals of a sale that is already Paid,
// for the "another request finalized it while we waited on the lock" branch.
// Reads the cart's own stored resolved prices - the same source the original
// finalization posted from - so a replay reports the figures that were really
// posted.
func outcomeFromStoredCart(dataStr string) (*FinalizeOutcome, error) {
	var cart posCart
	if err := json.Unmarshal([]byte(dataStr), &cart); err != nil {
		return nil, err
	}
	salePaise := int64(0)
	for _, item := range cart.Items {
		salePaise += RupeesToPaise(item.SalePrice) * int64(item.Qty)
	}
	return &FinalizeOutcome{SaleTotal: PaiseToRupees(salePaise)}, nil
}

// IsRetryableDBConflict reports whether an error is a Postgres serialization
// failure or deadlock rather than a business rejection (47.3.4).
//
// The distinction matters at the till: a serialization conflict means "two
// people touched the same row, try again and it will work", while an
// InsufficientStockError means "there is not enough stock, and retrying will
// never change that". Reporting the first as the second tells the cashier a
// lie about their own inventory.
//
// Matched on SQLSTATE text rather than a driver-specific error type because
// this codebase deliberately carries no pq/pgx error-type dependency beyond the
// driver registration itself.
func IsRetryableDBConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "40001") || // serialization_failure
		strings.Contains(msg, "40P01") || // deadlock_detected
		strings.Contains(msg, "could not serialize access") ||
		strings.Contains(msg, "deadlock detected")
}

// recordOfflineSyncVariance (20.13) writes one POSOfflineSyncVariance
// document per SKU that went negative when an offline-queued sale replayed
// against server-side stock. Registered read-only in doctype_meta (same
// "engine writes directly, no role gets a generic create grant" pattern as
// POSSession/PaymentProposal) so Store Manager/HR-Admin can browse the list
// via the ordinary generic doctype-table screen without any new frontend
// code. Best-effort: a failure here must never undo or block the sale
// itself, which has already committed by the time this runs - it only logs.
func recordOfflineSyncVariance(tenantID, cartNumber string, events []NegativeStockEvent) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	for _, ev := range events {
		id := NewDocID("POSSYNCVAR")
		data := map[string]interface{}{
			"cart_number":         cartNumber,
			"sku":                 ev.SKU,
			"location":            ev.LocationCode,
			"shortfall_qty":       ev.Shortfall,
			"resulting_available": ev.ResultingAvailable,
			"status":              "Open",
		}
		marshaled, err := json.Marshal(data)
		if err != nil {
			continue
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, 'POSOfflineSyncVariance', $2, 'Open', 'system')`, schema), id, marshaled); err != nil {
			LogSystemError(tenantID, "", "ERROR", "recordOfflineSyncVariance", fmt.Sprintf("failed to record offline sync variance for cart %s sku %s: %v", cartNumber, ev.SKU, err), "")
		}
	}
	// MOBILE-0176 (Stage 25.5): "Offline sync conflict" - an offline-queued
	// sale replaying against stock the server has since sold elsewhere is
	// exactly this scenario. The catalog marks it Blocking:true/409, but
	// 20.13's own design deliberately never blocks here (the sale already
	// physically happened before the server could be asked) - same
	// "don't reverse an already-deliberate workflow decision" reasoning
	// Stage 25 Batch 3 applied to SALESP-0123, so this is a log-only tag,
	// not a rejection.
	LogSystemError(tenantID, "", "Medium", "Mobile App / Device", fmt.Sprintf("[MOBILE-0176] offline-synced cart %s replayed with %d SKU(s) now short - recorded as POSOfflineSyncVariance for review", cartNumber, len(events)), "")
}
