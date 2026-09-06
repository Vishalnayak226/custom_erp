package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Stage 26.12.5 (Returns/RTO/QC/Refund): a request/approval-gated workflow
// distinct from the pre-existing ProcessReturnAnywhere (engines/fulfillment.go,
// deliberately left untouched) - that stays the POS in-store walk-in-return
// path (a customer with the item and receipt in hand, instant receive+
// restock+refund is the right shape there). This file is the OMS/e-commerce
// path: a customer-initiated return request OR a courier RTO
// (request_type distinguishes them) goes through Requested -> Approved ->
// Received -> QC Complete, with QC assigning a disposition per line that
// drives which Stage 26.12.6 inventory bucket the qty lands in, and a
// refund computed from the *original order line's* price (never the
// return-time input) that only becomes a GL post once its own RefundRequest
// is separately approved and processed - the checklist's own "distinct from
// the immediate GL post" requirement.

// ReturnItemInput is one requested return line - qty is the caller's ask;
// original_unit_price/original_cost_price are always re-resolved from the
// origin document (POSCart for a Customer Return, SalesOrderLine for an
// RTO), never trusted from the caller, per the design note (§12 of
// docs/specs/oms_master_blueprint_reference.md): "compute refund amount
// from the original order line's price, not the return-time price".
type ReturnItemInput struct {
	SKU string
	Qty int
}

// returnItemRecord is the persisted shape of one ReturnRequest line -
// Disposition starts blank and is filled in by ApplyReturnQC.
type returnItemRecord struct {
	SKU               string  `json:"sku"`
	Qty               int     `json:"qty"`
	OriginalUnitPrice float64 `json:"original_unit_price"`
	OriginalCostPrice float64 `json:"original_cost_price"`
	Disposition       string  `json:"disposition"`
	// ExchangeSKU (Stage 35.9.2) is set only when this line was resolved as
	// an exchange rather than a refund - see ApplyReturnQC's own comment.
	ExchangeSKU string `json:"exchange_sku,omitempty"`
}

// originalLinePrice is what resolveOriginalSaleLinePrices/
// resolveSalesOrderLinePrices resolve per SKU - never provided by the
// return-time caller.
type originalLinePrice struct {
	SalePrice float64
	CostPrice float64
}

// returnDispositionRule maps each of the checklist's six QC disposition
// buckets (Sellable/Damaged/Repairable/Missing/Wrong-Item/Rejected) to
// whether stock is physically received at all, which Stage 26.12.6
// inventory_availability bucket it lands in, and whether that line is
// refund-eligible. Explicit, documented decision (per the design note's own
// "decide explicitly... don't inherit silently" caution): Sellable/Damaged/
// Repairable all mean the customer genuinely returned the ordered item (the
// loss on a damaged/repairable unit is absorbed as non-sellable stock, not
// pushed onto the customer), so all three refund in full; Missing/
// Wrong-Item/Rejected mean the correct item was never actually received
// back, so none of those refund - the retired prototype's "one QC-failed
// line holds the whole refund" behavior is deliberately NOT inherited here,
// this decides per-line instead, matching the blueprint's own line-level-
// refund principle.
var returnDispositionRule = map[string]struct {
	ReceivesStock  bool
	Bucket         string // "available" | "damaged" | "qc_hold" | "" (none)
	RefundEligible bool
}{
	"Sellable":   {true, "available", true},
	"Damaged":    {true, "damaged", true},
	"Repairable": {true, "qc_hold", true},
	"Missing":    {false, "", false},
	"Wrong-Item": {true, "qc_hold", false},
	"Rejected":   {false, "", false},
}

// resolveOriginalSaleLinePrices reads a POSCart's own stored sale_price per
// SKU - resolveOriginalSale (engines/fulfillment.go) only captures sku/qty via
// transferLine, which is enough for the SALESR-0129/0130/0131 window/qty
// checks this function's caller reuses but not enough to price a refund
// correctly. A SalesInvoice-only reference (no per-line data at all, see
// resolveOriginalSale's own comment) resolves to an empty map - those lines
// simply have no resolvable original price.
//
// Stage 47.4.2: COST no longer comes from the cart at all. Stage 47.2.2
// removed cost_price from what a cart stores - it was the browser's own
// figure - so reading it here would resolve every returned line to a zero cost
// basis and silently post no COGS reversal, which is exactly what the 47.4.7
// inspection test caught. Cost is resolved the same server-authoritative way
// the sale itself resolved it (ResolveQuoteUnitCostPaise: moving-average cost,
// then Item.standard_cost, then zero), so a sale and its return agree on what
// the goods cost by construction rather than by both trusting the same
// client-supplied number.
func resolveOriginalSaleLinePrices(tenantID, orderID string) (map[string]originalLinePrice, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1 AND status = 'Paid'`, schema),
		orderID).Scan(&dataStr)
	if err == sql.ErrNoRows {
		return map[string]originalLinePrice{}, nil
	} else if err != nil {
		return nil, err
	}
	var cart struct {
		Items []struct {
			Sku       string  `json:"sku"`
			SalePrice float64 `json:"sale_price"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(dataStr), &cart); err != nil {
		return nil, err
	}
	prices := map[string]originalLinePrice{}
	for _, l := range cart.Items {
		costPaise, _ := ResolveQuoteUnitCostPaise(tenantID, l.Sku)
		prices[l.Sku] = originalLinePrice{SalePrice: l.SalePrice, CostPrice: PaiseToRupees(costPaise)}
	}
	return prices, nil
}

// resolveSalesOrderLinePrices reads a SalesOrder's own SalesOrderLine rows
// for RTO pricing - SalesOrderLine has no cost_price field (only
// unit_price), so CostPrice stays 0 for every RTO-sourced line; the
// inventory-side GL reversal in ApplyReturnQC simply posts 0 for those, a
// documented limitation matching this repo's other RTO scope notes (no
// per-batch cost capture exists yet - engines/marketplace.go's own header).
func resolveSalesOrderLinePrices(tenantID, orderID string) (map[string]originalLinePrice, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT data->>'sku', COALESCE((data->>'unit_price')::numeric, 0) FROM %s.documents
		 WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL`, schema), orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	prices := map[string]originalLinePrice{}
	for rows.Next() {
		var sku string
		var price float64
		if err := rows.Scan(&sku, &price); err != nil {
			return nil, err
		}
		prices[sku] = originalLinePrice{SalePrice: price}
	}
	return prices, rows.Err()
}

// The per-original-order return pool used to be summed by a pair of helpers
// here and in engines/fulfillment.go, read outside any transaction and then
// acted on. Stage 47.4.3 replaced both with assertReturnEligibleTx
// (engines/returns_atomic.go), which sums the same two document families
// INSIDE the transaction that holds the original sale locked - the read and
// the insert were the race, not the arithmetic.

// preparedReturnRequest is everything CreateReturnRequest resolves BEFORE it
// needs a lock: the request type, the resolved original order, the priced
// line records and the routed return location.
//
// Stage 47.4.3 split this out of CreateReturnRequest so the cumulative-quantity
// check - the only part that genuinely races - can run inside a transaction
// holding a lock on the original sale, while everything above it (which reads
// immutable or slow-moving data) stays outside and keeps the lock short.
type preparedReturnRequest struct {
	requestType     string
	originalOrderID string
	returnLocation  string
	autoRouted      bool
	records         []returnItemRecord
	soldBySku       map[string]int
	interstate      bool
	// skipEligibility is set only by a No Receipt exception (47.4.5): there is
	// no original sale to check cumulative quantity against, which is the
	// whole nature of that case. It is never set by ordinary validation.
	skipEligibility bool
}

// prepareReturnRequest runs every validation that does not need serialization:
// request type, original-bill existence, return window, RTO booking state,
// return routing, and the server-side price resolution. It deliberately does
// NOT check cumulative returned quantity - that is assertReturnEligibleTx's
// job, under the lock (engines/returns_atomic.go).
func prepareReturnRequest(tenantID, schema, requestType, returnLocation, originalOrderID, bookingID string, items []ReturnItemInput, exception *ReturnException) (*preparedReturnRequest, error) {
	if requestType != "Customer Return" && requestType != "RTO" {
		return nil, fmt.Errorf("request_type must be 'Customer Return' or 'RTO', got %q", requestType)
	}
	if len(items) == 0 {
		return nil, errors.New("at least one item is required")
	}

	out := &preparedReturnRequest{requestType: requestType, originalOrderID: originalOrderID, soldBySku: map[string]int{}}
	priceBySku := map[string]originalLinePrice{}
	// Stage 35.9.3 (return routing): populated only for an RTO, whose
	// LogisticsBooking already carries the pincode the parcel was being
	// delivered to - the one pincode source this workflow has. A Customer
	// Return's original order is a POSCart/SalesInvoice (resolveOriginalSale,
	// engines/fulfillment.go), neither of which carries a shipping address, so
	// auto-routing below simply has nothing to route from there and that path
	// keeps requiring an explicit return_location, same as today.
	rtoDestinationPincode := ""
	var err error

	switch requestType {
	case "Customer Return":
		// 47.4.5 No Receipt: there is deliberately no bill to resolve. The
		// line is priced from the item master instead, which is the honest
		// basis when nobody can say what was actually paid - and the
		// difference from a receipted return is recorded on the document, not
		// hidden by making an unpriced return look like a priced one.
		if exception != nil && exception.Type == ReturnExceptionNoReceipt {
			out.skipEligibility = true
			for _, it := range items {
				salePrice, mrp := itemMasterPrices(tenantID, it.SKU)
				if salePrice <= 0 {
					salePrice = mrp
				}
				priceBySku[it.SKU] = originalLinePrice{SalePrice: salePrice}
			}
			break
		}
		if originalOrderID == "" {
			return nil, errors.New("original_order_id is required for a Customer Return")
		}
		soldLines, saleDate, found, errResolve := resolveOriginalSale(tenantID, originalOrderID)
		if errResolve != nil {
			return nil, errResolve
		}
		if !found {
			return nil, &ValidationError{Code: "SALESR-0131", Message: fmt.Sprintf("no original bill found for %q - a return requires a valid original bill reference. A supervisor may still take it back as a No Receipt exception.", originalOrderID)}
		}
		returnWindowDays := salesReturnWindowDaysFor(tenantID)
		// 47.4.5 Goodwill: a supervisor may accept a return past the window.
		// The window check is the ONLY thing it waives - cumulative eligibility
		// still holds, because "we will take this back as a gesture" is not the
		// same as "you may return more than you bought".
		goodwill := exception != nil && exception.Type == ReturnExceptionGoodwill
		if !goodwill && !saleDate.IsZero() && time.Since(saleDate) > time.Duration(returnWindowDays)*24*time.Hour {
			return nil, &ValidationError{Code: "SALESR-0129", Message: fmt.Sprintf("return is not allowed more than %d days after the original sale (%s). A supervisor may still accept it as a Goodwill exception.", returnWindowDays, saleDate.Format("2006-01-02"))}
		}
		for _, l := range soldLines {
			out.soldBySku[l.Sku] += l.Qty
		}
		priceBySku, err = resolveOriginalSaleLinePrices(tenantID, originalOrderID)
		if err != nil {
			return nil, err
		}
		out.interstate = originalSaleWasInterstate(tenantID, originalOrderID)

	case "RTO":
		if bookingID == "" {
			return nil, errors.New("booking_id is required for an RTO return request")
		}
		_, bookingData, bookingStatus, errB := fetchLogisticsBooking(tenantID, bookingID)
		if errB != nil {
			return nil, errB
		}
		if bookingStatus != "RTO" {
			return nil, fmt.Errorf("booking %s is not marked RTO (currently %s) - call RecordRTO first", bookingID, bookingStatus)
		}
		var existing string
		errDup := db.DB.QueryRow(fmt.Sprintf(
			`SELECT id FROM %s.documents WHERE doctype = 'ReturnRequest' AND data->>'booking_id' = $1 AND data->>'status' != 'Rejected' AND deleted_at IS NULL LIMIT 1`, schema),
			bookingID).Scan(&existing)
		if errDup == nil {
			return nil, &duplicateRTOError{existingID: existing}
		} else if errDup != sql.ErrNoRows {
			return nil, errDup
		}
		out.originalOrderID, _ = bookingData["order_id"].(string)
		if out.originalOrderID != "" {
			priceBySku, err = resolveSalesOrderLinePrices(tenantID, out.originalOrderID)
			if err != nil {
				return nil, err
			}
		}
		rtoDestinationPincode, _ = bookingData["destination_pincode"].(string)
	}

	// Stage 35.9.3: return routing. An explicit return_location always wins
	// (unchanged from before this stage); only when the caller leaves it blank
	// does this resolve one automatically, reusing engines/sourcing.go's own
	// Nearest-Pincode strategy - the same distance-proxy logic
	// ResolveAllocationPlan already uses to source an order, run in reverse to
	// decide where a return should land.
	out.returnLocation = returnLocation
	if out.returnLocation == "" && rtoDestinationPincode != "" {
		routingItems := make([]map[string]interface{}, len(items))
		for i, it := range items {
			routingItems[i] = map[string]interface{}{"sku": it.SKU, "qty": it.Qty}
		}
		if loc, ok, errRoute := singleLocationNearestPincode(schema, rtoDestinationPincode, routingItems); errRoute == nil && ok {
			out.returnLocation = loc
			out.autoRouted = true
		}
	}
	if out.returnLocation == "" {
		return nil, errors.New("return_location is required (no return location could be auto-routed for this request)")
	}

	out.records = make([]returnItemRecord, len(items))
	for i, it := range items {
		if it.SKU == "" || it.Qty <= 0 {
			return nil, fmt.Errorf("each item requires a non-empty sku and a positive qty")
		}
		// 47.4.2: price and cost basis come from the immutable original sale
		// lines, never from the caller. A SKU that was not on the original
		// bill resolves to a zero-price record, which the eligibility check
		// under the lock then rejects outright.
		p := priceBySku[it.SKU]
		out.records[i] = returnItemRecord{SKU: it.SKU, Qty: it.Qty, OriginalUnitPrice: p.SalePrice, OriginalCostPrice: p.CostPrice}
	}
	return out, nil
}

// duplicateRTOError signals that an RTO return already exists for a booking -
// not an error condition, but not a fresh creation either. CreateReturnRequest
// unwraps it back into its historical "return the existing id" behaviour.
type duplicateRTOError struct{ existingID string }

func (e *duplicateRTOError) Error() string {
	return fmt.Sprintf("an RTO return request (%s) already exists for this booking", e.existingID)
}

// originalSaleWasInterstate reads the tax basis the original sale was booked
// under, so a return reverses the tax the way it was actually charged rather
// than the way it would be charged today. A sale with no stored breakdown (a
// SalesInvoice, or a cart from before Stage 17.5) reads as intrastate, which
// is this codebase's own default.
func originalSaleWasInterstate(tenantID, originalOrderID string) bool {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var interstate sql.NullBool
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT (data->'gst_breakdown'->>'interstate')::boolean FROM %s.documents WHERE id = $1`, schema),
		originalOrderID).Scan(&interstate)
	return interstate.Valid && interstate.Bool
}

// CreateReturnRequest is the workflow's entry point for both request types.
// For a Customer Return it reuses resolveOriginalSale's window/qty checks
// (SALESR-0129/0130/0131), plus this file's own sumPriorReturnRequests pool.
// For an RTO it requires the LogisticsBooking (Stage 26.12.4) to already be in
// the 'RTO' status RecordRTO sets, and is idempotent on booking_id (replaying
// an RTO webhook/call returns the existing non-Rejected request rather than
// duplicating it). Returns the new ReturnRequest's id.
//
// Stage 47.4.3: this now delegates to the same locked, transactional path
// CreateReturnRequestCommand uses (engines/returns_atomic.go), so the
// cumulative-quantity check can no longer be defeated by two concurrent
// callers - previously it read the prior returns and then inserted with
// nothing in between. It keeps its original signature and its RTO
// deduplication for the internal/webhook callers that already have their own
// idempotency; a caller that can retry (the POS, the API) should use
// CreateReturnRequestCommand and pass a key.
func CreateReturnRequest(tenantID, requestType, returnLocation, originalOrderID, bookingID, requestedBy string, items []ReturnItemInput) (string, error) {
	returnID, err := createReturnRequestLocked(tenantID, requestType, returnLocation, originalOrderID, bookingID, requestedBy, "", items, nil)
	if err != nil {
		var dup *duplicateRTOError
		if errors.As(err, &dup) {
			return dup.existingID, nil
		}
		return "", err
	}
	return returnID, nil
}

func fetchReturnRequest(tenantID, returnRequestID string) (schema string, data map[string]interface{}, err error) {
	schema, err = db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var dataBytes []byte
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'ReturnRequest' AND id = $1 AND deleted_at IS NULL`, schema),
		returnRequestID).Scan(&dataBytes)
	if err != nil {
		return "", nil, fmt.Errorf("return request %s not found: %v", returnRequestID, err)
	}
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return "", nil, err
	}
	return schema, data, nil
}

func saveReturnRequest(schema, returnRequestID string, data map[string]interface{}, status string) error {
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ReturnRequest' AND id = $3`, schema),
		marshaled, status, returnRequestID)
	return err
}

// ApproveReturnRequest is the workflow's approval-before-receipt gate -
// nothing is received or QC'd until a request has moved past Requested.
func ApproveReturnRequest(tenantID, returnRequestID, approvedBy string) error {
	schema, data, err := fetchReturnRequest(tenantID, returnRequestID)
	if err != nil {
		return err
	}
	if data["status"] != "Requested" {
		return fmt.Errorf("return request %s is not Requested (currently %v)", returnRequestID, data["status"])
	}
	data["status"] = "Approved"
	data["approved_by"] = approvedBy
	if err := saveReturnRequest(schema, returnRequestID, data, "Approved"); err != nil {
		return err
	}
	originalOrderID, _ := data["original_order_id"].(string)
	DispatchNotification(tenantID, "Return Approved", originalOrderID, map[string]string{"return_request_id": returnRequestID})
	return nil
}

// RejectReturnRequest requires a mandatory, category-matched ReasonCode
// (Stage 26.12.9's 'Return' category, already present in the foundation
// migration), the same convention 26.12.1's Hold/Cancel and 26.12.3's
// short-pick actions already use.
func RejectReturnRequest(tenantID, returnRequestID, reasonCode, rejectedBy string) error {
	schema, data, err := fetchReturnRequest(tenantID, returnRequestID)
	if err != nil {
		return err
	}
	status, _ := data["status"].(string)
	if status != "Requested" && status != "Approved" {
		return fmt.Errorf("return request %s cannot be rejected from status %q", returnRequestID, status)
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Return"); err != nil {
		return err
	}
	data["status"] = "Rejected"
	data["rejection_reason"] = reasonCode
	data["approved_by"] = rejectedBy
	if err := saveReturnRequest(schema, returnRequestID, data, "Rejected"); err != nil {
		return err
	}
	originalOrderID, _ := data["original_order_id"].(string)
	DispatchNotification(tenantID, "Return Rejected", originalOrderID, map[string]string{"return_request_id": returnRequestID, "reason_code": reasonCode})
	return nil
}

// ReceiveReturnRequest marks the goods as physically arrived - a distinct
// step from QC (ApplyReturnQC below), per the checklist's own "request/
// approval step before receipt" wording implying receipt and disposition
// are separate moments, not simultaneous.
func ReceiveReturnRequest(tenantID, returnRequestID, receivedBy string) error {
	schema, data, err := fetchReturnRequest(tenantID, returnRequestID)
	if err != nil {
		return err
	}
	// "Pickup Scheduled" (Stage 35.9.1) is the courier-reverse-pickup path's
	// own extra step between Approved and Received - a manual receipt is
	// still allowed from it (the operator physically has the parcel before
	// the tracking webhook catches up), same as receiving straight from
	// Approved always has been for a walk-in/self-shipped return.
	status, _ := data["status"].(string)
	if status != "Approved" && status != "Pickup Scheduled" {
		return fmt.Errorf("return request %s is not Approved (currently %v)", returnRequestID, data["status"])
	}
	data["status"] = "Received"
	return saveReturnRequest(schema, returnRequestID, data, "Received")
}

// applyReturnedStockToBucket increments on_hand plus exactly one of
// available/damaged/qc_hold, per returnDispositionRule - the same
// ON CONFLICT upsert shape ProcessReturnAnywhere already uses, extended to
// the Stage 26.12.6 buckets so a Damaged/Repairable/Wrong-Item receipt
// raises on_hand without raising available (computeATS, engines/inventory.go,
// stays correct: on_hand moves, but ATS itself nets to zero for a bucket
// addition since available/damaged/qc_hold are all separately subtracted).
func applyReturnedStockToBucket(tx *sql.Tx, schema, sku, locationCode string, qty int, bucket string) error {
	available, damaged, qcHold := 0, 0, 0
	switch bucket {
	case "available":
		available = qty
	case "damaged":
		damaged = qty
	case "qc_hold":
		qcHold = qty
	}
	_, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available, damaged, qc_hold)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (sku, location_code) DO UPDATE SET
			on_hand = %s.inventory_availability.on_hand + EXCLUDED.on_hand,
			available = %s.inventory_availability.available + EXCLUDED.available,
			damaged = %s.inventory_availability.damaged + EXCLUDED.damaged,
			qc_hold = %s.inventory_availability.qc_hold + EXCLUDED.qc_hold,
			updated_at = CURRENT_TIMESTAMP`, schema, schema, schema, schema, schema),
		sku, locationCode, qty, available, damaged, qcHold)
	return err
}

// ApplyReturnQC assigns a disposition per SKU line (dispositions keyed by
// SKU - one disposition per line, not sub-split within a line, a documented
// simplification for this effort tier), places physically-received stock
// into the matching inventory bucket, and creates a RefundRequest (Stage
// 26.12.5's own doctype, distinct from any GL post - ProcessRefundRequest
// below is where the refund itself posts) for the sum of refund-eligible
// lines. The inventory-side GL reversal (debit Inventory Control / credit
// COGS) posts here, at the point stock is actually received back into a
// bucket, independent of whether the customer's refund is later approved -
// see ProcessRefundRequest's own comment for why the revenue-side post is
// deliberately kept separate. A return with nothing refund-eligible closes
// immediately (no RefundRequest is created for a zero amount).
//
// exchangeFor (Stage 35.9.2, variadic so every existing call site is
// untouched) is an optional originalSKU -> desiredExchangeSKU map. A line
// named there is resolved as a same-value swap instead of a refund: the
// exchange SKU is picked from the same return_location stock the returned
// item just landed in, in the SAME transaction, so a shortage on the
// exchange side rolls back the whole QC call - nothing is received without
// its replacement actually being available, and nothing is issued without
// the original actually coming back. This build deliberately only supports
// an equal-value exchange (no price lookup exists on the Item master to
// price a different-value swap correctly - the same missing-cost-master gap
// resolveSalesOrderLinePrices' own comment already documents for RTO lines);
// a different-value swap is refused with a message pointing at return +
// new sale instead.
func ApplyReturnQC(tenantID, returnRequestID string, dispositions map[string]string, qcBy string, exchangeFor ...map[string]string) (totalRefund float64, refundRequestID string, err error) {
	schema, data, err := fetchReturnRequest(tenantID, returnRequestID)
	if err != nil {
		return 0, "", err
	}
	if data["status"] != "Received" {
		return 0, "", fmt.Errorf("return request %s is not Received (currently %v)", returnRequestID, data["status"])
	}
	returnLocation, _ := data["return_location"].(string)

	itemsRaw, err := json.Marshal(data["items"])
	if err != nil {
		return 0, "", err
	}
	var items []returnItemRecord
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		return 0, "", err
	}
	if len(items) == 0 {
		return 0, "", fmt.Errorf("return request %s has no items", returnRequestID)
	}

	exchanges := map[string]string{}
	if len(exchangeFor) > 0 {
		exchanges = exchangeFor[0]
	}
	for original, exchangeSKU := range exchanges {
		if exchangeSKU == "" || exchangeSKU == original {
			return 0, "", fmt.Errorf("exchange sku for %q must be a non-empty, different sku", original)
		}
		found := false
		for _, it := range items {
			if it.SKU == original {
				found = true
				break
			}
		}
		if !found {
			return 0, "", fmt.Errorf("exchange requested for sku %q, which is not on return request %s", original, returnRequestID)
		}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, "", err
	}

	var refundTotal, costReceivedTotal, exchangeCostTotal float64
	for i := range items {
		disposition := dispositions[items[i].SKU]
		rule, ok := returnDispositionRule[disposition]
		if !ok {
			return 0, "", fmt.Errorf("a valid disposition is required for sku %q (must be one of Sellable/Damaged/Repairable/Missing/Wrong-Item/Rejected)", items[i].SKU)
		}
		items[i].Disposition = disposition
		if rule.ReceivesStock {
			if err := applyReturnedStockToBucket(tx, schema, items[i].SKU, returnLocation, items[i].Qty, rule.Bucket); err != nil {
				return 0, "", err
			}
			costReceivedTotal += items[i].OriginalCostPrice * float64(items[i].Qty)
		}

		if exchangeSKU, wantsExchange := exchanges[items[i].SKU]; wantsExchange {
			if !rule.ReceivesStock {
				return 0, "", fmt.Errorf("sku %q was dispositioned %q (the original item was not actually received back) and cannot be exchanged", items[i].SKU, disposition)
			}
			var exists bool
			if err := tx.QueryRow(fmt.Sprintf(
				`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1 AND status != 'Cancelled' AND deleted_at IS NULL)`, schema),
				exchangeSKU).Scan(&exists); err != nil {
				return 0, "", err
			}
			if !exists {
				return 0, "", fmt.Errorf("exchange sku %q is not a valid active Item", exchangeSKU)
			}
			if err := deductExchangeStock(tx, schema, exchangeSKU, returnLocation, items[i].Qty, returnRequestID, qcBy); err != nil {
				return 0, "", fmt.Errorf("exchange for sku %q: %v", items[i].SKU, err)
			}
			items[i].ExchangeSKU = exchangeSKU
			// Same documented cost-basis simplification as OriginalCostPrice
			// itself: no Item-master cost field exists to look up the exchange
			// SKU's own cost, so this build uses the returned line's own cost as
			// the exchange leg's COGS basis too, correct for the common
			// same-family swap (size/colour) this feature targets.
			exchangeCostTotal += items[i].OriginalCostPrice * float64(items[i].Qty)
			continue
		}

		if rule.RefundEligible {
			refundTotal += items[i].OriginalUnitPrice * float64(items[i].Qty)
		}
	}

	finalStatus := "QC Complete"
	if refundTotal <= 0 {
		finalStatus = "Closed"
	}
	data["items"] = items
	data["status"] = finalStatus
	data["total_refund_eligible"] = refundTotal
	marshaled, err := json.Marshal(data)
	if err != nil {
		return 0, "", err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ReturnRequest' AND id = $3`, schema),
		marshaled, finalStatus, returnRequestID); err != nil {
		return 0, "", err
	}

	if refundTotal > 0 {
		refundRequestID = NewDocID("RF")
		refundDoc := map[string]interface{}{
			"code": refundRequestID, "return_request_id": returnRequestID, "amount": refundTotal,
			"status": "Pending", "refund_method": "", "approved_by": "", "processed_by": "", "rejection_reason": "",
		}
		refundMarshaled, err := json.Marshal(refundDoc)
		if err != nil {
			return 0, "", err
		}
		if _, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'RefundRequest', $2, 'Pending', 'system')`, schema),
			refundRequestID, refundMarshaled); err != nil {
			return 0, "", err
		}
	}

	// Stage 47.4.4: both GL legs post INSIDE this transaction, not after it.
	// Before this they ran after tx.Commit(), so a failure between the two
	// left stock physically received with no COGS reversal against it - the
	// same split-commit shape audit finding A-03 named for checkout, applied
	// to returns. Both now also carry a real posting key, so a retried QC
	// cannot double-post (the COGS leg had no key at all).
	if costReceivedTotal > 0 {
		inventoryDebits := map[string]int64{"1200": RupeesToPaise(costReceivedTotal)}
		inventoryCredits := map[string]int64{"5100": RupeesToPaise(costReceivedTotal)}
		if err := PostDoubleEntryTx(tx, tenantID, schema, "ReturnRequest", returnRequestID, inventoryDebits, inventoryCredits, "",
			fmt.Sprintf("ReturnRequest:%s:COGS_REVERSAL", returnRequestID)); err != nil {
			return 0, "", err
		}
	}
	if exchangeCostTotal > 0 {
		// Mirror image of the block above: stock physically left in the
		// exchange, so Inventory Control is credited and COGS debited this
		// time, for the same reused cost basis.
		exchangeDebits := map[string]int64{"5100": RupeesToPaise(exchangeCostTotal)}
		exchangeCredits := map[string]int64{"1200": RupeesToPaise(exchangeCostTotal)}
		if err := PostDoubleEntryTx(tx, tenantID, schema, "ReturnRequest", returnRequestID, exchangeDebits, exchangeCredits, "",
			fmt.Sprintf("ReturnRequest:%s:EXCHANGE", returnRequestID)); err != nil {
			return 0, "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, "", err
	}

	return refundTotal, refundRequestID, nil
}

// deductExchangeStock issues the exchange SKU out of return_location's own
// stock, inside the caller's transaction - the same ATS-check-then-ledger-
// entry shape engines/bundles.go's applyBundleAssemblyMovements already uses
// for a stocked kit's own outbound leg, reused here rather than reinvented.
func deductExchangeStock(tx *sql.Tx, schema, sku, locationCode string, qty int, returnRequestID, userID string) error {
	var available, reserved, safety, blocked, qc, damaged, buffer, held int
	err := tx.QueryRow(fmt.Sprintf(
		`SELECT available,reserved,safety_stock,blocked,qc_hold,damaged,channel_buffer,hold_qty
		 FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
		sku, locationCode).Scan(&available, &reserved, &safety, &blocked, &qc, &damaged, &buffer, &held)
	if err == sql.ErrNoRows {
		return fmt.Errorf("insufficient stock for exchange sku %s at %s: no inventory record", sku, locationCode)
	}
	if err != nil {
		return err
	}
	ats := computeATS(available, reserved, safety, blocked, qc, damaged, buffer, held)
	if ats < qty {
		return fmt.Errorf("insufficient ATS for exchange sku %s at %s: ATS %d, requested %d", sku, locationCode, ats, qty)
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.inventory_availability SET on_hand = on_hand - $3, available = available - $3, updated_at = CURRENT_TIMESTAMP
		 WHERE sku = $1 AND location_code = $2`, schema),
		sku, locationCode, qty); err != nil {
		return err
	}
	ledgerID := NewDocIDCompact("SLE")
	ledgerData, err := json.Marshal(map[string]interface{}{
		"id": ledgerID, "code": ledgerID, "item_id": sku, "warehouse_id": locationCode, "qty": -qty,
		"voucher_type": "ReturnExchange", "voucher_id": returnRequestID,
		"idempotency_key": fmt.Sprintf("ReturnExchange:%s:%s:%s", returnRequestID, locationCode, sku),
		"user_id":         userID, "status": "Active",
	})
	if err != nil {
		return err
	}
	// created_by is 'system' like every other insert in this file
	// (CreateReturnRequest's own ReturnRequest insert included) - it has a
	// foreign key onto the users table, whereas the actor name passed in here
	// (qcBy) is free text recorded in the JSON payload's user_id, not
	// guaranteed to be a real login.
	_, err = tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents(id,doctype,data,status,created_by) VALUES($1,'StockLedgerEntry',$2,'Active','system')`, schema),
		ledgerID, ledgerData)
	return err
}

func fetchRefundRequest(tenantID, refundRequestID string) (schema string, data map[string]interface{}, err error) {
	schema, err = db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var dataBytes []byte
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'RefundRequest' AND id = $1 AND deleted_at IS NULL`, schema),
		refundRequestID).Scan(&dataBytes)
	if err != nil {
		return "", nil, fmt.Errorf("refund request %s not found: %v", refundRequestID, err)
	}
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return "", nil, err
	}
	return schema, data, nil
}

func saveRefundRequest(schema, refundRequestID string, data map[string]interface{}, status string) error {
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'RefundRequest' AND id = $3`, schema),
		marshaled, status, refundRequestID)
	return err
}

// ApproveRefundRequest is the refund's own approval step - distinct from
// ApplyReturnQC's inventory-side GL post, this is the money-movement side
// that stays gated until a human signs off.
func ApproveRefundRequest(tenantID, refundRequestID, approvedBy string) error {
	schema, data, err := fetchRefundRequest(tenantID, refundRequestID)
	if err != nil {
		return err
	}
	if data["status"] != "Pending" {
		return fmt.Errorf("refund request %s is not Pending (currently %v)", refundRequestID, data["status"])
	}
	data["status"] = "Approved"
	data["approved_by"] = approvedBy
	return saveRefundRequest(schema, refundRequestID, data, "Approved")
}

// RejectRefundRequest requires the same mandatory 'Return'-category
// ReasonCode as RejectReturnRequest.
func RejectRefundRequest(tenantID, refundRequestID, reasonCode, rejectedBy string) error {
	schema, data, err := fetchRefundRequest(tenantID, refundRequestID)
	if err != nil {
		return err
	}
	status, _ := data["status"].(string)
	if status != "Pending" && status != "Approved" {
		return fmt.Errorf("refund request %s cannot be rejected from status %q", refundRequestID, status)
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Return"); err != nil {
		return err
	}
	data["status"] = "Rejected"
	data["rejection_reason"] = reasonCode
	data["processed_by"] = rejectedBy
	return saveRefundRequest(schema, refundRequestID, data, "Rejected")
}

// ProcessRefundRequest posts the revenue-side GL reversal (debit Sales
// Revenue, credit Cash/Bank - the same accounts ProcessReturnAnywhere uses
// for its own instant path) once a refund is Approved, and closes the
// parent ReturnRequest. This is the "refund-request record distinct from
// the immediate GL post" the checklist calls for: the inventory/COGS side
// already posted back in ApplyReturnQC when stock was actually received;
// this call is only the customer-facing money movement, held behind its own
// approval gate rather than firing the moment stock arrives.
func ProcessRefundRequest(tenantID, refundRequestID, processedBy, refundMethod string) error {
	schema, data, err := fetchRefundRequest(tenantID, refundRequestID)
	if err != nil {
		return err
	}
	if data["status"] != "Approved" {
		return fmt.Errorf("refund request %s is not Approved (currently %v)", refundRequestID, data["status"])
	}
	amountF := 0.0
	switch v := data["amount"].(type) {
	case float64:
		amountF = v
	case int:
		amountF = float64(v)
	}
	returnRequestID, _ := data["return_request_id"].(string)

	// Stage 47.4.4: the money leg, the refund status and the return closure
	// are ONE transaction. Before this the GL posted first and the status was
	// saved afterwards, so a failure in between left cash credited against a
	// refund still marked Approved - i.e. immediately processable again, for
	// the same money. The posting also had no idempotency key, so that second
	// processing would have posted a second time rather than no-opping.
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	// The status is re-read and re-checked under a row lock, not trusted from
	// the read above: two approvers clicking Process at once both saw
	// "Approved" there.
	var lockedStatus string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data->>'status' FROM %s.documents WHERE doctype = 'RefundRequest' AND id = $1 FOR UPDATE`, schema),
		refundRequestID).Scan(&lockedStatus); err != nil {
		return err
	}
	if lockedStatus != "Approved" {
		return fmt.Errorf("refund request %s is not Approved (currently %s)", refundRequestID, lockedStatus)
	}

	revenueDebits := map[string]int64{"4100": RupeesToPaise(amountF)}
	revenueCredits := map[string]int64{"1100": RupeesToPaise(amountF)}
	if err := PostDoubleEntryTx(tx, tenantID, schema, "RefundRequest", refundRequestID, revenueDebits, revenueCredits, "",
		fmt.Sprintf("RefundRequest:%s:REFUND", refundRequestID)); err != nil {
		return err
	}

	data["status"] = "Processed"
	data["processed_by"] = processedBy
	if refundMethod != "" {
		data["refund_method"] = refundMethod
	}
	refundMarshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'RefundRequest' AND id = $3`, schema),
		refundMarshaled, "Processed", refundRequestID); err != nil {
		return err
	}

	originalOrderID := ""
	if returnRequestID != "" {
		_, rrData, errRR := fetchReturnRequest(tenantID, returnRequestID)
		if errRR != nil {
			return errRR
		}
		originalOrderID, _ = rrData["original_order_id"].(string)

		// 47.4.4 tax reversal. Reversed from the RETURNED lines at their
		// original prices, using the same ComputeGSTForLines the sale used, so
		// the sale and its return cannot disagree about how much tax was on
		// the goods. Keyed on the return request, so this is posted once no
		// matter how many refunds a return produces.
		if err := PostReturnGSTReversalTx(tx, schema, tenantID, returnRequestID,
			refundedGSTLines(rrData), originalSaleWasInterstate(tenantID, originalOrderID)); err != nil {
			return err
		}

		rrData["status"] = "Closed"
		rrMarshaled, errM := json.Marshal(rrData)
		if errM != nil {
			return errM
		}
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ReturnRequest' AND id = $3`, schema),
			rrMarshaled, "Closed", returnRequestID); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if returnRequestID != "" {
		DispatchNotification(tenantID, "Refund Processed", originalOrderID, map[string]string{
			"return_request_id": returnRequestID, "refund_request_id": refundRequestID,
			"amount": fmt.Sprintf("%d", int(amountF)),
		})
	}
	return nil
}

// refundedGSTLines turns a ReturnRequest's refund-eligible lines back into GST
// inputs at their ORIGINAL sale prices - which is the only correct basis for a
// tax reversal, since that is the price the tax was charged on. A line that was
// dispositioned as not refundable (Missing, Rejected) contributes nothing,
// because no money is going back for it and therefore no tax should.
func refundedGSTLines(returnData map[string]interface{}) []GSTLineInput {
	itemsRaw, err := json.Marshal(returnData["items"])
	if err != nil {
		return nil
	}
	var items []returnItemRecord
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		return nil
	}
	var lines []GSTLineInput
	for _, it := range items {
		rule, ok := returnDispositionRule[it.Disposition]
		if !ok || !rule.RefundEligible || it.ExchangeSKU != "" || it.OriginalUnitPrice <= 0 {
			continue
		}
		lines = append(lines, GSTLineInput{Sku: it.SKU, Qty: it.Qty, UnitRate: it.OriginalUnitPrice})
	}
	return lines
}

// ScheduleReturnReversePickup (Stage 35.9.1) books a courier reverse-pickup
// for an Approved Customer Return - RTO never needs this (that parcel is
// already inbound the moment RecordRTO fires; it only needs QC once it
// arrives). Rather than a parallel booking mechanism, this creates an
// ordinary LogisticsBooking through the exact same CreateLogisticsBooking
// serviceability/carrier-selection logic Stage 26.12.4 built, tags it
// shipment_direction=Reverse plus the return_request_id, and then drives it
// through the unmodified Stage 35.5 AllocateCourierAWB/ScheduleCourierPickup
// pair - so a provider's real pickup API is called exactly the way a forward
// shipment already calls it, and RecordDeliveryEvent's own Reverse-direction
// branch (engines/marketplace.go) auto-receives the return once the same
// tracking webhook that already ingests forward deliveries reports this
// parcel Delivered (i.e. arrived back at the warehouse).
//
// pickupPincode doubles as CreateLogisticsBooking's destination_pincode for
// the serviceability check - a repurposing of a forward-shipment field for a
// pickup-direction lookup, not a new one, since a courier's CourierServiceArea
// coverage is the same table either direction.
func ScheduleReturnReversePickup(ctx context.Context, tenantID, returnRequestID, provider, pickupPincode, pickupAddress, pickupName string, pickupAt time.Time) (bookingID string, awb string, err error) {
	schema, data, err := fetchReturnRequest(tenantID, returnRequestID)
	if err != nil {
		return "", "", err
	}
	if data["status"] != "Approved" {
		return "", "", fmt.Errorf("return request %s is not Approved (currently %v)", returnRequestID, data["status"])
	}
	if data["request_type"] != "Customer Return" {
		return "", "", fmt.Errorf("reverse pickup only applies to a Customer Return (return request %s is %v)", returnRequestID, data["request_type"])
	}

	var existing string
	errDup := db.DB.QueryRow(fmt.Sprintf(
		`SELECT id FROM %s.documents WHERE doctype = 'LogisticsBooking' AND data->>'return_request_id' = $1 AND deleted_at IS NULL LIMIT 1`, schema),
		returnRequestID).Scan(&existing)
	if errDup == nil {
		return existing, "", nil
	} else if errDup != sql.ErrNoRows {
		return "", "", errDup
	}

	originalOrderID, _ := data["original_order_id"].(string)
	bookingID, err = CreateLogisticsBooking(tenantID, originalOrderID, "", provider, "", pickupPincode, 0)
	if err != nil {
		return "", "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('return_request_id', $1::text, 'shipment_direction', 'Reverse'), updated_at = CURRENT_TIMESTAMP
		 WHERE id = $2 AND doctype = 'LogisticsBooking'`, schema),
		returnRequestID, bookingID); err != nil {
		return bookingID, "", err
	}

	awbResult, err := AllocateCourierAWB(ctx, tenantID, provider, bookingID, CourierShipmentRequest{
		OriginPincode: pickupPincode, DestinationPincode: pickupPincode,
		RecipientName: pickupName, RecipientAddress: pickupAddress,
	})
	if err != nil {
		return bookingID, "", err
	}
	if _, err := ScheduleCourierPickup(ctx, tenantID, provider, bookingID, pickupName, pickupAt); err != nil {
		return bookingID, awbResult.AWB, err
	}

	data["status"] = "Pickup Scheduled"
	data["pickup_booking_id"] = bookingID
	if err := saveReturnRequest(schema, returnRequestID, data, "Pickup Scheduled"); err != nil {
		return bookingID, awbResult.AWB, err
	}
	DispatchNotification(tenantID, "Return Pickup Scheduled", originalOrderID, map[string]string{"return_request_id": returnRequestID, "booking_id": bookingID})
	return bookingID, awbResult.AWB, nil
}
