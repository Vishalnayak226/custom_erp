package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
	Items        []posCartLine `json:"items"`
	GSTBreakdown GSTBreakdown  `json:"gst_breakdown"`
	// RedeemPoints (Stage 30.2.5) is the loyalty redemption the cashier
	// applied to this cart. It is an INTENT recorded on the cart, not an
	// already-burned balance: the burn happens at finalization, once the sale
	// is actually going through. Before this, "Redeem Points" burned the
	// points the instant it was clicked and left the cashier to type the
	// discount into a line's price by hand - so abandoning the cart lost the
	// customer's points outright, and forgetting to type the discount charged
	// them full price for a sale they had already paid for in points.
	RedeemPoints int `json:"redeem_points"`
	// Stage 73.1.4: what authorised the redemption - the verified one-time
	// code and the manager's approval, when the tenant's controls need them.
	RedemptionChallengeID string `json:"redemption_challenge_id"`
	RedemptionApprovalID  string `json:"redemption_approval_id"`
	// 20.13: stamped by handleCheckout from the client's own "offline_synced"
	// request field - true only when this cart is being replayed from a
	// cashier's offline queue after reconnecting, never for a normal live
	// sale. Read back here (rather than threaded as a parameter) so every
	// finalization path gets the same behavior for free with no call-site
	// change.
	OfflineSynced bool `json:"offline_synced"`
	// Consumption (Stage 72.3) is what the sale uses up with its lines -
	// resolved from the Backflush Rules and the cashier's Packaging choices by
	// handleCheckout and stored here, so an approval-gated or authorise-only
	// sale posts exactly what the cashier confirmed.
	Consumption []ConsumptionLine `json:"consumption"`
	// GiftCardTenders (Stage 72.8): gift cards paying part of the bill,
	// checked at checkout and redeemed in this transaction. OfferDiscount is
	// read only to cap them at what the customer actually owed.
	GiftCardTenders []GiftCardTender `json:"gift_card_tenders"`
	OfferDiscount   float64          `json:"offer_discount"`
	// Tenders (Stage 73.1.6) are the fixed parts of a split payment;
	// PaymentMode takes the rest - see settlePOSTenders.
	Tenders []POSTender `json:"tenders"`
	// ExchangeCreditID (Stage 73.2.6) is the credit an exchange's returned
	// items carry to this sale; POSSession is the till session it ran in.
	ExchangeCreditID string `json:"exchange_credit_id"`
	POSSession       string `json:"pos_session"`
	// AppliedOffers are the quote's; an issued voucher among them (Stage
	// 73.2.10) is redeemed in this transaction.
	AppliedOffers []AppliedOffer `json:"applied_offers"`
}

// posCartLine is one stored cart line.
type posCartLine struct {
	Sku       string  `json:"sku"`
	Qty       int     `json:"qty"`
	SalePrice float64 `json:"sale_price"`
	// OfferShare (Stage 73.1.1) is the quote's per-line share of the cart's
	// OfferDiscount - see cartOfferShares for carts stored without it.
	OfferShare float64 `json:"offer_share"`
}

// FinalizeOutcome is what one committed sale produced. Returned so the caller
// can build its response from the same numbers that were posted, and so the
// idempotency record can store that response verbatim for replay (47.3.1).
type FinalizeOutcome struct {
	// BillNumber (Stage 85.D1) is the store's server-issued bill number.
	BillNumber      string
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

	itemsInterface := make([]interface{}, 0, len(cart.Items))
	// Stage 72.9-72.11: what each line takes out of stock and what it costs
	// now depends on the item's policy - see saleStockIssue. A gift's cost
	// goes to its profile's expense account rather than COGS.
	giftCostByAccount := map[string]int64{}
	// Stage 45: paise totals for the ledger/receipt, computed straight from
	// each line's own float64 price - not from a whole-rupee int() truncated
	// before the qty multiply, which is the exact leak the 2026-07-31
	// durability audit's finding #7 named (a fractional-rupee item lost its
	// fraction on every unit sold, not just once per sale).
	//
	// Stage 73.1.1 retired the whole-rupee totals Stage 45 had kept for the
	// loyalty cap and the earn base: both now work from the net paise below,
	// so the points economy and the posted revenue are the same figure.
	totalSalePaise, totalCostPaise := int64(0), int64(0)
	var costingGaps []CostingGapLine
	for _, item := range cart.Items {
		issue := saleStockIssue(tenantID, item.Sku, item.Qty)
		itemsInterface = append(itemsInterface, issue.stock...)
		totalSalePaise += RupeesToPaise(item.SalePrice) * int64(item.Qty)
		if issue.handled {
			if issue.giftAccount != "" {
				giftCostByAccount[issue.giftAccount] += issue.costPaise
			} else {
				totalCostPaise += issue.costPaise
			}
			if !issue.costResolved {
				costingGaps = append(costingGaps, CostingGapLine{SKU: item.Sku, Qty: item.Qty, SaleValue: item.SalePrice * float64(item.Qty)})
			}
			continue
		}
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
	}

	// Stage 106.4: the sale takes the store's chosen "From" location first.
	if err := drawFromDefaultFromTx(tx, schema, "POS Sale", cart.Location, itemsInterface); err != nil {
		return nil, fmt.Errorf("inventory decrement failed: %w", err)
	}
	negativeEvents, ledgerLines, err := PostInventoryLedgerWithVoucherTx(tx, schema, cart.Location, itemsInterface, cart.OfflineSynced)
	if err != nil {
		return nil, fmt.Errorf("inventory decrement failed: %w", err)
	}

	// Stage 73.1.1: the bill's offer discount, line by line. Revenue, the
	// points cap, gift cards and the earn base all work from the net - what
	// the customer was actually charged - not the lines' list prices.
	lineSKUs := make([]string, len(cart.Items))
	lineQtys := make([]int, len(cart.Items))
	lineGross := make([]int64, len(cart.Items))
	storedOffer := make([]float64, len(cart.Items))
	for i, item := range cart.Items {
		lineSKUs[i], lineQtys[i], storedOffer[i] = item.Sku, item.Qty, item.OfferShare
		lineGross[i] = RupeesToPaise(item.SalePrice) * int64(item.Qty)
	}
	lineOffer := cartOfferShares(lineGross, storedOffer, cart.OfferDiscount)
	netSalePaise := totalSalePaise
	for _, o := range lineOffer {
		netSalePaise -= o
	}

	// Stage 73.2.10: an issued voucher the quote applied is spent here, with
	// the sale - locked, re-checked and counted - so it is used if and only if
	// the sale posts, and a voucher used up since the quote stops the sale.
	for _, o := range cart.AppliedOffers {
		if o.OfferType != OfferTypeVoucher || o.CouponCode == "" {
			continue
		}
		if _, err := redeemVoucherTx(tx, schema, o.CouponCode, cart.CustomerID, cartNumber, func(v Voucher) error {
			return checkVoucherUsable(v, cart.CustomerID, VoucherChannelPOS)
		}); err != nil {
			return nil, &ValidationError{Code: "POSOFF-0613", Message: fmt.Sprintf("%v - remove the code, or price the bill again", err)}
		}
	}

	// Loyalty redemption (Stage 30.2.5), now inside the sale's transaction.
	// RedeemLoyaltyPointsTx re-checks the balance under the customer's
	// loyalty lock, so a balance that changed between adding the points to
	// the cart and completing the sale is caught rather than overdrawn - and a
	// rolled-back sale un-burns the points by definition, which is what
	// removed the compensating-reversal branches this function used to carry.
	loyaltyDiscount, pointsUsed := 0, 0
	valuePerPoint := redemptionValuePerPointFor(tenantID)
	if valuePerPoint <= 0 {
		valuePerPoint = 1
	}
	if cart.RedeemPoints > 0 && cart.CustomerID != "" {
		loyaltyDiscount, err = RedeemLoyaltyPointsTx(tx, schema, tenantID, cart.CustomerID, cart.RedeemPoints, cartNumber,
			LoyaltyRedemptionAuth{ChallengeID: cart.RedemptionChallengeID, ApprovalID: cart.RedemptionApprovalID})
		if err != nil {
			return nil, err
		}
		pointsUsed = cart.RedeemPoints
		if RupeesToPaise(float64(loyaltyDiscount)) > netSalePaise {
			// Cap rather than reject: the points were already accepted, and a
			// customer covering more than the bill simply pays nothing. The
			// unused POINTS go straight back - in the same transaction, so they
			// can no longer be lost to a failed compensating write. 73.1.1: the
			// cap is the net bill, and what goes back is points, not rupees
			// (the two differ whenever a point is worth more than Rs 1).
			pointsUsed = int(netSalePaise / RupeesToPaise(float64(valuePerPoint)))
			if _, err := tx.Exec(fmt.Sprintf(`
				INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id)
				VALUES ($1, 'Earn', $2, 'POSCart', $3)`, schema),
				cart.CustomerID, cart.RedeemPoints-pointsUsed, cartNumber+":REDEMPTION-REVERSAL"); err != nil {
				return nil, err
			}
			loyaltyDiscount = pointsUsed * valuePerPoint
		}
	}

	loyaltyDiscountPaise := RupeesToPaise(float64(loyaltyDiscount))
	if err := PostSalesFinanceBookingTx(tx, schema, tenantID, cartNumber, netSalePaise, totalCostPaise, cart.PaymentMode, loyaltyDiscountPaise); err != nil {
		return nil, fmt.Errorf("GL booking failed: %v", err)
	}
	// Stage 73.2.12: when accruing, points redeemed release the liability.
	if err := postLoyaltyValueTx(tx, tenantID, schema, "POSCart", cartNumber, "POSCart:"+cartNumber+":LOYALTY_RELEASE", loyaltyDiscountPaise, false, accountLoyaltyRedeemedExp); err != nil {
		return nil, err
	}
	if err := PostSalesGSTBookingTx(tx, schema, tenantID, cartNumber, cart.GSTBreakdown); err != nil {
		return nil, fmt.Errorf("GST booking failed: %v", err)
	}
	// Stage 72.8: gift cards paying the bill - liability down, payment
	// clearing credited by the same amount, all or nothing with the sale.
	var giftTotal int64
	for _, t := range cart.GiftCardTenders {
		giftTotal += t.AmountPaise
	}
	if len(cart.GiftCardTenders) > 0 {
		if owed := netSalePaise - loyaltyDiscountPaise; giftTotal > owed {
			return nil, &ValidationError{Code: "POSOFF-0435", Message: fmt.Sprintf("gift cards cover %.2f but the bill comes to %.2f", PaiseToRupees(giftTotal), PaiseToRupees(owed))}
		}
		if err := redeemGiftCardTendersTx(tx, schema, tenantID, cartNumber, cart.Location, cashier, cart.PaymentMode, cart.GiftCardTenders); err != nil {
			return nil, err
		}
	}
	// Stage 73.2.6: an exchange's credit pays next; any of it the sale does
	// not need is paid back from the till.
	var exchangeUse int64
	if cart.ExchangeCreditID != "" {
		exchangeUse, _, err = useExchangeCreditTx(tx, tenantID, schema, cartNumber, cart.ExchangeCreditID, cart.PaymentMode, cart.POSSession,
			netSalePaise-loyaltyDiscountPaise-giftTotal)
		if err != nil {
			return nil, err
		}
	}
	// Stage 73.1.6: what is left after points and gift cards is paid in money,
	// split across the cart's tenders, each in its own clearing account.
	moneyParts, err := settlePOSTenders(cart.Tenders, cart.PaymentMode, netSalePaise-loyaltyDiscountPaise-giftTotal-exchangeUse)
	if err != nil {
		return nil, err
	}
	if err := postTenderReclassTx(tx, tenantID, schema, cartNumber, cart.PaymentMode, moneyParts); err != nil {
		return nil, err
	}
	// The credit is its own tender in the allocation (posted above, against
	// 2620), so a later return of these items refunds it like money.
	if exchangeUse > 0 {
		moneyParts = append([]tenderPart{{Key: TenderExchangeCredit, Paise: exchangeUse}}, moneyParts...)
	}
	// Stage 26.6.11: move any exempt/nil-rated/zero-rated turnover out of 4100
	// so it is not later reported as taxable value. A wholly taxable cart
	// no-ops here.
	if err := PostExemptSalesReclassTx(tx, schema, tenantID, cartNumber, cart.GSTBreakdown); err != nil {
		return nil, fmt.Errorf("exempt-sales reclass failed: %v", err)
	}
	// Stage 72.7: a gift handed over free is a promotional cost, not COGS.
	for _, acct := range sortedPaiseKeys(giftCostByAccount) {
		if giftCostByAccount[acct] <= 0 {
			continue
		}
		if err := PostDoubleEntryTx(tx, tenantID, schema, "POSCart", cartNumber,
			map[string]int64{acct: giftCostByAccount[acct]}, map[string]int64{"1200": giftCostByAccount[acct]},
			"", fmt.Sprintf("POSCart:%s:GIFT_COST:%s", cartNumber, acct)); err != nil {
			return nil, fmt.Errorf("gift cost posting failed: %v", err)
		}
	}
	// Stage 72.3: packaging and consumables the sale used, from the rules and
	// the cashier's choices stored on the cart at checkout.
	consumptionNegative, consumptionLedger, err := postSaleConsumptionTx(tx, schema, tenantID, cartNumber, cart.Location, cart.Consumption, cart.OfflineSynced)
	if err != nil {
		return nil, err
	}

	// Stage 73.1.1: the sale's allocation is stored with the sale it describes,
	// and (73.3.1) amount_paid - the net bill - which tiers, campaigns, RFM and
	// Customer 360 read as the customer's spend; nothing stored it before.
	// in the same transaction - a return, the drawer and the reports read it
	// rather than re-deriving it.
	allocation, err := json.Marshal(buildSaleAllocation(lineSKUs, lineQtys, lineGross, lineOffer,
		pointsUsed, RupeesToPaise(float64(valuePerPoint)), giftTotal, moneyParts))
	if err != nil {
		return nil, err
	}
	// Stage 85.D1: the bill number is drawn here, in the sale's transaction -
	// a sale that rolls back gives it back, so the store's series has no gap.
	billNumber, err := issuePOSBillNumberTx(tx, schema, cart.Location, time.Now())
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET status = 'Paid',
		   data = jsonb_set(data, '{payment_state}', to_jsonb('Posted'::text)) || jsonb_build_object('allocation', $2::jsonb, 'amount_paid', $3::numeric, 'bill_number', $4::text),
		   updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber, string(allocation), PaiseToRupees(netSalePaise), billNumber); err != nil {
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
		BillNumber:      billNumber,
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
		response["bill_number"] = outcome.BillNumber
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
	WriteStockLedgerLines(tenantID, cart.Location, "POSConsumption", cartNumber, cashier, consumptionLedger)
	for _, ev := range consumptionNegative {
		RecordItemException(tenantID, ItemExceptionInput{
			ProcessType: ItemExceptionShortStock, SourceDoctype: "POSCart", SourceID: cartNumber,
			SKU: ev.SKU, Location: ev.LocationCode, Code: "POSOFF-0416", Qty: ev.Shortfall,
			Message:  fmt.Sprintf("sale %s used %s the store did not have: stock is now %d - count it, or receive the missing quantity", cartNumber, ev.SKU, ev.ResultingAvailable),
			DedupKey: "short|" + cartNumber + "|" + ev.SKU,
		})
	}

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
	// earn base is what was paid in money: 73.1.1 nets off the offer as well
	// as anything paid for with points, matching what a return reverses.
	if cart.CustomerID != "" {
		if errEarn := EarnLoyaltyPoints(tenantID, cart.CustomerID, int(PaiseToRupees(netSalePaise-loyaltyDiscountPaise)), cartNumber); errEarn != nil {
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
