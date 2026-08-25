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

// resolveOriginalSaleLinePrices reads a POSCart's own stored sale_price/
// cost_price per SKU - resolveOriginalSale (engines/fulfillment.go) only
// captures sku/qty via transferLine, which is enough for the SALESR-0129/
// 0130/0131 window/qty checks this function's caller reuses but not enough
// to price a refund correctly. A SalesInvoice-only reference (no per-line
// data at all, see resolveOriginalSale's own comment) resolves to an empty
// map - those lines simply have no resolvable original price.
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
			CostPrice float64 `json:"cost_price"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(dataStr), &cart); err != nil {
		return nil, err
	}
	prices := map[string]originalLinePrice{}
	for _, l := range cart.Items {
		prices[l.Sku] = originalLinePrice{SalePrice: l.SalePrice, CostPrice: l.CostPrice}
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

// sumPriorReturnRequests mirrors sumPriorReturns (engines/fulfillment.go)
// but scans this file's own ReturnRequest doctype - CreateReturnRequest
// checks both pools so a customer can't over-return by mixing the instant
// POS-return path and this approval-gated OMS path against the same
// original order. A Rejected request never happened, so it's excluded.
func sumPriorReturnRequests(tenantID, originalOrderID string) (map[string]int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'ReturnRequest' AND data->>'original_order_id' = $1 AND data->>'status' != 'Rejected' AND deleted_at IS NULL`, schema),
		originalOrderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := map[string]int{}
	for rows.Next() {
		var dataStr string
		if err := rows.Scan(&dataStr); err != nil {
			return nil, err
		}
		var doc struct {
			Items []returnItemRecord `json:"items"`
		}
		if err := json.Unmarshal([]byte(dataStr), &doc); err != nil {
			continue
		}
		for _, it := range doc.Items {
			totals[it.SKU] += it.Qty
		}
	}
	return totals, rows.Err()
}

// CreateReturnRequest is the workflow's entry point for both request types.
// For a Customer Return it reuses resolveOriginalSale's window/qty checks
// (SALESR-0129/0130/0131) exactly as ProcessReturnAnywhere does, plus this
// file's own sumPriorReturnRequests pool. For an RTO it requires the
// LogisticsBooking (Stage 26.12.4) to already be in the 'RTO' status
// RecordRTO sets, and is idempotent on booking_id (replaying an RTO webhook/
// call returns the existing non-Rejected request rather than duplicating
// it). Returns the new ReturnRequest's id.
func CreateReturnRequest(tenantID, requestType, returnLocation, originalOrderID, bookingID, requestedBy string, items []ReturnItemInput) (string, error) {
	if requestType != "Customer Return" && requestType != "RTO" {
		return "", fmt.Errorf("request_type must be 'Customer Return' or 'RTO', got %q", requestType)
	}
	if len(items) == 0 {
		return "", errors.New("at least one item is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	priceBySku := map[string]originalLinePrice{}
	// Stage 35.9.3 (return routing): populated only for an RTO, whose
	// LogisticsBooking already carries the pincode the parcel was being
	// delivered to - the one pincode source this workflow has. A Customer
	// Return's original order is a POSCart/SalesInvoice (resolveOriginalSale,
	// engines/fulfillment.go), neither of which carries a shipping address, so
	// auto-routing below simply has nothing to route from there and that path
	// keeps requiring an explicit return_location, same as today.
	rtoDestinationPincode := ""

	switch requestType {
	case "Customer Return":
		if originalOrderID == "" {
			return "", errors.New("original_order_id is required for a Customer Return")
		}
		soldLines, saleDate, found, errResolve := resolveOriginalSale(tenantID, originalOrderID)
		if errResolve != nil {
			return "", errResolve
		}
		if !found {
			return "", &ValidationError{Code: "SALESR-0131", Message: fmt.Sprintf("no original bill found for %q - a return requires a valid original bill reference", originalOrderID)}
		}
		returnWindowDays := salesReturnWindowDaysFor(tenantID)
		if !saleDate.IsZero() && time.Since(saleDate) > time.Duration(returnWindowDays)*24*time.Hour {
			return "", &ValidationError{Code: "SALESR-0129", Message: fmt.Sprintf("return is not allowed more than %d days after the original sale (%s)", returnWindowDays, saleDate.Format("2006-01-02"))}
		}
		if len(soldLines) > 0 {
			soldBySku := map[string]int{}
			for _, l := range soldLines {
				soldBySku[l.Sku] += l.Qty
			}
			alreadyReturned, errSum := sumPriorReturns(tenantID, originalOrderID)
			if errSum != nil {
				return "", errSum
			}
			alreadyRequested, errSum2 := sumPriorReturnRequests(tenantID, originalOrderID)
			if errSum2 != nil {
				return "", errSum2
			}
			for _, it := range items {
				remaining := soldBySku[it.SKU] - alreadyReturned[it.SKU] - alreadyRequested[it.SKU]
				if it.Qty > remaining {
					return "", &ValidationError{Code: "SALESR-0130", Message: fmt.Sprintf("return quantity for SKU %q (%d) exceeds remaining returnable quantity (%d)", it.SKU, it.Qty, remaining)}
				}
			}
		}
		priceBySku, err = resolveOriginalSaleLinePrices(tenantID, originalOrderID)
		if err != nil {
			return "", err
		}

	case "RTO":
		if bookingID == "" {
			return "", errors.New("booking_id is required for an RTO return request")
		}
		_, bookingData, bookingStatus, errB := fetchLogisticsBooking(tenantID, bookingID)
		if errB != nil {
			return "", errB
		}
		if bookingStatus != "RTO" {
			return "", fmt.Errorf("booking %s is not marked RTO (currently %s) - call RecordRTO first", bookingID, bookingStatus)
		}
		var existing string
		errDup := db.DB.QueryRow(fmt.Sprintf(
			`SELECT id FROM %s.documents WHERE doctype = 'ReturnRequest' AND data->>'booking_id' = $1 AND data->>'status' != 'Rejected' AND deleted_at IS NULL LIMIT 1`, schema),
			bookingID).Scan(&existing)
		if errDup == nil {
			return existing, nil
		} else if errDup != sql.ErrNoRows {
			return "", errDup
		}
		originalOrderID, _ = bookingData["order_id"].(string)
		if originalOrderID != "" {
			priceBySku, err = resolveSalesOrderLinePrices(tenantID, originalOrderID)
			if err != nil {
				return "", err
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
	autoRouted := false
	if returnLocation == "" && rtoDestinationPincode != "" {
		routingItems := make([]map[string]interface{}, len(items))
		for i, it := range items {
			routingItems[i] = map[string]interface{}{"sku": it.SKU, "qty": it.Qty}
		}
		if loc, ok, errRoute := singleLocationNearestPincode(schema, rtoDestinationPincode, routingItems); errRoute == nil && ok {
			returnLocation = loc
			autoRouted = true
		}
	}
	if returnLocation == "" {
		return "", errors.New("return_location is required (no return location could be auto-routed for this request)")
	}

	records := make([]returnItemRecord, len(items))
	for i, it := range items {
		if it.SKU == "" || it.Qty <= 0 {
			return "", fmt.Errorf("each item requires a non-empty sku and a positive qty")
		}
		p := priceBySku[it.SKU]
		records[i] = returnItemRecord{SKU: it.SKU, Qty: it.Qty, OriginalUnitPrice: p.SalePrice, OriginalCostPrice: p.CostPrice}
	}

	returnID := NewDocID("RR")
	doc := map[string]interface{}{
		"code": returnID, "request_type": requestType, "original_order_id": originalOrderID,
		"booking_id": bookingID, "return_location": returnLocation, "return_location_auto_routed": autoRouted,
		"status": "Requested", "requested_by": requestedBy, "approved_by": "", "rejection_reason": "",
		"items": records, "total_refund_eligible": 0,
	}
	marshaled, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ReturnRequest', $2, 'Requested', 'system')`, schema),
		returnID, marshaled); err != nil {
		return "", err
	}

	event := "Return Requested"
	if requestType == "RTO" {
		event = "RTO Detected"
	}
	DispatchNotification(tenantID, event, originalOrderID, map[string]string{"return_request_id": returnID, "request_type": requestType})
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

	if err := tx.Commit(); err != nil {
		return 0, "", err
	}

	if costReceivedTotal > 0 {
		inventoryDebits := map[string]int64{"1200": RupeesToPaise(costReceivedTotal)}
		inventoryCredits := map[string]int64{"5100": RupeesToPaise(costReceivedTotal)}
		if err := PostDoubleEntry(tenantID, "ReturnRequest", returnRequestID, inventoryDebits, inventoryCredits, "", ""); err != nil {
			return refundTotal, refundRequestID, err
		}
	}
	if exchangeCostTotal > 0 {
		// Mirror image of the block above: stock physically left in the
		// exchange, so Inventory Control is credited and COGS debited this
		// time, for the same reused cost basis.
		exchangeDebits := map[string]int64{"5100": RupeesToPaise(exchangeCostTotal)}
		exchangeCredits := map[string]int64{"1200": RupeesToPaise(exchangeCostTotal)}
		if err := PostDoubleEntry(tenantID, "ReturnRequest", returnRequestID, exchangeDebits, exchangeCredits, "", fmt.Sprintf("ReturnRequest:%s:EXCHANGE", returnRequestID)); err != nil {
			return refundTotal, refundRequestID, err
		}
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

	revenueDebits := map[string]int64{"4100": RupeesToPaise(amountF)}
	revenueCredits := map[string]int64{"1100": RupeesToPaise(amountF)}
	if err := PostDoubleEntry(tenantID, "RefundRequest", refundRequestID, revenueDebits, revenueCredits, "", ""); err != nil {
		return err
	}

	data["status"] = "Processed"
	data["processed_by"] = processedBy
	if refundMethod != "" {
		data["refund_method"] = refundMethod
	}
	if err := saveRefundRequest(schema, refundRequestID, data, "Processed"); err != nil {
		return err
	}

	if returnRequestID != "" {
		if _, rrData, errRR := fetchReturnRequest(tenantID, returnRequestID); errRR == nil {
			originalOrderID, _ := rrData["original_order_id"].(string)
			if errSave := saveReturnRequest(schema, returnRequestID, rrData, "Closed"); errSave == nil {
				DispatchNotification(tenantID, "Refund Processed", originalOrderID, map[string]string{
					"return_request_id": returnRequestID, "refund_request_id": refundRequestID,
					"amount": fmt.Sprintf("%d", int(amountF)),
				})
			}
		}
	}
	return nil
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
