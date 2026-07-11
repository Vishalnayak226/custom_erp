package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
)

// FindBestFulfillmentNode resolves the location node with the highest ATS stock for all requested items
func FindBestFulfillmentNode(tenantID string, items []map[string]interface{}) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	// 1. Fetch all availability records for these SKUs
	if len(items) == 0 {
		return "", errors.New("no items specified for sourcing")
	}

	// Query unique locations
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT DISTINCT location_code 
		FROM %s.inventory_availability`, schema))
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var locations []string
	for rows.Next() {
		var loc string
		if err := rows.Scan(&loc); err == nil {
			locations = append(locations, loc)
		}
	}
	rows.Close()

	bestLocation := ""
	maxTotalATS := -1

	// 2. Evaluate each location node
	for _, loc := range locations {
		hasAllItems := true
		totalATS := 0

		for _, item := range items {
			sku, _ := item["sku"].(string)
			reqQty, _ := item["qty"].(int)

			var available, reserved, safetyStock int
			err = db.DB.QueryRow(fmt.Sprintf(`
				SELECT available, reserved, safety_stock 
				FROM %s.inventory_availability 
				WHERE sku = $1 AND location_code = $2`, schema), sku, loc).Scan(&available, &reserved, &safetyStock)
			if err == sql.ErrNoRows {
				hasAllItems = false
				break
			} else if err != nil {
				return "", err
			}

			ats := available - reserved - safetyStock
			if ats < reqQty {
				hasAllItems = false
				break
			}
			totalATS += ats
		}

		if hasAllItems && totalATS > maxTotalATS {
			maxTotalATS = totalATS
			bestLocation = loc
		}
	}

	if bestLocation == "" {
		// Fallback to HO default node if no specific store has enough stock
		return "HO", nil
	}

	return bestLocation, nil
}

// ImportChannelOrder validates and imports an external order, reserving stock atomically
func ImportChannelOrder(tenantID string, channel string, channelOrderID string, items []map[string]interface{}) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	// 1. Check idempotency: has this order already been processed?
	var exists bool
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1 FROM %s.channel_order_mapping 
			WHERE channel_order_id = $1 AND channel = $2
		)`, schema), channelOrderID, channel).Scan(&exists)
	if err != nil {
		return "", err
	}
	if exists {
		return "", errors.New("ORDER_ALREADY_IMPORTED")
	}

	// 2. Map channel SKUs to ERP SKUs
	var mappedItems []map[string]interface{}
	for _, item := range items {
		channelSku, _ := item["sku"].(string)
		qty, _ := item["qty"].(int)

		var erpSku string
		err = db.DB.QueryRow(fmt.Sprintf(`
			SELECT sku FROM %s.channel_product_mapping 
			WHERE channel_sku = $1 AND channel = $2`, schema), channelSku, channel).Scan(&erpSku)
		if err == sql.ErrNoRows {
			// Fallback to channel SKU string itself
			erpSku = channelSku
		} else if err != nil {
			return "", err
		}

		mappedItems = append(mappedItems, map[string]interface{}{
			"sku": erpSku,
			"qty": qty,
		})
	}

	// 3. Find the best fulfillment location node
	location, err := FindBestFulfillmentNode(tenantID, mappedItems)
	if err != nil {
		return "", err
	}

	// 4. Create stock reservations
	for _, item := range mappedItems {
		sku := item["sku"].(string)
		qty := item["qty"].(int)
		_, err = CreateReservation(tenantID, sku, location, qty, "Online", 86400) // 24hr expiration
		if err != nil {
			return "", fmt.Errorf("failed to reserve stock for SKU %s at node %s: %v", sku, location, err)
		}
	}

	// 5. Create POSCart document inside ERP in 'Reserved' status
	orderID := fmt.Sprintf("ORD-%s-%s", channel, channelOrderID)
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}

	// Save mapping
	_, err = tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.channel_order_mapping (order_id, channel, channel_order_id) 
		VALUES ($1, $2, $3)`, schema), orderID, channel, channelOrderID)
	if err != nil {
		return "", err
	}

	err = tx.Commit()
	return orderID, err
}

// MapChannelProduct registers mapping records between external channels and internal SKUs
func MapChannelProduct(tenantID string, channel string, sku string, channelSku string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.channel_product_mapping (sku, channel, channel_sku) 
		VALUES ($1, $2, $3) 
		ON CONFLICT (sku, channel) DO UPDATE SET 
			channel_sku = EXCLUDED.channel_sku, 
			updated_at = CURRENT_TIMESTAMP`, schema)
	_, err = db.DB.Exec(query, sku, channel, channelSku)
	return err
}
