package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// salesReturnWindowDaysFor (SALESR-0129) resolves the tenant's configured
// sales-return window. Stage 30.7 replaced the former hardcoded 30-day
// constant with the "sales.return_window_days" setting; the registered
// default is still 30, so an untouched tenant is unchanged. Read per call
// (not cached in a package var) so an admin edit applies to the very next
// return with no restart.
func salesReturnWindowDaysFor(tenantID string) int {
	return GetSettingInt(tenantID, "sales.return_window_days")
}

// resolveOriginalSale (SALESR-0129/0130/0131) looks up the sale a return
// claims against. POSCart is checked first (this app's actual retail sale
// path) and returns its line items so the caller can cross-check returned
// quantities; SalesInvoice has no per-line item data at all (it's a single
// total_amount doctype - see sales_invoice.go), so a SalesInvoice match
// only satisfies "an original bill exists", not a quantity cross-check.
func resolveOriginalSale(tenantID, orderID string) (lines []transferLine, saleDate time.Time, found bool, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, time.Time{}, false, err
	}
	var dataStr string
	var createdAt time.Time
	if errQ := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, created_at AT TIME ZONE current_setting('TimeZone') FROM %s.documents WHERE doctype = 'POSCart' AND id = $1 AND status = 'Paid'`, schema),
		orderID).Scan(&dataStr, &createdAt); errQ == nil {
		var cart struct {
			Items []transferLine `json:"items"`
		}
		if errU := json.Unmarshal([]byte(dataStr), &cart); errU == nil {
			lines = cart.Items
		}
		return lines, createdAt, true, nil
	}
	if errQ := db.DB.QueryRow(fmt.Sprintf(
		`SELECT created_at AT TIME ZONE current_setting('TimeZone') FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1`, schema),
		orderID).Scan(&createdAt); errQ == nil {
		return nil, createdAt, true, nil
	}
	return nil, time.Time{}, false, nil
}

// sumPriorReturns was the legacy return path's "already returned" pool,
// read outside any transaction from a SalesReturn document whose repeat
// writes were silently swallowed - the mechanism audit finding A-04 named.
// Retired with ProcessReturnAnywhere in Stage 47.4.1; the replacement is
// assertReturnEligibleTx (engines/returns_atomic.go), which sums the same
// document families INSIDE the transaction that holds the original sale
// locked.

// CreateFulfillmentTasks registers a store-level pick task
func CreateFulfillmentTasks(tenantID string, orderID string, locationCode string, items []interface{}) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	taskID := NewDocID("TSK")
	docData := map[string]interface{}{
		"code":          taskID,
		"order_id":      orderID,
		"location_code": locationCode,
		"status":        "Pending",
		"items":         items,
	}

	marshaled, err := json.Marshal(docData)
	if err != nil {
		return "", err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by) 
		VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system')`, schema)
	_, err = db.DB.Exec(query, taskID, marshaled)
	return taskID, err
}

// OrderStatusReleased is a SalesOrder handed to the warehouse: its pick tasks
// exist. Stock stays reserved (lines keep line_status Reserved) until the task
// is Dispatched, which is where TransitionTaskStatus deducts it.
const OrderStatusReleased = "Released"

// ReleaseOrderToFulfillment is the operator's next step for a Reserved order
// (FA-20261005-03). Before it, nothing in the product ever created a
// FulfillmentTask for an order - CreateFulfillmentTasks above had only test
// callers - so a manual or channel order sat at Reserved with an empty
// Fulfillment screen and no action that moved it.
//
// One task per sourcing location (each line already carries the location its
// stock was reserved at), created in one transaction with the order's move to
// Released, so a failure leaves neither. Idempotent: releasing an order that is
// already Released returns its existing tasks and creates nothing.
func ReleaseOrderToFulfillment(tenantID, orderID, actor string) (taskIDs []string, created bool, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, false, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var orderStr string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'SalesOrder' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema),
		orderID).Scan(&orderStr); err != nil {
		return nil, false, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("order %s not found", orderID)}
	}
	var order map[string]interface{}
	if err := json.Unmarshal([]byte(orderStr), &order); err != nil {
		return nil, false, err
	}
	status, _ := order["order_status"].(string)

	existing := func() ([]string, error) {
		rows, err := tx.Query(fmt.Sprintf(
			`SELECT id FROM %s.documents WHERE doctype = 'FulfillmentTask' AND data->>'order_id' = $1 AND deleted_at IS NULL ORDER BY created_at, id`, schema), orderID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	}
	if status == OrderStatusReleased {
		ids, err := existing()
		return ids, false, err
	}
	if status != "Reserved" {
		return nil, false, &ValidationError{Code: "GLOBAL-0019", Message: fmt.Sprintf("only a Reserved order can be released to fulfillment; order %s is %s", orderID, status)}
	}

	rows, err := tx.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL ORDER BY id`, schema), orderID)
	if err != nil {
		return nil, false, err
	}
	byLocation := map[string][]interface{}{}
	var locations []string
	for rows.Next() {
		var lineID, lineStr string
		if err := rows.Scan(&lineID, &lineStr); err != nil {
			rows.Close()
			return nil, false, err
		}
		var line map[string]interface{}
		if err := json.Unmarshal([]byte(lineStr), &line); err != nil {
			rows.Close()
			return nil, false, err
		}
		if ls, _ := line["line_status"].(string); ls != "Reserved" {
			continue // held, cancelled or already moved lines are not picked
		}
		loc, _ := line["location_code"].(string)
		if loc == "" {
			rows.Close()
			return nil, false, &ValidationError{Code: "GLOBAL-0019", Message: fmt.Sprintf("line %s has no sourcing location; reallocate the order first", lineID)}
		}
		if _, seen := byLocation[loc]; !seen {
			locations = append(locations, loc)
		}
		byLocation[loc] = append(byLocation[loc], map[string]interface{}{
			"sku": line["sku"], "qty": int(numFromInterface(line["qty"])), "line_id": lineID,
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(locations) == 0 {
		return nil, false, &ValidationError{Code: "GLOBAL-0019", Message: fmt.Sprintf("order %s has no reserved lines to release", orderID)}
	}

	for _, loc := range locations {
		taskID := NewDocID("TSK")
		taskBytes, err := json.Marshal(map[string]interface{}{
			"code": taskID, "order_id": orderID, "location_code": loc, "status": "Pending", "items": byLocation[loc],
		})
		if err != nil {
			return nil, false, err
		}
		if _, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system')`, schema),
			taskID, taskBytes); err != nil {
			return nil, false, err
		}
		taskIDs = append(taskIDs, taskID)
	}

	order["order_status"] = OrderStatusReleased
	order["released_at"] = time.Now().UTC().Format(time.RFC3339)
	order["released_by"] = actor
	orderBytes, err := json.Marshal(order)
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $3`, schema),
		orderBytes, OrderStatusReleased, orderID); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	LogAuditEvent(tenantID, actor, "ORDER_RELEASED_TO_FULFILLMENT", "SUCCESS",
		fmt.Sprintf("Order %s released: %d pick task(s) %v", orderID, len(taskIDs), taskIDs))
	return taskIDs, true, nil
}

// taskItemQty reads one task item's quantity whatever JSON number type it
// arrived as.
func taskItemQty(item map[string]interface{}) int {
	return int(numFromInterface(item["qty"]))
}

// itemStandardCostRupees is the COGS fallback for an item with no receipt
// history yet: its master's standard_cost, or 0.
func itemStandardCostRupees(tenantID, sku string) float64 {
	item, err := ResolveItemBySKU(tenantID, sku)
	if err != nil || item == nil {
		return 0
	}
	return numFromInterface(item.Data["standard_cost"])
}

// TransitionTaskStatus handles status workflows for picking and dispatches.
//
// 2026-10-07 (FA-20261005-03 follow-through, once Release to Fulfillment made
// this path reachable from the product):
//   - The task row is read and locked INSIDE the transaction, and a task that
//     is already Dispatched or Rejected is refused - before, a second
//     "Dispatched" (the generic transition route has no guard of its own)
//     deducted the stock a second time.
//   - Dispatched now does what a sale does at POS: consumes the order line's
//     reservation row (it used to stay behind as a phantom the sweeper could
//     later release a second time), marks the line Dispatched, posts COGS
//     (Dr 5100 / Cr 1200 at the moving-average cost, standard cost as the
//     fallback) in the same transaction, and writes the stock ledger.
//   - Rejected re-reserves at the next node ATTRIBUTED to the order line and
//     moves the line there; it used to create an unattributed row the sweeper
//     dropped at TTL while the order still needed the stock.
func TransitionTaskStatus(tenantID string, taskID string, newStatus string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	// 1. Fetch and lock the current task document
	var docDataBytes []byte
	var currentStatus string
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT data, status FROM %s.documents
		WHERE id = $1 AND doctype = 'FulfillmentTask' FOR UPDATE`, schema), taskID).Scan(&docDataBytes, &currentStatus)
	if err != nil {
		return fmt.Errorf("task %s not found: %v", taskID, err)
	}
	if currentStatus == "Dispatched" || currentStatus == "Rejected" {
		return &ValidationError{Code: "GLOBAL-0019", Message: fmt.Sprintf("task %s is already %s", taskID, currentStatus)}
	}

	var task map[string]interface{}
	if err := json.Unmarshal(docDataBytes, &task); err != nil {
		return err
	}

	orderID, _ := task["order_id"].(string)
	locationCode, _ := task["location_code"].(string)
	itemsRaw, _ := task["items"].([]interface{})

	setLine := func(lineID string, fields map[string]interface{}, status string) error {
		patch, _ := json.Marshal(fields)
		_, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.documents SET data = data || $1::jsonb, status = COALESCE(NULLIF($2, ''), status), updated_at = CURRENT_TIMESTAMP
			WHERE doctype = 'SalesOrderLine' AND id = $3`, schema), string(patch), status, lineID)
		return err
	}

	var ledgerLines []PostedStockLine

	if newStatus == "Rejected" {
		// A. Release this node's hold: the availability count and, for an
		// attributed line, its reservation row.
		var sourcingItems []map[string]interface{}
		for _, itemVal := range itemsRaw {
			item, ok := itemVal.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := item["sku"].(string)
			qty := taskItemQty(item)
			lineID, _ := item["line_id"].(string)

			if _, err = tx.Exec(fmt.Sprintf(`
				UPDATE %s.inventory_availability
				SET reserved = GREATEST(0, reserved - $1), updated_at = CURRENT_TIMESTAMP
				WHERE sku = $2 AND location_code = $3`, schema), qty, sku, locationCode); err != nil {
				return err
			}
			if lineID != "" {
				if _, err := tx.Exec(fmt.Sprintf(
					`DELETE FROM %s.inventory_reservation WHERE line_id = $1 AND location_code = $2`, schema), lineID, locationCode); err != nil {
					return err
				}
			}
			sourcingItems = append(sourcingItems, map[string]interface{}{"sku": sku, "qty": qty, "line_id": lineID})
		}

		// B. Trigger re-routing rules to find next best node
		nextLocation, errRoute := FindBestFulfillmentNode(tenantID, sourcingItems)
		if errRoute == nil && nextLocation != "" && nextLocation != locationCode {
			expiresAt := time.Now().Add(time.Duration(GetSettingInt(tenantID, "inventory.reservation_ttl_seconds")) * time.Second)
			for _, item := range sourcingItems {
				sku := item["sku"].(string)
				qty := item["qty"].(int)
				lineID, _ := item["line_id"].(string)
				// Inline rather than via CreateReservation because this runs
				// inside this transaction; attributed to the order line when
				// there is one, so the sweeper keeps it while the line lives.
				if _, errRes := tx.Exec(fmt.Sprintf(`
					INSERT INTO %s.inventory_reservation (sku, location_code, quantity, reservation_type, expires_at, order_id, line_id)
					VALUES ($1, $2, $3, 'Online', $4, NULLIF($5, ''), NULLIF($6, ''))`, schema),
					sku, nextLocation, qty, expiresAt, orderID, lineID); errRes != nil {
					return errRes
				}
				if _, errResAvail := tx.Exec(fmt.Sprintf(`
					INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available, reserved)
					VALUES ($1, $2, 0, 0, $3)
					ON CONFLICT (sku, location_code) DO UPDATE SET
						reserved = %s.inventory_availability.reserved + EXCLUDED.reserved,
						updated_at = CURRENT_TIMESTAMP`, schema, schema), sku, nextLocation, qty); errResAvail != nil {
					return errResAvail
				}
				if lineID != "" {
					if err := setLine(lineID, map[string]interface{}{"location_code": nextLocation}, ""); err != nil {
						return err
					}
				}
			}

			// Spawn a new pick task for the target store node
			newTaskID := NewDocID("TSK")
			newDocData := map[string]interface{}{
				"code":          newTaskID,
				"order_id":      orderID,
				"location_code": nextLocation,
				"status":        "Pending",
				"items":         itemsRaw,
			}
			newMarshaled, _ := json.Marshal(newDocData)
			if _, errTask := tx.Exec(fmt.Sprintf(`
				INSERT INTO %s.documents (id, doctype, data, status, created_by)
				VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system')`, schema), newTaskID, newMarshaled); errTask != nil {
				return errTask
			}
		}

	} else if newStatus == "Dispatched" {
		var cogsPaise int64
		for _, itemVal := range itemsRaw {
			item, ok := itemVal.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := item["sku"].(string)
			qty := taskItemQty(item)
			lineID, _ := item["line_id"].(string)
			// A short-picked unit never left the building: only what was
			// actually shipped leaves stock, while the line's whole
			// reservation is released either way.
			shipped := qty - int(numFromInterface(item["short_qty"]))
			if shipped < 0 {
				shipped = 0
			}

			// Deduct stock and release reserve
			if _, err = tx.Exec(fmt.Sprintf(`
				UPDATE %s.inventory_availability
				SET on_hand = GREATEST(0, on_hand - $1),
				    available = GREATEST(0, available - $1),
				    reserved = GREATEST(0, reserved - $4),
				    updated_at = CURRENT_TIMESTAMP
				WHERE sku = $2 AND location_code = $3`, schema), shipped, sku, locationCode, qty); err != nil {
				return err
			}
			if lineID != "" {
				// The reservation is now fulfilled, not released: the
				// availability update above already took it out of reserved.
				if _, err := tx.Exec(fmt.Sprintf(
					`DELETE FROM %s.inventory_reservation WHERE line_id = $1 AND location_code = $2`, schema), lineID, locationCode); err != nil {
					return err
				}
				if err := setLine(lineID, map[string]interface{}{"line_status": "Dispatched", "shipped_qty": shipped}, "Dispatched"); err != nil {
					return err
				}
			}
			if shipped > 0 {
				ledgerLines = append(ledgerLines, PostedStockLine{SKU: sku, Qty: -shipped})
				cogsPaise += int64(shipped) * ResolveCOGSUnitCostPaise(tenantID, sku, itemStandardCostRupees(tenantID, sku))
			}
		}
		if cogsPaise > 0 {
			if err := PostDoubleEntryTx(tx, tenantID, schema, "FulfillmentTask", taskID,
				map[string]int64{"5100": cogsPaise}, map[string]int64{"1200": cogsPaise}, "",
				fmt.Sprintf("FulfillmentTask:%s:DISPATCH_COGS", taskID)); err != nil {
				return fmt.Errorf("COGS posting failed, task not dispatched: %v", err)
			}
		}
	}

	// 2. Update status of the current task
	task["status"] = newStatus
	updatedBytes, err := json.Marshal(task)
	if err != nil {
		return err
	}

	if _, err = tx.Exec(fmt.Sprintf(`
		UPDATE %s.documents
		SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND doctype = 'FulfillmentTask'`, schema), updatedBytes, newStatus, taskID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// Post-commit and idempotency-keyed, exactly as POS checkout does it.
	if len(ledgerLines) > 0 {
		WriteStockLedgerLines(tenantID, locationCode, "FulfillmentTask", taskID, "", ledgerLines)
	}
	return nil
}

// ProcessReturnAnywhere is RETIRED as of Stage 47.4.1 (audit finding A-04).
//
// It is kept as a named refusal rather than deleted because it was an exported
// engine function as well as an HTTP route, and a caller inside this tree that
// still reaches for it should get a compile-time-visible, explained failure
// rather than silently finding some other path.
//
// What it did wrong, precisely: it took sale_price and cost_price from its
// caller, incremented stock with an unlocked ON CONFLICT upsert, committed
// that, and only then posted two GL reversals with no idempotency key at all.
// Its "already returned" pool came from a single SalesReturn document with the
// deterministic id "RET-<originalOrderID>", whose repeat INSERT hit a primary
// key conflict its HTTP caller discarded - so the recorded returned total never
// advanced past what the FIRST call wrote while stock and GL for every later
// call still went through. Four calls of 3 against a sale of 10 returned 12.
//
// Everything it was for now lives in the ReturnRequest aggregate
// (engines/returns.go + returns_atomic.go): eligibility checked under a lock
// on the original sale, prices resolved from the immutable sale lines, a
// tenant-scoped idempotency key, and stock + COGS + revenue + tax + refund in
// one transaction with real posting keys.
func ProcessReturnAnywhere(tenantID string, returnLocation string, originalOrderID string, items []interface{}) (totalRefund int, err error) {
	return 0, &ValidationError{
		Code: "SALESR-0131",
		Message: "the instant return path is retired (Stage 47.4.1): it could not enforce cumulative return eligibility " +
			"and posted stock and finance in separate transactions. Raise the return through CreateReturnRequestCommand " +
			"(POST /api/v1/returns), which resolves prices from the original sale, locks eligibility, and posts stock, " +
			"COGS, revenue, tax and the refund together",
	}
}
