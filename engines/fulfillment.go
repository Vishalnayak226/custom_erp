package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// CreateFulfillmentTasks registers a store-level pick task
func CreateFulfillmentTasks(tenantID string, orderID string, locationCode string, items []interface{}) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	taskID := fmt.Sprintf("TSK-%d", time.Now().UnixNano())
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
				// Create new reservation
				expiresAt := time.Now().Add(86400 * time.Second)
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
			newTaskID := fmt.Sprintf("TSK-%d", time.Now().UnixNano())
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

// ProcessReturnAnywhere processes sales returns at any store, updating inventory and general ledger
func ProcessReturnAnywhere(tenantID string, returnLocation string, originalOrderID string, items []interface{}) error {
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

	totalSalePrice := 0
	totalCostPrice := 0

	// 1. Process returned item inventory delta sync
	for _, itemVal := range items {
		itemMap, ok := itemVal.(map[string]interface{})
		if !ok {
			continue
		}

		sku, _ := itemMap["sku"].(string)
		qty := 0
		if q, exists := itemMap["qty"]; exists {
			switch v := q.(type) {
			case float64:
				qty = int(v)
			case int:
				qty = v
			}
		}

		// A return only ever adds stock back - unlike checkout, there's no legitimate
		// negative-qty case here. Without this check a negative qty silently reduces
		// the return location's stock with no floor/lock (the increment below is a
		// bare ON CONFLICT DO UPDATE, not the row-locked floor-checked path checkout
		// uses), which is exactly the "negative stock" loophole applied to this handler.
		if qty <= 0 {
			return fmt.Errorf("return quantity must be positive (sku=%q, qty=%d)", sku, qty)
		}

		salePrice := 0
		if p, exists := itemMap["sale_price"]; exists {
			switch v := p.(type) {
			case float64:
				salePrice = int(v)
			case int:
				salePrice = v
			}
		}

		costPrice := 0
		if p, exists := itemMap["cost_price"]; exists {
			switch v := p.(type) {
			case float64:
				costPrice = int(v)
			case int:
				costPrice = v
			}
		}

		totalSalePrice += salePrice * qty
		totalCostPrice += costPrice * qty

		// Increment return location stock
		_, err = tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) 
			VALUES ($1, $2, $3, $3) 
			ON CONFLICT (sku, location_code) DO UPDATE SET 
				on_hand = %s.inventory_availability.on_hand + EXCLUDED.on_hand, 
				available = %s.inventory_availability.available + EXCLUDED.available, 
				updated_at = CURRENT_TIMESTAMP`, schema, schema, schema), sku, returnLocation, qty)
		if err != nil {
			return err
		}
	}

	// 2. Commit DB transaction first before using Finance engine
	err = tx.Commit()
	if err != nil {
		return err
	}

	// 3. Post double-entry reverse finance bookings (debit Revenue, credit Cash/Bank; debit Inventory, credit COGS)
	revenueDebits := map[string]int{"4100": totalSalePrice}  // Debit: Sales Revenue (reduce revenue)
	revenueCredits := map[string]int{"1100": totalSalePrice} // Credit: Cash/Bank (refund customer)
	err = PostDoubleEntry(tenantID, "SalesReturn", originalOrderID, revenueDebits, revenueCredits)
	if err != nil {
		return err
	}

	inventoryDebits := map[string]int{"1200": totalCostPrice}  // Debit: Inventory Control (receive stock)
	inventoryCredits := map[string]int{"5100": totalCostPrice} // Credit: Cost of Goods Sold (reduce COGS)
	return PostDoubleEntry(tenantID, "SalesReturn", originalOrderID, inventoryDebits, inventoryCredits)
}
