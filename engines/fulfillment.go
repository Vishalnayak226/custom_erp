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
		`SELECT data, created_at FROM %s.documents WHERE doctype = 'POSCart' AND id = $1 AND status = 'Paid'`, schema),
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
		`SELECT created_at FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1`, schema),
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

// TransitionTaskStatus handles status workflows for picking and dispatches
func TransitionTaskStatus(tenantID string, taskID string, newStatus string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	// 1. Fetch current task document
	var docDataBytes []byte
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data FROM %s.documents 
		WHERE id = $1 AND doctype = 'FulfillmentTask'`, schema), taskID).Scan(&docDataBytes)
	if err != nil {
		return fmt.Errorf("task %s not found: %v", taskID, err)
	}

	var task map[string]interface{}
	if err := json.Unmarshal(docDataBytes, &task); err != nil {
		return err
	}

	orderID, _ := task["order_id"].(string)
	locationCode, _ := task["location_code"].(string)
	itemsRaw, _ := task["items"].([]interface{})

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	if newStatus == "Rejected" {
		// A. Cancel reservations at current location
		for _, itemVal := range itemsRaw {
			item, ok := itemVal.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := item["sku"].(string)
			qty := 0
			if q, exists := item["qty"]; exists {
				switch v := q.(type) {
				case float64:
					qty = int(v)
				case int:
					qty = v
				}
			}

			// Release reserved stock from availability count
			_, err = tx.Exec(fmt.Sprintf(`
				UPDATE %s.inventory_availability 
				SET reserved = GREATEST(0, reserved - $1), updated_at = CURRENT_TIMESTAMP 
				WHERE sku = $2 AND location_code = $3`, schema), qty, sku, locationCode)
			if err != nil {
				return err
			}
		}

		// B. Trigger re-routing rules to find next best node
		var sourcingItems []map[string]interface{}
		for _, itemVal := range itemsRaw {
			item, _ := itemVal.(map[string]interface{})
			sku, _ := item["sku"].(string)
			qty := 0
			if q, exists := item["qty"]; exists {
				switch v := q.(type) {
				case float64:
					qty = int(v)
				case int:
					qty = v
				}
			}
			sourcingItems = append(sourcingItems, map[string]interface{}{
				"sku": sku,
				"qty": qty,
			})
		}

		nextLocation, errRoute := FindBestFulfillmentNode(tenantID, sourcingItems)
		if errRoute == nil && nextLocation != "" && nextLocation != locationCode {
			// Create reservations at the new location node
			for _, item := range sourcingItems {
				sku := item["sku"].(string)
				qty := item["qty"].(int)
				// Create new reservation (Stage 28: tenant-configured hold TTL;
				// inline here rather than via CreateReservation because this runs
				// inside the caller's existing transaction tx).
				expiresAt := time.Now().Add(time.Duration(GetSettingInt(tenantID, "inventory.reservation_ttl_seconds")) * time.Second)
				_, errRes := tx.Exec(fmt.Sprintf(`
					INSERT INTO %s.inventory_reservation (sku, location_code, quantity, reservation_type, expires_at) 
					VALUES ($1, $2, $3, 'Online', $4)`, schema), sku, nextLocation, qty, expiresAt)
				if errRes != nil {
					return errRes
				}

				// Update reservation count in availability read model
				_, errResAvail := tx.Exec(fmt.Sprintf(`
					INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available, reserved) 
					VALUES ($1, $2, 0, 0, $3) 
					ON CONFLICT (sku, location_code) DO UPDATE SET 
						reserved = %s.inventory_availability.reserved + EXCLUDED.reserved, 
						updated_at = CURRENT_TIMESTAMP`, schema, schema), sku, nextLocation, qty)
				if errResAvail != nil {
					return errResAvail
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
			_, errTask := tx.Exec(fmt.Sprintf(`
				INSERT INTO %s.documents (id, doctype, data, status, created_by) 
				VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system')`, schema), newTaskID, newMarshaled)
			if errTask != nil {
				return errTask
			}
		}

	} else if newStatus == "Dispatched" {
		// Finalize stock reduction (deduct physical on-hand and release reservation)
		for _, itemVal := range itemsRaw {
			item, ok := itemVal.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := item["sku"].(string)
			qty := 0
			if q, exists := item["qty"]; exists {
				switch v := q.(type) {
				case float64:
					qty = int(v)
				case int:
					qty = v
				}
			}

			// Deduct stock and release reserve
			_, err = tx.Exec(fmt.Sprintf(`
				UPDATE %s.inventory_availability 
				SET on_hand = GREATEST(0, on_hand - $1), 
				    available = GREATEST(0, available - $1), 
				    reserved = GREATEST(0, reserved - $1), 
				    updated_at = CURRENT_TIMESTAMP 
				WHERE sku = $2 AND location_code = $3`, schema), qty, sku, locationCode)
			if err != nil {
				return err
			}
		}
	}

	// 2. Update status of the current task
	task["status"] = newStatus
	updatedBytes, err := json.Marshal(task)
	if err != nil {
		return err
	}

	_, err = tx.Exec(fmt.Sprintf(`
		UPDATE %s.documents 
		SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP 
		WHERE id = $3 AND doctype = 'FulfillmentTask'`, schema), updatedBytes, newStatus, taskID)
	if err != nil {
		return err
	}

	return tx.Commit()
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
