package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Stage 73.2.15 - order changes after release.
//
// Header, address, payment, facility and split edits already exist
// (order_mutations.go). This adds the three customer-service changes the desk
// still had to refuse:
//
//   - Cancel after Released. Allowed while no pick task has reached Packed:
//     the open tasks are cancelled and their bin allocations released in one
//     transaction, then CancelOrder's normal path releases the reservations.
//     Once anything is packed the parcel exists, and the answer is a return or
//     an RTO, not a cancel.
//   - Line edits up to the pick cutoff. Quantity changes, removals and new
//     lines, accepted while the order is Draft, On Hold or Reserved, or
//     Released with every pick task still Pending (the warehouse has not
//     started). A Released order goes back to Reserved, its untouched tasks
//     cancelled, so the next release picks exactly the edited lines. Stock is
//     re-reserved for the difference, the total is recomputed, and a
//     confirmed payment that no longer covers the total puts the order On
//     Hold for payment - all in one transaction.
//   - Replacement and reship orders linked to the original: zero-priced,
//     within the quantity the original line shipped, idempotent on a request
//     key, and visible from both orders.

// FulfillmentTaskCancelled is a pick task stopped because its order was
// cancelled or edited before picking finished.
const FulfillmentTaskCancelled = "Cancelled"

// cancelOpenPickTasksTx cancels an order's open pick tasks. allowPicking
// decides whether a task already being picked may be stopped (cancel: yes,
// the picker puts the units back; line edit: no, the cutoff has passed).
func cancelOpenPickTasksTx(tx *sql.Tx, schema, orderID, reason, userID string, allowPicking bool) ([]string, error) {
	rows, err := tx.Query(fmt.Sprintf(
		`SELECT id, status FROM %s.documents WHERE doctype = 'FulfillmentTask' AND data->>'order_id' = $1 AND deleted_at IS NULL FOR UPDATE`, schema), orderID)
	if err != nil {
		return nil, err
	}
	type task struct{ id, status string }
	var tasks []task
	for rows.Next() {
		var t task
		if err := rows.Scan(&t.id, &t.status); err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, t := range tasks {
		switch t.status {
		case "Packed", "Dispatched":
			return nil, &ValidationError{Code: "ORDERM-0465", SubFor: "order",
				Message: fmt.Sprintf("pick task %s is already %s - the parcel exists, so this needs a return or RTO, not a cancel or edit", t.id, t.status)}
		case "Picking":
			if !allowPicking {
				return nil, &ValidationError{Code: "ORDERM-0466", SubFor: "lines",
					Message: fmt.Sprintf("picking has started on task %s - line changes close once the warehouse starts picking", t.id)}
			}
		}
	}
	var cancelled []string
	patch, _ := json.Marshal(map[string]interface{}{
		"status": FulfillmentTaskCancelled, "cancel_reason": reason, "cancelled_by": userID,
		"cancelled_at": time.Now().UTC().Format(time.RFC3339),
	})
	for _, t := range tasks {
		if t.status != "Pending" && t.status != "Picking" {
			continue
		}
		if err := ReleaseTaskBinStockTx(tx, schema, t.id, ""); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3 AND doctype = 'FulfillmentTask'`, schema),
			string(patch), FulfillmentTaskCancelled, t.id); err != nil {
			return nil, err
		}
		cancelled = append(cancelled, t.id)
	}
	return cancelled, nil
}

// cancelReleasedOrderTasks is CancelOrder's Released branch: it stops the
// warehouse first, in its own transaction, so a cancelled order never leaves
// a picker working on it.
func cancelReleasedOrderTasks(tenantID, schema, orderID, reasonCode, userID string) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cancelled, err := cancelOpenPickTasksTx(tx, schema, orderID, "order cancelled: "+reasonCode, userID, true)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "ORDER_PICK_TASKS_CANCELLED", "SUCCESS",
		fmt.Sprintf("order=%s tasks=%v reason=%s", orderID, cancelled, reasonCode))
	return nil
}

// OrderLineChange is one requested line change. LineID empty adds a line.
type OrderLineChange struct {
	LineID    string   `json:"line_id"`
	SKU       string   `json:"sku"`
	Qty       int      `json:"qty"`
	UnitPrice *float64 `json:"unit_price"`
	Remove    bool     `json:"remove"`
}

// orderLineEditableStatuses are the order statuses line edits are open in;
// Released is checked against its tasks as well.
var orderLineEditableStatuses = map[string]bool{"Draft": true, "On Hold": true, "Reserved": true, OrderStatusReleased: true}

// reserveLineDeltaTx reserves qty more of sku at loc for a line, inside tx.
// Inline rather than CreateReservation because it must commit or roll back
// with the line edit - the TransitionTaskStatus precedent.
func reserveLineDeltaTx(tx *sql.Tx, tenantID, schema, sku, loc string, qty int, orderID, lineID string) error {
	var available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer, held int
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT available, reserved, safety_stock, blocked, qc_hold, damaged, channel_buffer, hold_qty
		FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema), sku, loc).
		Scan(&available, &reserved, &safetyStock, &blocked, &qcHold, &damaged, &channelBuffer, &held)
	if err == sql.ErrNoRows {
		return &ValidationError{Code: "ORDERM-0469", SubFor: "qty", Message: fmt.Sprintf("%s has no stock record at %s", sku, loc)}
	} else if err != nil {
		return err
	}
	if ats := computeATS(atsPolicyFor(schema), available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer, held); ats < qty {
		return &ValidationError{Code: "ORDERM-0469", SubFor: "qty",
			Message: fmt.Sprintf("only %d more of %s can be reserved at %s (asked for %d)", ats, sku, loc, qty)}
	}
	expiresAt := time.Now().Add(time.Duration(GetSettingInt(tenantID, "inventory.reservation_ttl_seconds")) * time.Second)
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.inventory_reservation (sku, location_code, quantity, reservation_type, expires_at, order_id, line_id)
		VALUES ($1, $2, $3, 'Online', $4, $5, $6)`, schema), sku, loc, qty, expiresAt, orderID, lineID); err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf(
		`UPDATE %s.inventory_availability SET reserved = reserved + $1, updated_at = CURRENT_TIMESTAMP WHERE sku = $2 AND location_code = $3`, schema),
		qty, sku, loc)
	return err
}

// releaseLineQtyTx gives back qty of a line's reservation, inside tx: the
// line's own rows shrink (newest first) and the availability count follows.
func releaseLineQtyTx(tx *sql.Tx, schema, sku, loc string, qty int, lineID string) error {
	rows, err := tx.Query(fmt.Sprintf(
		`SELECT id::text, quantity FROM %s.inventory_reservation WHERE line_id = $1 AND location_code = $2 ORDER BY created_at DESC FOR UPDATE`, schema), lineID, loc)
	if err != nil {
		return err
	}
	type res struct {
		id  string
		qty int
	}
	var held []res
	for rows.Next() {
		var r res
		if err := rows.Scan(&r.id, &r.qty); err != nil {
			rows.Close()
			return err
		}
		held = append(held, r)
	}
	rows.Close()
	remaining := qty
	for _, r := range held {
		if remaining <= 0 {
			break
		}
		if r.qty <= remaining {
			if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_reservation WHERE id = $1::uuid`, schema), r.id); err != nil {
				return err
			}
			remaining -= r.qty
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_reservation SET quantity = quantity - $1 WHERE id = $2::uuid`, schema), remaining, r.id); err != nil {
			return err
		}
		remaining = 0
	}
	_, err = tx.Exec(fmt.Sprintf(
		`UPDATE %s.inventory_availability SET reserved = GREATEST(0, reserved - $1), updated_at = CURRENT_TIMESTAMP WHERE sku = $2 AND location_code = $3`, schema),
		qty, sku, loc)
	return err
}

// EditOrderLines applies line changes to an order before the pick cutoff and
// returns the order's new status, total and any payment consequence.
func EditOrderLines(tenantID, orderID string, changes []OrderLineChange, reason, userID string) (map[string]interface{}, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, &ValidationError{Code: "ORDERM-0467", SubFor: "reason", Message: "say why the lines are changing - it is kept on the order's history"}
	}
	if len(changes) == 0 {
		return nil, &ValidationError{Code: "ORDERM-0467", SubFor: "lines", Message: "no line changes were sent"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var orderRaw []byte
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'SalesOrder' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), orderID).Scan(&orderRaw); err != nil {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("order %s not found", orderID)}
	}
	var order map[string]interface{}
	if err := json.Unmarshal(orderRaw, &order); err != nil {
		return nil, err
	}
	status, _ := order["order_status"].(string)
	if !orderLineEditableStatuses[status] {
		return nil, &ValidationError{Code: "ORDERM-0466", SubFor: "lines",
			Message: fmt.Sprintf("lines cannot change while the order is %s - line changes close once picking starts", status)}
	}
	if err := orderMutationAllowed(tenantID, status, "Edit Lines"); err != nil {
		return nil, &ValidationError{Code: "ORDERM-0466", SubFor: "lines", Message: err.Error()}
	}
	var invoiced int
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'SalesInvoice' AND data->>'sales_order_id' = $1 AND deleted_at IS NULL AND status <> 'Cancelled'`, schema), orderID).Scan(&invoiced); err != nil {
		return nil, err
	}
	if invoiced > 0 {
		return nil, &ValidationError{Code: "ORDERM-0466", SubFor: "lines", Message: "this order is already invoiced - its lines are fixed; use a return or credit note"}
	}

	reopened := []string{}
	if status == OrderStatusReleased {
		if reopened, err = cancelOpenPickTasksTx(tx, schema, orderID, "order lines edited: "+reason, userID, false); err != nil {
			return nil, err
		}
		status = "Reserved"
	}

	type lineRec struct {
		id   string
		data map[string]interface{}
	}
	rows, err := tx.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL ORDER BY id FOR UPDATE`, schema), orderID)
	if err != nil {
		return nil, err
	}
	byID := map[string]*lineRec{}
	var ordered []*lineRec
	maxSuffix := 0
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		rec := &lineRec{id: id}
		if err := json.Unmarshal(raw, &rec.data); err != nil {
			rows.Close()
			return nil, err
		}
		byID[id] = rec
		ordered = append(ordered, rec)
		if i := strings.LastIndex(id, "-L"); i >= 0 {
			if n, err := strconv.Atoi(id[i+2:]); err == nil && n > maxSuffix {
				maxSuffix = n
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	active := func(rec *lineRec) bool {
		ls, _ := rec.data["line_status"].(string)
		return ls != "Cancelled" && ls != "Returned"
	}
	defaultLocation := ""
	for _, rec := range ordered {
		if loc, _ := rec.data["location_code"].(string); loc != "" && active(rec) {
			defaultLocation = loc
			break
		}
	}

	bad := func(msg string) error { return &ValidationError{Code: "ORDERM-0467", SubFor: "lines", Message: msg} }
	summary := []string{}
	touched := map[string]bool{}
	for _, c := range changes {
		if c.LineID == "" {
			// A new line.
			sku := strings.TrimSpace(c.SKU)
			if sku == "" || c.Qty < 1 {
				return nil, bad("a new line needs an item and a quantity of at least 1")
			}
			if c.UnitPrice == nil || *c.UnitPrice < 0 {
				return nil, bad(fmt.Sprintf("give the unit price for the new %s line", sku))
			}
			item, err := ResolveItemBySKU(tenantID, sku)
			if err != nil || item == nil {
				return nil, bad(fmt.Sprintf("no active item matches %q", sku))
			}
			if code, _ := item.Data["code"].(string); code != "" {
				sku = code
			}
			maxSuffix++
			lineID := fmt.Sprintf("%s-L%d", orderID, maxSuffix)
			lineStatus := "Pending"
			if status == "Reserved" && defaultLocation != "" {
				if err := reserveLineDeltaTx(tx, tenantID, schema, sku, defaultLocation, c.Qty, orderID, lineID); err != nil {
					return nil, err
				}
				lineStatus = "Reserved"
			}
			rec := &lineRec{id: lineID, data: map[string]interface{}{
				"code": lineID, "order_id": orderID, "sku": sku, "qty": c.Qty, "unit_price": *c.UnitPrice,
				"location_code": defaultLocation, "line_status": lineStatus, "added_after_order": true,
			}}
			raw, _ := json.Marshal(rec.data)
			if _, err := tx.Exec(fmt.Sprintf(
				`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesOrderLine', $2, $3, $4)`, schema),
				lineID, raw, lineStatus, userID); err != nil {
				return nil, err
			}
			byID[lineID] = rec
			ordered = append(ordered, rec)
			summary = append(summary, fmt.Sprintf("added %s x%d", sku, c.Qty))
			continue
		}

		rec, ok := byID[c.LineID]
		if !ok {
			return nil, bad(fmt.Sprintf("line %s is not on order %s", c.LineID, orderID))
		}
		if touched[c.LineID] {
			return nil, bad(fmt.Sprintf("line %s is changed twice in one request", c.LineID))
		}
		touched[c.LineID] = true
		lineStatus, _ := rec.data["line_status"].(string)
		if lineStatus != "Reserved" && lineStatus != "Pending" && lineStatus != "On Hold" {
			return nil, bad(fmt.Sprintf("line %s is %s and can no longer change", c.LineID, lineStatus))
		}
		if b, _ := rec.data["bundle_sku"].(string); b != "" {
			return nil, bad(fmt.Sprintf("line %s is part of bundle %s - change the bundle by cancelling and re-ordering it", c.LineID, b))
		}
		sku, _ := rec.data["sku"].(string)
		loc, _ := rec.data["location_code"].(string)
		oldQty := int(numFromInterface(rec.data["qty"]))

		if c.Remove {
			if lineStatus == "Reserved" {
				if err := releaseLineQtyTx(tx, schema, sku, loc, oldQty, rec.id); err != nil {
					return nil, err
				}
			}
			rec.data["line_status"] = "Cancelled"
			rec.data["cancel_reason"] = "removed: " + reason
			summary = append(summary, fmt.Sprintf("removed %s (%s x%d)", rec.id, sku, oldQty))
		} else {
			if c.Qty < 1 {
				return nil, bad(fmt.Sprintf("line %s needs a quantity of at least 1 - remove the line instead", c.LineID))
			}
			if lineStatus == "Reserved" && c.Qty > oldQty {
				if err := reserveLineDeltaTx(tx, tenantID, schema, sku, loc, c.Qty-oldQty, orderID, rec.id); err != nil {
					return nil, err
				}
			} else if lineStatus == "Reserved" && c.Qty < oldQty {
				if err := releaseLineQtyTx(tx, schema, sku, loc, oldQty-c.Qty, rec.id); err != nil {
					return nil, err
				}
			}
			rec.data["qty"] = c.Qty
			if c.UnitPrice != nil {
				if *c.UnitPrice < 0 {
					return nil, bad(fmt.Sprintf("line %s cannot have a negative price", c.LineID))
				}
				rec.data["unit_price"] = *c.UnitPrice
			}
			summary = append(summary, fmt.Sprintf("%s qty %d->%d", rec.id, oldQty, c.Qty))
		}
		raw, _ := json.Marshal(rec.data)
		ls, _ := rec.data["line_status"].(string)
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrderLine' AND id = $3`, schema),
			raw, ls, rec.id); err != nil {
			return nil, err
		}
	}

	var newTotal float64
	activeLines := []SalesOrderLineInput{}
	for _, rec := range ordered {
		if !active(rec) {
			continue
		}
		qty := int(numFromInterface(rec.data["qty"]))
		price := numFromInterface(rec.data["unit_price"])
		newTotal += price * float64(qty)
		sku, _ := rec.data["sku"].(string)
		activeLines = append(activeLines, SalesOrderLineInput{SKU: sku, Qty: qty, UnitPrice: price})
	}
	if len(activeLines) == 0 {
		return nil, bad("that would remove every line - cancel the order instead")
	}
	oldTotal := numFromInterface(order["total_amount"])
	payment, _ := order["payment_status"].(string)
	consequence := ""
	if payment == "Confirmed" && newTotal > oldTotal+0.005 {
		order["payment_status"] = "Pending"
		order["payment_due_extra"] = newTotal - oldTotal
		consequence = fmt.Sprintf("total rose by %.2f - the confirmed payment no longer covers it, so the order waits for payment", newTotal-oldTotal)
	} else if payment == "Confirmed" && newTotal < oldTotal-0.005 {
		order["refund_due_amount"] = numFromInterface(order["refund_due_amount"]) + (oldTotal - newTotal)
		consequence = fmt.Sprintf("total fell by %.2f - refund the difference to the customer", oldTotal-newTotal)
	}

	shippingAddress, _ := order["shipping_address"].(string)
	holdReason, err := validateOrderChain(tenantID, shippingAddress, fmt.Sprint(order["payment_status"]), activeLines)
	if err != nil {
		return nil, err
	}
	if holdReason != "" {
		status = "On Hold"
	}
	order["order_status"] = status
	order["hold_reason"] = holdReason
	order["total_amount"] = newTotal
	history, _ := order["line_change_history"].([]interface{})
	order["line_change_history"] = append(history, map[string]interface{}{
		"at": time.Now().UTC().Format(time.RFC3339), "by": userID, "reason": reason,
		"changes": summary, "total_before": oldTotal, "total_after": newTotal,
	})
	raw, _ := json.Marshal(order)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $3`, schema),
		raw, status, orderID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "ORDER_LINES_EDIT", "SUCCESS",
		fmt.Sprintf("order=%s %s total %.2f->%.2f status=%s hold=%s reason=%s tasks_cancelled=%v",
			orderID, strings.Join(summary, "; "), oldTotal, newTotal, status, holdReason, reason, reopened))
	return map[string]interface{}{
		"order_id": orderID, "order_status": status, "hold_reason": holdReason,
		"total_before": oldTotal, "total_after": newTotal, "payment_consequence": consequence,
		"pick_tasks_cancelled": reopened,
	}, nil
}

// ReplacementLine is one line of a replacement or reship. SKU empty means the
// original line's SKU; a different SKU (an exchange) is a Replacement only.
type ReplacementLine struct {
	OriginalLineID string `json:"original_line_id"`
	SKU            string `json:"sku"`
	Qty            int    `json:"qty"`
}

var replaceableOrderStatuses = map[string]bool{"Shipped": true, "Delivered": true, "Closed": true, "Dispatched": true, "Partially Fulfilled": true}

// CreateReplacementOrder raises a zero-priced order linked to the original:
// Reship (the same goods again - lost or undelivered) or Replacement (the
// same or an exchanged item - damaged or wrong). requestKey makes a repeated
// submit return the first order instead of a second one.
func CreateReplacementOrder(tenantID, originalOrderID, kind string, lines []ReplacementLine, reason, requestKey, userID string) (string, bool, error) {
	bad := func(msg string) error { return &ValidationError{Code: "ORDERM-0468", SubFor: "lines", Message: msg} }
	if kind != "Replacement" && kind != "Reship" {
		return "", false, bad("choose Replacement or Reship")
	}
	reason = strings.TrimSpace(reason)
	requestKey = strings.TrimSpace(requestKey)
	if reason == "" || requestKey == "" {
		return "", false, bad("a reason and a request key are required")
	}
	if len(lines) == 0 {
		return "", false, bad("choose at least one line to send again")
	}
	schema, original, err := fetchSalesOrder(tenantID, originalOrderID)
	if err != nil {
		return "", false, &ValidationError{Code: "GLOBAL-0004", Message: err.Error()}
	}

	// One request key, one order - serialized so two clicks cannot both pass
	// the lookup below.
	ctx := context.Background()
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return "", false, err
	}
	defer conn.Close()
	lockKey := schema + "|replacement|" + originalOrderID + "|" + requestKey
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1))`, lockKey); err != nil {
		return "", false, err
	}
	defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, lockKey)

	var existing string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT id FROM %s.documents WHERE doctype = 'SalesOrder' AND deleted_at IS NULL AND data->>'original_order_id' = $1 AND data->>'replacement_request_key' = $2`, schema),
		originalOrderID, requestKey).Scan(&existing)
	if err == nil {
		return existing, false, nil
	} else if err != sql.ErrNoRows {
		return "", false, err
	}

	status, _ := original["order_status"].(string)
	if !replaceableOrderStatuses[status] {
		return "", false, bad(fmt.Sprintf("order %s is %s - a replacement or reship follows a shipped order; before that, edit or cancel it", originalOrderID, status))
	}

	// Quantity already sent again per original line, across live replacements.
	alreadySent := map[string]int{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT l.data->>'original_line_id', COALESCE((l.data->>'qty')::int, 0)
		FROM %[1]s.documents l JOIN %[1]s.documents o ON o.id = l.data->>'order_id' AND o.doctype = 'SalesOrder'
		WHERE l.doctype = 'SalesOrderLine' AND l.deleted_at IS NULL AND o.deleted_at IS NULL
		  AND o.data->>'original_order_id' = $1 AND o.status <> 'Cancelled' AND l.status <> 'Cancelled'`, schema), originalOrderID)
	if err != nil {
		return "", false, err
	}
	for rows.Next() {
		var lineID string
		var qty int
		if err := rows.Scan(&lineID, &qty); err != nil {
			rows.Close()
			return "", false, err
		}
		alreadySent[lineID] += qty
	}
	rows.Close()

	inputs := []SalesOrderLineInput{}
	origins := []string{}
	for _, rl := range lines {
		var raw []byte
		err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND id = $1 AND data->>'order_id' = $2 AND deleted_at IS NULL`, schema),
			rl.OriginalLineID, originalOrderID).Scan(&raw)
		if err != nil {
			return "", false, bad(fmt.Sprintf("line %s is not on order %s", rl.OriginalLineID, originalOrderID))
		}
		var ol map[string]interface{}
		_ = json.Unmarshal(raw, &ol)
		if ls, _ := ol["line_status"].(string); ls == "Cancelled" {
			return "", false, bad(fmt.Sprintf("line %s was cancelled and never shipped", rl.OriginalLineID))
		}
		origQty := int(numFromInterface(ol["qty"]))
		if rl.Qty < 1 || alreadySent[rl.OriginalLineID]+rl.Qty > origQty {
			return "", false, bad(fmt.Sprintf("line %s shipped %d and %d already went again - at most %d more",
				rl.OriginalLineID, origQty, alreadySent[rl.OriginalLineID], origQty-alreadySent[rl.OriginalLineID]))
		}
		alreadySent[rl.OriginalLineID] += rl.Qty
		origSKU, _ := ol["sku"].(string)
		sku := strings.TrimSpace(rl.SKU)
		if sku == "" {
			sku = origSKU
		}
		if sku != origSKU && kind == "Reship" {
			return "", false, bad("a reship sends the same item again - use Replacement for an exchange")
		}
		inputs = append(inputs, SalesOrderLineInput{SKU: sku, Qty: rl.Qty, UnitPrice: 0})
		origins = append(origins, rl.OriginalLineID)
	}

	channel, _ := original["channel"].(string)
	customer, _ := original["customer_name"].(string)
	address, _ := original["shipping_address"].(string)
	state, _ := original["shipping_state"].(string)
	phone, _ := original["customer_phone"].(string)
	newID, err := CreateSalesOrder(tenantID, SalesOrderInput{
		Channel: channel, CustomerName: customer, ShippingAddress: address, ShippingState: state,
		CustomerPhone: phone, PaymentStatus: "Confirmed", Lines: inputs,
	})
	if err != nil {
		return "", false, err
	}
	link, _ := json.Marshal(map[string]interface{}{
		"original_order_id": originalOrderID, "replacement_kind": kind, "replacement_reason": reason,
		"replacement_request_key": requestKey, "replacement_requested_by": userID,
	})
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb WHERE id = $2 AND doctype = 'SalesOrder'`, schema), string(link), newID); err != nil {
		return "", false, err
	}
	for i, origin := range origins {
		lineLink, _ := json.Marshal(map[string]interface{}{"original_line_id": origin})
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb WHERE id = $2 AND doctype = 'SalesOrderLine'`, schema),
			string(lineLink), fmt.Sprintf("%s-L%d", newID, i+1)); err != nil {
			return "", false, err
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = jsonb_set(data, '{replacement_order_ids}', COALESCE(data->'replacement_order_ids', '[]'::jsonb) || to_jsonb($1::text)),
		       updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND doctype = 'SalesOrder'`, schema), newID, originalOrderID); err != nil {
		return "", false, err
	}
	LogAuditEvent(tenantID, userID, "ORDER_REPLACEMENT_CREATED", "SUCCESS",
		fmt.Sprintf("order=%s %s order %s lines=%v reason=%s", originalOrderID, kind, newID, origins, reason))
	return newID, true, nil
}

// FindSalesOrderByChannelRef returns the order a channel reference already
// created, or "" - the same lookup CreateSalesOrder's idempotency uses, so a
// caller can tell "created" from "you sent this before" (FA-20261005-06).
func FindSalesOrderByChannelRef(tenantID, channel, channelOrderID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var id string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT id FROM %s.documents WHERE doctype = 'SalesOrder' AND data->>'channel' = $1 AND data->>'channel_order_id' = $2 AND deleted_at IS NULL LIMIT 1`, schema),
		channel, channelOrderID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}
