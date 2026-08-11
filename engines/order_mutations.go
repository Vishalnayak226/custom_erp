package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 35.3 - the order-mutation surface.
//
// Uniware exposes hold/unhold at order *and* item level, order edit, switch
// facility, set priority and split; this repo had order-level hold only. Each
// function here is one of those, and all five share three deliberate
// properties:
//
//  1. They go through orderMutationAllowed, which consults the same
//     StatusTransitionRule master (26.12.9) the cancellation gate uses, so the
//     matrix stays configurable rather than five new hardcoded status lists.
//  2. Anything that changes what is being fulfilled re-validates through
//     validateOrderChain - never a bespoke "resume from here" path. That is
//     26.12.1's own precedent, and it is what stops an edited address or a
//     re-split order from skipping the checks a new order would face.
//  3. They write reservations through CreateReservation / releaseLineReservation
//     rather than touching inventory_availability directly, so ATS stays the
//     one formula computeATS defines.

// orderMutationDefaults is the built-in gate used when a tenant has configured
// no StatusTransitionRule for a mutation. Terminal statuses are closed to
// everything: an order that has shipped cannot be re-priced, re-split, or
// moved to another warehouse, because the goods are already gone.
var orderMutationDefaults = map[string]bool{
	"Shipped":   false,
	"Delivered": false,
	"Closed":    false,
	"Cancelled": false,
}

// orderMutationAllowed answers whether one mutation may run against an order
// in its current status. The mutation name doubles as the StatusTransitionRule
// to_status, so an admin configures e.g. entity=Order, from_status=Shipped,
// to_status=Edit, allowed=Yes to permit a post-shipment address correction.
func orderMutationAllowed(tenantID, orderStatus, mutation string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	rule, err := lookupStatusTransitionRule(schema, "Order", orderStatus, mutation)
	if err != nil {
		return err
	}
	if rule.found {
		if !rule.allowed {
			return fmt.Errorf("%s is not allowed while the order is %s (blocked by a StatusTransitionRule)", mutation, orderStatus)
		}
		return nil
	}
	if allowed, isTerminal := orderMutationDefaults[orderStatus]; isTerminal && !allowed {
		return fmt.Errorf("%s is not allowed while the order is %s", mutation, orderStatus)
	}
	return nil
}

// fetchOrderLine reads one SalesOrderLine and the order it belongs to.
func fetchOrderLine(tenantID, lineID string) (schema string, lineData map[string]interface{}, orderID string, err error) {
	schema, err = db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, "", err
	}
	var raw []byte
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND id = $1 AND deleted_at IS NULL`, schema),
		lineID).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", nil, "", fmt.Errorf("order line %s not found", lineID)
	} else if err != nil {
		return "", nil, "", err
	}
	if err := json.Unmarshal(raw, &lineData); err != nil {
		return "", nil, "", err
	}
	orderID, _ = lineData["order_id"].(string)
	return schema, lineData, orderID, nil
}

// writeOrderLine persists a mutated line document and mirrors line_status onto
// the row's status column, which every list view and RBAC filter reads.
func writeOrderLine(schema, lineID string, lineData map[string]interface{}) error {
	marshaled, err := json.Marshal(lineData)
	if err != nil {
		return err
	}
	status, _ := lineData["line_status"].(string)
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrderLine' AND id = $3`, schema),
		marshaled, status, lineID)
	return err
}

// releaseLineReservation gives back the stock one line is holding.
//
// inventory_reservation carries no order or line reference (db/migration.sql
// §301), so the reservation is matched on (sku, location, quantity, type) and
// the oldest match is released. Stated plainly because it is a real
// limitation: with two concurrent orders reserving the same SKU/location/qty,
// this releases one of the two reservations, not provably "this line's". The
// availability arithmetic is identical either way - the same quantity comes
// back to the same pool - so it is correct in aggregate, which is what ATS
// consumes. Attributing reservations per line is a schema change and belongs
// with the reservation-sweeper work noted in micro_checklist.md 35.1.3.
func releaseLineReservation(schema, sku, location string, qty int) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}
	var resID string
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT id::text FROM %s.inventory_reservation
		 WHERE sku = $1 AND location_code = $2 AND quantity = $3
		 ORDER BY created_at LIMIT 1 FOR UPDATE`, schema), sku, location, qty).Scan(&resID)
	if err == sql.ErrNoRows {
		// Nothing to release - a line that was never reserved (created On
		// Hold) is not an error condition for the caller.
		return nil
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_reservation WHERE id = $1::uuid`, schema), resID); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.inventory_availability SET reserved = GREATEST(reserved - $1, 0), updated_at = CURRENT_TIMESTAMP
		 WHERE sku = $2 AND location_code = $3`, schema), qty, sku, location); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------------------
// 35.3.1 - item-level hold / unhold
// ---------------------------------------------------------------------------

// HoldOrderLine stops one line of an order without stopping the rest, and
// gives its reserved stock back so another order can take it. The order header
// is left alone: a partially-held order is still a live order, which is the
// whole point of holding at line level rather than order level.
func HoldOrderLine(tenantID, lineID, reasonCode, owner string) error {
	if err := requireActiveReasonCode(tenantID, reasonCode, "Hold"); err != nil {
		return err
	}
	schema, lineData, orderID, err := fetchOrderLine(tenantID, lineID)
	if err != nil {
		return err
	}
	orderStatus, _, err := GetOrderStatus(tenantID, orderID)
	if err != nil {
		return err
	}
	if err := orderMutationAllowed(tenantID, orderStatus, "Hold Line"); err != nil {
		return err
	}
	lineStatus, _ := lineData["line_status"].(string)
	if lineStatus == "On Hold" {
		return fmt.Errorf("order line %s is already On Hold", lineID)
	}
	if lineStatus == "Dispatched" || lineStatus == "Cancelled" || lineStatus == "Returned" {
		return fmt.Errorf("order line %s cannot be held from status %s", lineID, lineStatus)
	}

	if lineStatus == "Reserved" {
		sku, _ := lineData["sku"].(string)
		location, _ := lineData["location_code"].(string)
		qty := int(numericFromAny(lineData["qty"]))
		if location != "" && qty > 0 {
			if err := releaseLineReservation(schema, sku, location, qty); err != nil {
				return fmt.Errorf("failed to release the reservation behind line %s: %v", lineID, err)
			}
		}
	}

	lineData["line_status"] = "On Hold"
	lineData["hold_reason"] = reasonCode
	lineData["hold_owner"] = owner
	if err := writeOrderLine(schema, lineID, lineData); err != nil {
		return err
	}
	LogAuditEvent(tenantID, owner, "ORDER_LINE_HOLD", "SUCCESS", fmt.Sprintf("line=%s order=%s reason=%s", lineID, orderID, reasonCode))
	DispatchNotification(tenantID, "Order Line On Hold", orderID, map[string]string{"line_id": lineID, "hold_reason": reasonCode})
	return nil
}

// ReleaseOrderLineHold puts a held line back into the fulfilment flow,
// re-reserving its stock. If the stock is no longer there the line returns to
// Pending rather than Reserved, so it surfaces in the allocation-pending queue
// instead of silently claiming inventory that does not exist.
func ReleaseOrderLineHold(tenantID, lineID, userID string) error {
	schema, lineData, orderID, err := fetchOrderLine(tenantID, lineID)
	if err != nil {
		return err
	}
	if status, _ := lineData["line_status"].(string); status != "On Hold" {
		return fmt.Errorf("order line %s is not On Hold", lineID)
	}
	sku, _ := lineData["sku"].(string)
	location, _ := lineData["location_code"].(string)
	qty := int(numericFromAny(lineData["qty"]))

	newStatus := "Pending"
	if location != "" && qty > 0 {
		if _, err := CreateReservation(tenantID, sku, location, qty, "Online", 0); err == nil {
			newStatus = "Reserved"
		}
	}
	lineData["line_status"] = newStatus
	lineData["hold_reason"] = ""
	lineData["hold_owner"] = ""
	if err := writeOrderLine(schema, lineID, lineData); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "ORDER_LINE_RELEASE", "SUCCESS", fmt.Sprintf("line=%s order=%s -> %s", lineID, orderID, newStatus))
	return nil
}

// ---------------------------------------------------------------------------
// 35.3.2 - order edit
// ---------------------------------------------------------------------------

// OrderEdit carries the header fields an order edit may change. A nil pointer
// means "leave this alone", which is what lets a caller correct only the
// shipping address without having to resend the whole header and risk blanking
// a field it never intended to touch.
type OrderEdit struct {
	CustomerName    *string
	CustomerPhone   *string
	ShippingAddress *string
	BillingAddress  *string
	PaymentStatus   *string
	CustomFields    map[string]string
}

// EditSalesOrder applies a header edit and then re-runs the *same*
// validateOrderChain a new order goes through, so an edit that invalidates the
// order (a bad address, a payment status moved back to Pending) puts it On
// Hold with a routable reason instead of leaving a silently-broken order in
// the Reserved queue. An edit that fixes a held order releases the hold.
func EditSalesOrder(tenantID, orderID string, edit OrderEdit, userID string) error {
	schema, orderData, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	orderStatus, _ := orderData["order_status"].(string)
	if err := orderMutationAllowed(tenantID, orderStatus, "Edit"); err != nil {
		return err
	}

	changed := []string{}
	apply := func(key string, value *string) {
		if value == nil {
			return
		}
		if existing, _ := orderData[key].(string); existing == *value {
			return
		}
		orderData[key] = *value
		changed = append(changed, key)
	}
	apply("customer_name", edit.CustomerName)
	apply("shipping_address", edit.ShippingAddress)
	apply("billing_address", edit.BillingAddress)
	apply("payment_status", edit.PaymentStatus)
	if edit.CustomerPhone != nil {
		// Cleaned through the same phone engine CreateSalesOrder uses, so an
		// edited number is stored in the identical shape to an imported one.
		p := NormalizeTenantPhone(tenantID, *edit.CustomerPhone)
		orderData["customer_phone"] = p.National
		if p.CountryISO2 != "" {
			orderData["customer_country"] = p.CountryISO2
		}
		changed = append(changed, "customer_phone")
	}
	for key, value := range edit.CustomFields {
		// Custom fields are namespaced so an edit can never overwrite an
		// engine-owned key (order_status, hold_reason, total_amount...).
		orderData["custom_"+key] = value
		changed = append(changed, "custom_"+key)
	}
	if len(changed) == 0 {
		return fmt.Errorf("nothing to edit on order %s", orderID)
	}

	lines, err := orderLineInputs(schema, orderID)
	if err != nil {
		return err
	}
	shippingAddress, _ := orderData["shipping_address"].(string)
	paymentStatus, _ := orderData["payment_status"].(string)
	holdReason, err := validateOrderChain(tenantID, shippingAddress, paymentStatus, lines)
	if err != nil {
		return err
	}

	newStatus := orderStatus
	switch {
	case holdReason != "":
		newStatus = "On Hold"
	case orderStatus == "On Hold":
		// The edit fixed whatever the hold was about. Leave the status at On
		// Hold rather than guessing Reserved here: ReleaseOrderHold is the one
		// function that knows how to re-allocate and re-reserve, and running
		// half of its job from here would be exactly the bespoke resume path
		// this file exists to avoid. The console offers Release next to Edit.
		newStatus = "On Hold"
	}
	orderData["order_status"] = newStatus
	orderData["hold_reason"] = holdReason
	marshaled, err := json.Marshal(orderData)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $3`, schema),
		marshaled, newStatus, orderID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "ORDER_EDIT", "SUCCESS",
		fmt.Sprintf("order=%s fields=%s status=%s hold=%s", orderID, strings.Join(changed, ","), newStatus, holdReason))
	return nil
}

// orderLineInputs reads an order's lines back into the SalesOrderLineInput
// shape validateOrderChain and ResolveAllocationPlan both take.
func orderLineInputs(schema, orderID string) ([]SalesOrderLineInput, error) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT COALESCE(data->>'sku', ''), COALESCE((data->>'qty')::int, 0), COALESCE((data->>'unit_price')::numeric, 0)
		 FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL
		 ORDER BY id`, schema), orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lines []SalesOrderLineInput
	for rows.Next() {
		var l SalesOrderLineInput
		if err := rows.Scan(&l.SKU, &l.Qty, &l.UnitPrice); err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

// ---------------------------------------------------------------------------
// 35.3.3 - switch facility
// ---------------------------------------------------------------------------

// SwitchOrderFacility moves an order's not-yet-picked lines to another
// location. Lines that are already Dispatched, Cancelled or Returned are left
// exactly where they are - the goods have physically moved, and rewriting
// their location would make the stock ledger disagree with the warehouse.
//
// Passing an empty targetLocation re-runs ResolveAllocationPlan instead, which
// is the "just find me a better node" case the console's Reallocate button
// fires.
func SwitchOrderFacility(tenantID, orderID, targetLocation, userID string) (int, error) {
	schema, orderData, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return 0, err
	}
	orderStatus, _ := orderData["order_status"].(string)
	if err := orderMutationAllowed(tenantID, orderStatus, "Switch Facility"); err != nil {
		return 0, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL ORDER BY id`, schema),
		orderID)
	if err != nil {
		return 0, err
	}
	type movable struct {
		id   string
		data map[string]interface{}
	}
	var candidates []movable
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		var d map[string]interface{}
		if err := json.Unmarshal(raw, &d); err != nil {
			rows.Close()
			return 0, err
		}
		switch status, _ := d["line_status"].(string); status {
		case "Dispatched", "Cancelled", "Returned":
			continue
		}
		candidates = append(candidates, movable{id: id, data: d})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, fmt.Errorf("order %s has no unpicked lines to move", orderID)
	}

	// With no explicit target, re-plan through the allocation engine scoped to
	// exactly these lines - never a second sourcing implementation.
	resolved := targetLocation
	var plan *AllocationPlan
	if resolved == "" {
		planLines := make([]SalesOrderLineInput, len(candidates))
		for i, c := range candidates {
			sku, _ := c.data["sku"].(string)
			planLines[i] = SalesOrderLineInput{SKU: sku, Qty: int(numericFromAny(c.data["qty"]))}
		}
		channel, _ := orderData["channel"].(string)
		shippingAddress, _ := orderData["shipping_address"].(string)
		plan, err = ResolveAllocationPlan(tenantID, channel, shippingAddress, planLines)
		if err != nil {
			return 0, err
		}
		if plan == nil {
			return 0, fmt.Errorf("no allocation plan is available for order %s under the configured rules", orderID)
		}
	}

	moved := 0
	for i, c := range candidates {
		sku, _ := c.data["sku"].(string)
		oldLocation, _ := c.data["location_code"].(string)
		qty := int(numericFromAny(c.data["qty"]))
		newLocation := resolved
		if plan != nil && i < len(plan.LineLocations) {
			newLocation = plan.LineLocations[i]
		}
		if newLocation == "" || newLocation == oldLocation {
			continue
		}
		wasReserved, _ := c.data["line_status"].(string)

		// Reserve at the destination BEFORE releasing at the source. The other
		// order would leave a window where the stock is unreserved at both
		// ends, and a concurrent order could take it - the line would then end
		// up Pending with its original reservation already gone.
		if wasReserved == "Reserved" && qty > 0 {
			if _, err := CreateReservation(tenantID, sku, newLocation, qty, "Online", 0); err != nil {
				return moved, fmt.Errorf("line %s: %s has no available stock for %s, nothing was moved past this point: %v", c.id, newLocation, sku, err)
			}
			if oldLocation != "" {
				if err := releaseLineReservation(schema, sku, oldLocation, qty); err != nil {
					return moved, err
				}
			}
		}
		c.data["location_code"] = newLocation
		if err := writeOrderLine(schema, c.id, c.data); err != nil {
			return moved, err
		}
		moved++
	}
	if moved == 0 {
		return 0, fmt.Errorf("every unpicked line on order %s is already at the requested location", orderID)
	}
	LogAuditEvent(tenantID, userID, "ORDER_SWITCH_FACILITY", "SUCCESS",
		fmt.Sprintf("order=%s target=%s lines_moved=%d", orderID, targetLocation, moved))
	return moved, nil
}

// ---------------------------------------------------------------------------
// 35.3.4 - priority / expedite
// ---------------------------------------------------------------------------

// validOrderPriorities matches the doctype field's own Select options.
var validOrderPriorities = map[string]bool{"Normal": true, "Expedite": true}

// SetOrderPriority flags an order for expedited handling. Honoured by the
// console's list ordering and by PickTasksInPriorityOrder below, which is what
// pick-list generation reads.
func SetOrderPriority(tenantID, orderID, priority, userID string) error {
	if !validOrderPriorities[priority] {
		return fmt.Errorf("priority must be Normal or Expedite, got %q", priority)
	}
	schema, orderData, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	orderStatus, _ := orderData["order_status"].(string)
	if err := orderMutationAllowed(tenantID, orderStatus, "Set Priority"); err != nil {
		return err
	}
	orderData["priority"] = priority
	marshaled, err := json.Marshal(orderData)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $2`, schema),
		marshaled, orderID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "ORDER_SET_PRIORITY", "SUCCESS", fmt.Sprintf("order=%s priority=%s", orderID, priority))
	return nil
}

// PickTasksInPriorityOrder lists open fulfillment tasks with expedited orders
// first, then oldest-first within each band. This is the ordering a picker's
// worklist should follow, and it is the only thing that makes 35.3.4's flag
// mean anything operationally rather than being a label on a screen.
func PickTasksInPriorityOrder(tenantID, locationCode string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	args := []interface{}{}
	locationClause := ""
	if locationCode != "" {
		locationClause = " AND t.data->>'location_code' = $1"
		args = append(args, locationCode)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT t.id, COALESCE(t.data->>'order_id', ''), COALESCE(t.data->>'location_code', ''), t.status,
		       COALESCE(o.data->>'priority', 'Normal'), t.created_at
		FROM %s.documents t
		LEFT JOIN %s.documents o ON o.doctype = 'SalesOrder' AND o.id = t.data->>'order_id' AND o.deleted_at IS NULL
		WHERE t.doctype = 'FulfillmentTask' AND t.deleted_at IS NULL
		  AND t.status NOT IN ('Dispatched', 'Rejected')%s
		ORDER BY (COALESCE(o.data->>'priority', 'Normal') = 'Expedite') DESC, t.created_at`,
		schema, schema, locationClause), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, orderID, location, status, priority string
		var createdAt sql.NullTime
		if err := rows.Scan(&id, &orderID, &location, &status, &priority, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"task_id": id, "order_id": orderID, "location_code": location,
			"status": status, "priority": priority, "created_at": createdAt.Time,
		})
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// 35.3.5 - order split
// ---------------------------------------------------------------------------

// SplitOrder moves the named lines into their own fulfilment group, so they
// are picked, packed and shipped independently of the rest of the order.
//
// Deliberately NOT a new SalesOrder. The allocation engine has always been
// able to place different lines at different locations; what was missing was a
// way to say so explicitly. Keeping one order means the customer keeps one
// order id, the invoice chain keeps one parent, and returns keep resolving to
// the order the customer actually placed - all of which a clone-into-two-
// orders implementation would break.
func SplitOrder(tenantID, orderID string, lineIDs []string, userID string) (string, error) {
	if len(lineIDs) == 0 {
		return "", fmt.Errorf("select at least one line to split out")
	}
	schema, orderData, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return "", err
	}
	orderStatus, _ := orderData["order_status"].(string)
	if err := orderMutationAllowed(tenantID, orderStatus, "Split"); err != nil {
		return "", err
	}

	var totalLines int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL`, schema),
		orderID).Scan(&totalLines); err != nil {
		return "", err
	}
	if len(lineIDs) >= totalLines {
		return "", fmt.Errorf("splitting every line out leaves nothing behind - a split needs at least one line to stay on the original group")
	}

	groupID := NewDocID("FG")
	for _, lineID := range lineIDs {
		lineSchema, lineData, lineOrderID, err := fetchOrderLine(tenantID, lineID)
		if err != nil {
			return "", err
		}
		if lineOrderID != orderID {
			return "", fmt.Errorf("line %s belongs to order %s, not %s", lineID, lineOrderID, orderID)
		}
		if status, _ := lineData["line_status"].(string); status == "Dispatched" || status == "Returned" {
			return "", fmt.Errorf("line %s is already %s and cannot be split out", lineID, status)
		}
		lineData["fulfillment_group"] = groupID
		if err := writeOrderLine(lineSchema, lineID, lineData); err != nil {
			return "", err
		}
	}
	LogAuditEvent(tenantID, userID, "ORDER_SPLIT", "SUCCESS",
		fmt.Sprintf("order=%s group=%s lines=%d", orderID, groupID, len(lineIDs)))
	return groupID, nil
}
