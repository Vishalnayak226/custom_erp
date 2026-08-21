package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// FinalizePOSCheckout runs the side effects of a completed sale - inventory
// decrement, GL/GST posting, loyalty earn - against a POSCart document that
// already exists in the documents table, and marks it Paid. Reads the cart's
// own stored data (captured by handleCheckout at request time) as the single
// source of truth, rather than taking items/totals as parameters, so it can
// be called identically from two places: handleCheckout's normal synchronous
// path, and handleDecideApproval once a manager approves a discount-gated
// cart (Stage 20.10) - the two can never compute different totals this way.
func FinalizePOSCheckout(tenantID, cartNumber, correlationID string) (saleTotal float64, costTotal float64, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}

	var dataStr, cashier string
	if err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, created_by FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber).Scan(&dataStr, &cashier); err != nil {
		return 0, 0, fmt.Errorf("cart not found: %v", err)
	}

	var cart struct {
		Location    string `json:"location"`
		PaymentMode string `json:"payment_mode"`
		CustomerID  string `json:"customer_id"`
		Items       []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
			CostPrice float64 `json:"cost_price"`
		} `json:"items"`
		GSTBreakdown GSTBreakdown `json:"gst_breakdown"`
		// RedeemPoints (Stage 30.2.5) is the loyalty redemption the cashier
		// applied to this cart. It is an INTENT recorded on the cart, not an
		// already-burned balance: the burn happens here, once the sale is
		// actually going through. Before this, "Redeem Points" burned the
		// points the instant it was clicked and left the cashier to type the
		// discount into a line's price by hand - so abandoning the cart lost
		// the customer's points outright, and forgetting to type the discount
		// charged them full price for a sale they had already paid for in
		// points.
		RedeemPoints int `json:"redeem_points"`
		// 20.13: stamped by handleCheckout from the client's own
		// "offline_synced" request field - true only when this cart is being
		// replayed from a cashier's offline queue after reconnecting, never
		// for a normal live sale. Read back here (rather than threaded as a
		// parameter) so both of FinalizePOSCheckout's callers - handleCheckout's
		// own synchronous path and handleDecideApproval's discount-approval
		// path - get the same behavior for free with no call-site change.
		OfflineSynced bool `json:"offline_synced"`
	}
	if err = json.Unmarshal([]byte(dataStr), &cart); err != nil {
		return 0, 0, err
	}

	markFailed := func() {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Failed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber)
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
	// ReverseLoyaltyRedemption, EarnLoyaltyPoints below) is denominated in
	// whole rupees by its own rupees_per_point setting and is not part of
	// this migration - recomputing it from the precise paise total instead
	// would shift point-earning behavior, which is a separate decision.
	totalSalePaise, totalCostPaise := int64(0), int64(0)
	totalSaleRupees, totalCostRupees := 0, 0
	for i, item := range cart.Items {
		itemsInterface[i] = map[string]interface{}{"sku": item.Sku, "qty": -item.Qty}
		totalSalePaise += RupeesToPaise(item.SalePrice) * int64(item.Qty)
		totalCostPaise += RupeesToPaise(item.CostPrice) * int64(item.Qty)
		totalSaleRupees += int(item.SalePrice) * item.Qty
		totalCostRupees += int(item.CostPrice) * item.Qty
	}

	negativeEvents, err := PostInventoryLedgerWithVoucher(tenantID, cart.Location, itemsInterface, cart.OfflineSynced, "POSInvoice", cartNumber, cashier)
	if err != nil {
		markFailed()
		return 0, 0, fmt.Errorf("inventory decrement failed: %v", err)
	}

	// Loyalty redemption (Stage 30.2.5). Burned here, at the point the sale is
	// really happening, rather than when the cashier clicked "Redeem Points".
	// RedeemLoyaltyPoints re-checks the balance against the ledger, so a
	// balance that changed between adding the points to the cart and
	// completing the sale is caught rather than overdrawn.
	//
	// Everything after this point that can fail reverses the burn first (see
	// failAndRefundPoints) - the ledger is append-only, so the reversal is a
	// compensating entry, not a delete.
	loyaltyDiscount := 0
	if cart.RedeemPoints > 0 && cart.CustomerID != "" {
		loyaltyDiscount, err = RedeemLoyaltyPoints(tenantID, cart.CustomerID, cart.RedeemPoints, cartNumber)
		if err != nil {
			markFailed()
			return 0, 0, err
		}
		if loyaltyDiscount > totalSaleRupees {
			// Cap rather than reject: the points were already accepted, and a
			// customer covering more than the bill simply pays nothing. The
			// unused remainder goes straight back.
			if errBack := ReverseLoyaltyRedemption(tenantID, cart.CustomerID, loyaltyDiscount-totalSaleRupees, cartNumber); errBack != nil {
				LogSystemError(tenantID, correlationID, "ERROR", "/api/v1/checkout",
					fmt.Sprintf("cart %s: could not return %d over-redeemed loyalty point(s): %v", cartNumber, loyaltyDiscount-totalSaleRupees, errBack), "")
			}
			loyaltyDiscount = totalSaleRupees
		}
	}
	failAndRefundPoints := func(wrapped error) (float64, float64, error) {
		if loyaltyDiscount > 0 {
			if errBack := ReverseLoyaltyRedemption(tenantID, cart.CustomerID, cart.RedeemPoints, cartNumber); errBack != nil {
				LogSystemError(tenantID, correlationID, "CRITICAL", "/api/v1/checkout",
					fmt.Sprintf("cart %s failed AND its %d redeemed loyalty point(s) could not be returned: %v", cartNumber, cart.RedeemPoints, errBack), "")
			}
		}
		markFailed()
		return 0, 0, wrapped
	}
	if len(negativeEvents) > 0 {
		// 20.13 decision: an offline-synced sale already physically happened
		// (goods left the store, payment was taken) before the server could
		// be asked whether stock covered it - so the sale always posts, and
		// any resulting negative stock is recorded here for a manager to
		// review/reconcile (e.g. against the next GRN), never silently lost.
		recordOfflineSyncVariance(tenantID, cartNumber, negativeEvents)
	}
	loyaltyDiscountPaise := RupeesToPaise(float64(loyaltyDiscount))
	if err = PostSalesFinanceBooking(tenantID, cartNumber, totalSalePaise, totalCostPaise, cart.PaymentMode, loyaltyDiscountPaise); err != nil {
		return failAndRefundPoints(fmt.Errorf("GL booking failed: %v", err))
	}
	if err = PostSalesGSTBooking(tenantID, cartNumber, cart.GSTBreakdown); err != nil {
		return failAndRefundPoints(fmt.Errorf("GST booking failed: %v", err))
	}
	// Stage 26.6.11: move any exempt/nil-rated/zero-rated turnover out of 4100
	// so it is not later reported as taxable value. In the same failure path as
	// the two postings above rather than the best-effort tail below, because a
	// sale whose revenue is misclassified in the GL produces a wrong GST
	// return - a filing problem, not a cosmetic one. A wholly taxable cart
	// no-ops here.
	if err = PostExemptSalesReclass(tenantID, cartNumber, cart.GSTBreakdown); err != nil {
		return failAndRefundPoints(fmt.Errorf("exempt-sales reclass failed: %v", err))
	}

	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Paid', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber)

	// Loyalty earn stays outside the failure path above, same as before this
	// refactor: it's purely additive and must never undo an already-completed
	// sale. The earn base nets off anything paid for with points, which is
	// what EarnLoyaltyPoints' own contract already asked its callers for -
	// there was simply no redemption on a cart to net off until Stage 30.2.5.
	if cart.CustomerID != "" {
		if errEarn := EarnLoyaltyPoints(tenantID, cart.CustomerID, totalSaleRupees-loyaltyDiscount, cartNumber); errEarn != nil {
			LogSystemError(tenantID, correlationID, "LOYALTY_EARN_FAILED", "/api/v1/checkout", errEarn.Error(), "")
		}
	}

	return PaiseToRupees(totalSalePaise), PaiseToRupees(totalCostPaise), nil
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
