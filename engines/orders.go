package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// SalesOrderLineInput is one requested line for CreateSalesOrder - qty/price
// come from the caller (channel webhook payload, or a future manual-order
// screen), everything else (location_code, line_status) is computed by the
// engine during validate/reserve.
type SalesOrderLineInput struct {
	SKU       string
	Qty       int
	UnitPrice float64
}

// Hold-routing exception codes (Stage 26.12.1). Deliberately distinct from
// the formal error catalog (internal/server/error_catalog_generated.go,
// generated from an external Standard Message Control Matrix xlsx this repo
// doesn't own the source of) - these are the Order Engine's own internal
// reason codes, stored on SalesOrder.hold_reason so a hold queue can filter/
// route by which check failed, per the validate chain design note in
// docs/specs/oms_master_blueprint_reference.md §12.
const (
	HoldSKUMappingFailed = "SKU_MAPPING_FAILED"
	HoldAddressInvalid   = "ADDR_INVALID"
	HoldPaymentPending   = "PAYMENT_PENDING"
	// HoldAllocationFailed (Stage 26.12.2) is a fourth hold-routing code,
	// raised after validateOrderChain passes but ResolveAllocationPlan
	// (engines/sourcing.go) can't find a usable plan under the tenant's
	// configured AllocationRule chain - the checklist's "allocation
	// exception" case. Distinct from the three validate-chain codes above
	// (those fail before sourcing is even attempted); this one fails after.
	HoldAllocationFailed = "ALLOCATION_FAILED"
)

// orderCancelBlockedStatuses is the blueprint's own hardcoded stage-gate
// fallback (§12: "forbids cancellation once Shipped/Delivered/Closed/
// Cancelled") - used only when no tenant-configured StatusTransitionRule row
// (Stage 26.12.9) overrides it, so the cancellation matrix is usable out of
// the box before any admin has populated that master.
var orderCancelBlockedStatuses = map[string]bool{
	"Shipped":   true,
	"Delivered": true,
	"Closed":    true,
	"Cancelled": true,
}

// pincodeShapeRe is a loose "contains something pincode-shaped" check, not a
// country-specific format validator (this repo has no address-validation
// service) - matches the design note's "pincode format" validation step in
// the abstract, same spirit as the retired prototype's own check.
var pincodeShapeRe = regexp.MustCompile(`\d{4,10}`)

// validateOrderChain runs the fixed-order SKU-mapping -> address -> payment
// checks from docs/specs/oms_master_blueprint_reference.md §12 and returns
// the first failing Hold* code, or "" if the order is clean. CreateSalesOrder
// and ReleaseOrderHold both call this same function so a resumed hold can
// never diverge from the create-time checks (the design note's own "re-run
// the same validation chain rather than a bespoke resume path").
func validateOrderChain(tenantID string, shippingAddress, paymentStatus string, lines []SalesOrderLineInput) (string, error) {
	// 1. SKU-mapping: each line's SKU must resolve to an active, non-deleted Item.
	// Stage 30.1.1: resolved through the shared ResolveItemBySKU (code ->
	// barcode -> id) instead of matching only `code`, so a channel that sends
	// a barcode or the internal id isn't held as an unmapped SKU.
	for _, l := range lines {
		item, err := ResolveItemBySKU(tenantID, l.SKU)
		if errors.Is(err, ErrItemNotFound) || (err == nil && item.Status != "Active") {
			return HoldSKUMappingFailed, nil
		} else if err != nil {
			return "", err
		}
	}

	// 2. Address: non-empty and contains a pincode-shaped token.
	if shippingAddress == "" || !pincodeShapeRe.MatchString(shippingAddress) {
		return HoldAddressInvalid, nil
	}

	// 3. Payment: must be confirmed before reservation (prepaid gate).
	if paymentStatus != "Confirmed" {
		return HoldPaymentPending, nil
	}

	return "", nil
}

// isCancellationBlocked checks Stage 26.12.9's StatusTransitionRule master
// first (entity='Order', from_status=orderStatus, to_status='Cancelled') so
// an admin can configure the matrix without a code change; falls back to the
// blueprint's own hardcoded blocklist when no matching rule has been
// configured yet, so the gate is real and enforced from day one either way.
func isCancellationBlocked(tenantID, orderStatus string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}

	var allowed string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'allowed' FROM %s.documents
		 WHERE doctype = 'StatusTransitionRule' AND status = 'Active' AND deleted_at IS NULL
		   AND data->>'entity' = 'Order' AND data->>'from_status' = $1 AND data->>'to_status' = 'Cancelled'
		 LIMIT 1`, schema), orderStatus).Scan(&allowed)
	if err == nil {
		return allowed != "Yes", nil
	} else if err != sql.ErrNoRows {
		return false, err
	}

	return orderCancelBlockedStatuses[orderStatus], nil
}

// CreateSalesOrder validates then reserves stock for a new order, in the
// fixed SKU-mapping -> address -> payment order the design note calls out.
// A failing check doesn't reject the order outright - it places the order
// On Hold with a routable hold_reason instead, the same "never hard-reject,
// let a human/queue decide" precedent FindBestFulfillmentNode's OMNI-0247
// fallback already uses at the sourcing layer, just applied one layer
// earlier at order validation. Returns the new SalesOrder's id.
//
// This is the Order Engine's own creation path - it does not replace
// ImportChannelOrder (engines/sourcing.go), which existing channel webhooks
// still call; rewiring those onto SalesOrder is a separate follow-up, not
// part of this item's scope.
func CreateSalesOrder(tenantID, channel, channelOrderID, customerName, shippingAddress, paymentStatus string, lines []SalesOrderLineInput) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", errors.New("order must have at least one line")
	}

	// Idempotency: replaying the same channel+channel_order_id returns the
	// existing order instead of erroring, so a retried webhook/API call is
	// safe (mirrors ImportChannelOrder's own channel_order_mapping check).
	if channel != "" && channelOrderID != "" {
		var existingID string
		err = db.DB.QueryRow(fmt.Sprintf(
			`SELECT id FROM %s.documents WHERE doctype = 'SalesOrder' AND data->>'channel' = $1 AND data->>'channel_order_id' = $2 AND deleted_at IS NULL`, schema),
			channel, channelOrderID).Scan(&existingID)
		if err == nil {
			return existingID, nil
		} else if err != sql.ErrNoRows {
			return "", err
		}
	}

	holdReason, err := validateOrderChain(tenantID, shippingAddress, paymentStatus, lines)
	if err != nil {
		return "", err
	}

	// Allocation runs only once the validate chain is clean - a failing
	// SKU-mapping/address/payment check takes priority over sourcing, same
	// fixed check order the design note calls out (§12).
	var plan *AllocationPlan
	if holdReason == "" {
		plan, err = ResolveAllocationPlan(tenantID, channel, shippingAddress, lines)
		if err != nil {
			return "", err
		}
		if plan == nil {
			holdReason = HoldAllocationFailed
		}
	}

	orderStatus := "Reserved"
	lineStatus := "Reserved"
	if holdReason != "" {
		orderStatus = "On Hold"
		lineStatus = "Pending"
	}

	orderID := NewDocID("SO")
	var totalAmount float64
	for _, l := range lines {
		totalAmount += l.UnitPrice * float64(l.Qty)
	}

	orderDoc := map[string]interface{}{
		"code":             orderID,
		"channel":          channel,
		"channel_order_id": channelOrderID,
		"customer_name":    customerName,
		"shipping_address": shippingAddress,
		"payment_status":   paymentStatus,
		"order_status":     orderStatus,
		"hold_reason":      holdReason,
		"hold_owner":       "",
		"total_amount":     totalAmount,
	}
	orderMarshaled, err := json.Marshal(orderDoc)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesOrder', $2, $3, 'system')`, schema),
		orderID, orderMarshaled, orderStatus); err != nil {
		return "", err
	}

	if holdReason != "" {
		LogSystemError(tenantID, "", "Medium", "Order Engine / OMS", fmt.Sprintf("[ORDER-HOLD] order %s placed On Hold: %s", orderID, holdReason), "")
	}

	for i, l := range lines {
		lineID := fmt.Sprintf("%s-L%d", orderID, i+1)
		lineLocation := ""
		if plan != nil {
			lineLocation = plan.LineLocations[i]
		}
		lineDoc := map[string]interface{}{
			"code":          lineID,
			"order_id":      orderID,
			"sku":           l.SKU,
			"qty":           l.Qty,
			"unit_price":    l.UnitPrice,
			"location_code": lineLocation,
			"line_status":   lineStatus,
		}
		lineMarshaled, err := json.Marshal(lineDoc)
		if err != nil {
			return "", err
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesOrderLine', $2, $3, 'system')`, schema),
			lineID, lineMarshaled, lineStatus); err != nil {
			return "", err
		}

		if holdReason == "" {
			if _, err := CreateReservation(tenantID, l.SKU, lineLocation, l.Qty, "Online", 0); err != nil {
				return "", fmt.Errorf("failed to reserve stock for SKU %s: %v", l.SKU, err)
			}
		}
	}

	// Stage 26.12.10: fire the order-placed notification either way, plus a
	// distinct on-hold one when validation/allocation didn't clear.
	DispatchNotification(tenantID, "Order Placed", orderID, map[string]string{"order_status": orderStatus})
	if holdReason != "" {
		DispatchNotification(tenantID, "Order On Hold", orderID, map[string]string{"hold_reason": holdReason})
	}

	return orderID, nil
}

// GetOrderStatus returns a SalesOrder's current order_status/hold_reason -
// exported so callers like the release-hold HTTP handler can report what
// actually happened (ReleaseOrderHold itself doesn't error just because the
// order stayed On Hold; the caller needs the resulting state to tell those
// two outcomes apart).
func GetOrderStatus(tenantID, orderID string) (orderStatus, holdReason string, err error) {
	_, orderData, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return "", "", err
	}
	orderStatus, _ = orderData["order_status"].(string)
	holdReason, _ = orderData["hold_reason"].(string)
	return orderStatus, holdReason, nil
}

// fetchSalesOrder loads a SalesOrder's raw fields needed by the hold/cancel
// engine functions below - a small local read helper, not a generic getter,
// since callers each need a different subset and none of this needs to be
// public.
func fetchSalesOrder(tenantID, orderID string) (schema string, orderData map[string]interface{}, err error) {
	schema, err = db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var dataBytes []byte
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'SalesOrder' AND id = $1 AND deleted_at IS NULL`, schema),
		orderID).Scan(&dataBytes)
	if err != nil {
		return "", nil, fmt.Errorf("order %s not found: %v", orderID, err)
	}
	if err := json.Unmarshal(dataBytes, &orderData); err != nil {
		return "", nil, err
	}
	return schema, orderData, nil
}

// PlaceOrderHold is the Hold engine's manual entry point (a CS agent placing
// an order on hold, as opposed to CreateSalesOrder's automatic validation-
// failure hold) - requires an Active ReasonCode (Stage 26.12.9) in the
// 'Hold' category, per the blueprint's "mandatory reason codes" requirement.
func PlaceOrderHold(tenantID, orderID, reasonCode, owner string) error {
	schema, orderData, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Hold"); err != nil {
		return err
	}

	orderData["order_status"] = "On Hold"
	orderData["hold_reason"] = reasonCode
	orderData["hold_owner"] = owner
	marshaled, err := json.Marshal(orderData)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'On Hold', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $2`, schema),
		marshaled, orderID)
	return err
}

// ReleaseOrderHold re-runs the exact same validateOrderChain CreateSalesOrder
// used, per the design note's "hold-release re-runs the same validation
// chain rather than a bespoke resume path". If the order is now clean, it
// runs the Allocation/Sourcing Engine (ResolveAllocationPlan, Stage 26.12.2)
// and reserves stock for every line still Pending (a line already Reserved/
// Dispatched/Cancelled/Returned is left untouched - this only ever fires for
// a hold placed before reservation). If validation is still failing,
// hold_reason is updated to whatever check now fails (which may differ from
// the original reason). If validation passes but no allocation plan can be
// found either, hold_reason becomes HoldAllocationFailed instead. Either way
// the order stays On Hold.
func ReleaseOrderHold(tenantID, orderID string) error {
	schema, orderData, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	if orderData["order_status"] != "On Hold" {
		return fmt.Errorf("order %s is not On Hold", orderID)
	}

	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL`, schema),
		orderID)
	if err != nil {
		return err
	}
	type lineRow struct {
		id   string
		data map[string]interface{}
	}
	var lineRows []lineRow
	for rows.Next() {
		var id string
		var dataBytes []byte
		if err := rows.Scan(&id, &dataBytes); err != nil {
			rows.Close()
			return err
		}
		var d map[string]interface{}
		if err := json.Unmarshal(dataBytes, &d); err != nil {
			rows.Close()
			return err
		}
		lineRows = append(lineRows, lineRow{id: id, data: d})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	lines := make([]SalesOrderLineInput, len(lineRows))
	for i, lr := range lineRows {
		sku, _ := lr.data["sku"].(string)
		unitPrice, _ := lr.data["unit_price"].(float64)
		qty := 0
		switch v := lr.data["qty"].(type) {
		case float64:
			qty = int(v)
		case int:
			qty = v
		}
		lines[i] = SalesOrderLineInput{SKU: sku, Qty: qty, UnitPrice: unitPrice}
	}

	shippingAddress, _ := orderData["shipping_address"].(string)
	paymentStatus, _ := orderData["payment_status"].(string)
	holdReason, err := validateOrderChain(tenantID, shippingAddress, paymentStatus, lines)
	if err != nil {
		return err
	}

	if holdReason != "" {
		orderData["hold_reason"] = holdReason
		marshaled, err := json.Marshal(orderData)
		if err != nil {
			return err
		}
		_, err = db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $2`, schema),
			marshaled, orderID)
		return err
	}

	// Only lines still Pending need (re-)allocation - a line already
	// Reserved/Dispatched/Cancelled/Returned is untouched, same scope the
	// original single-location version of this loop already had.
	var pendingIdx []int
	var pendingLines []SalesOrderLineInput
	for i, lr := range lineRows {
		if lr.data["line_status"] != "Pending" {
			continue
		}
		pendingIdx = append(pendingIdx, i)
		pendingLines = append(pendingLines, lines[i])
	}

	if len(pendingLines) > 0 {
		channel, _ := orderData["channel"].(string)
		plan, err := ResolveAllocationPlan(tenantID, channel, shippingAddress, pendingLines)
		if err != nil {
			return err
		}
		if plan == nil {
			orderData["hold_reason"] = HoldAllocationFailed
			marshaled, err := json.Marshal(orderData)
			if err != nil {
				return err
			}
			_, err = db.DB.Exec(fmt.Sprintf(
				`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $2`, schema),
				marshaled, orderID)
			return err
		}

		for j, idx := range pendingIdx {
			lr := lineRows[idx]
			lineLocation := plan.LineLocations[j]
			if _, err := CreateReservation(tenantID, pendingLines[j].SKU, lineLocation, pendingLines[j].Qty, "Online", 0); err != nil {
				return fmt.Errorf("failed to reserve stock for SKU %s: %v", pendingLines[j].SKU, err)
			}
			lr.data["line_status"] = "Reserved"
			lr.data["location_code"] = lineLocation
			lineMarshaled, err := json.Marshal(lr.data)
			if err != nil {
				return err
			}
			if _, err := db.DB.Exec(fmt.Sprintf(
				`UPDATE %s.documents SET data = $1, status = 'Reserved', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrderLine' AND id = $2`, schema),
				lineMarshaled, lr.id); err != nil {
				return err
			}
		}
	}

	orderData["order_status"] = "Reserved"
	orderData["hold_reason"] = ""
	orderData["hold_owner"] = ""
	marshaled, err := json.Marshal(orderData)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Reserved', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $2`, schema),
		marshaled, orderID)
	return err
}

// CancelOrder is the stage-gated cancellation matrix's enforcement point
// (§12: "forbids cancellation once Shipped/Delivered/Closed/Cancelled") -
// isCancellationBlocked checks the configurable StatusTransitionRule master
// first, falling back to that same hardcoded blocklist. Requires a mandatory
// Active ReasonCode in the 'Cancellation' category. Releases reservations
// only for lines still Pending/Reserved (§12: "releases reservations only
// for lines still Reserved/Allocated") - a Dispatched/Cancelled/Returned
// line is left alone.
func CancelOrder(tenantID, orderID, reasonCode string) error {
	schema, orderData, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	orderStatus, _ := orderData["order_status"].(string)

	blocked, err := isCancellationBlocked(tenantID, orderStatus)
	if err != nil {
		return err
	}
	if blocked {
		return fmt.Errorf("order %s cannot be cancelled from status %q", orderID, orderStatus)
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Cancellation"); err != nil {
		return err
	}

	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL`, schema),
		orderID)
	if err != nil {
		return err
	}
	type lineRow struct {
		id   string
		data map[string]interface{}
	}
	var lineRows []lineRow
	for rows.Next() {
		var id string
		var dataBytes []byte
		if err := rows.Scan(&id, &dataBytes); err != nil {
			rows.Close()
			return err
		}
		var d map[string]interface{}
		if err := json.Unmarshal(dataBytes, &d); err != nil {
			rows.Close()
			return err
		}
		lineRows = append(lineRows, lineRow{id: id, data: d})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, lr := range lineRows {
		lineStatus, _ := lr.data["line_status"].(string)
		if lineStatus == "Pending" || lineStatus == "Reserved" {
			if lineStatus == "Reserved" {
				sku, _ := lr.data["sku"].(string)
				locationCode, _ := lr.data["location_code"].(string)
				qty := 0
				switch v := lr.data["qty"].(type) {
				case float64:
					qty = int(v)
				case int:
					qty = v
				}
				// Release the reservation's held-back stock the same way
				// TransitionTaskStatus's own Rejected branch already does
				// (engines/fulfillment.go) - decrement the availability
				// read model's reserved count directly; no generic release-
				// by-reservation-id function exists yet in this repo.
				if _, err := db.DB.Exec(fmt.Sprintf(
					`UPDATE %s.inventory_availability SET reserved = GREATEST(0, reserved - $1), updated_at = CURRENT_TIMESTAMP WHERE sku = $2 AND location_code = $3`, schema),
					qty, sku, locationCode); err != nil {
					return err
				}
			}
			lr.data["line_status"] = "Cancelled"
			lineMarshaled, err := json.Marshal(lr.data)
			if err != nil {
				return err
			}
			if _, err := db.DB.Exec(fmt.Sprintf(
				`UPDATE %s.documents SET data = $1, status = 'Cancelled', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrderLine' AND id = $2`, schema),
				lineMarshaled, lr.id); err != nil {
				return err
			}
		}
	}

	orderData["order_status"] = "Cancelled"
	orderData["hold_reason"] = ""
	orderData["hold_owner"] = ""
	orderData["cancel_reason"] = reasonCode
	marshaled, err := json.Marshal(orderData)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Cancelled', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $2`, schema),
		marshaled, orderID)
	if err == nil {
		DispatchNotification(tenantID, "Order Cancelled", orderID, map[string]string{"reason_code": reasonCode})
	}
	return err
}

// requireActiveReasonCode validates a mandatory reason code against Stage
// 26.12.9's ReasonCode master - must exist, be Active, and match the
// expected category (Hold/Cancellation), per the blueprint's "mandatory
// reason codes" requirement for both hold and cancel actions.
func requireActiveReasonCode(tenantID, reasonCode, expectedCategory string) error {
	if reasonCode == "" {
		return fmt.Errorf("a reason code is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var status, category string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT status, data->>'category' FROM %s.documents WHERE doctype = 'ReasonCode' AND id = $1 AND deleted_at IS NULL`, schema),
		reasonCode).Scan(&status, &category)
	if err == sql.ErrNoRows {
		return fmt.Errorf("reason code %q not found", reasonCode)
	} else if err != nil {
		return err
	}
	if status != "Active" {
		return fmt.Errorf("reason code %q is not Active", reasonCode)
	}
	if category != expectedCategory {
		return fmt.Errorf("reason code %q is category %q, expected %q", reasonCode, category, expectedCategory)
	}
	return nil
}
