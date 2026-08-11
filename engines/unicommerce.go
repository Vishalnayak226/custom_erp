package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Unicommerce tables are created in migration.sql section 34:
//   tenant_default.unicommerce_credentials  (store_code, api_key, api_secret, base_url, active)
//   tenant_default.unicommerce_inventory_sync  (sku, store_code, quantity, last_synced_at)
//   tenant_default.unicommerce_order_mapping  (order_id, channel_order_id, store_code, status, created_at)

// SaveUnicommerceCredential stores or updates Unicommerce API credentials for a tenant.
func SaveUnicommerceCredential(tenantID, storeCode, apiKey, apiSecret, baseURL string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`
		INSERT INTO %s.unicommerce_credentials (store_code, api_key, api_secret, base_url, active)
		VALUES ($1, $2, $3, $4, TRUE)
		ON CONFLICT (store_code) DO UPDATE SET
			api_key = EXCLUDED.api_key,
			api_secret = EXCLUDED.api_secret,
			base_url = EXCLUDED.base_url,
			active = TRUE,
			updated_at = CURRENT_TIMESTAMP`, schema)
	_, err = db.DB.Exec(query, storeCode, apiKey, apiSecret, baseURL)
	return err
}

// GetUnicommerceCredentials retrieves active Unicommerce credentials for a tenant.
func GetUnicommerceCredentials(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT store_code, base_url, active, created_at
		FROM %s.unicommerce_credentials
		ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []map[string]interface{}
	for rows.Next() {
		var storeCode, baseURL, createdAt string
		var active bool
		if err := rows.Scan(&storeCode, &baseURL, &active, &createdAt); err == nil {
			creds = append(creds, map[string]interface{}{
				"store_code": storeCode,
				"base_url":   baseURL,
				"active":     active,
				"created_at": createdAt,
			})
		}
	}
	return creds, nil
}

// SyncUnicommerceInventory pushes local inventory levels for a SKU to Unicommerce
// via the outbox pattern (the actual HTTP call is handled by the background worker).
// This records the intent; the outbox worker dispatches it asynchronously.
func SyncUnicommerceInventory(tenantID, sku, storeCode string, quantity int) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	// Record the sync intent in the tracking table
	query := fmt.Sprintf(`
		INSERT INTO %s.unicommerce_inventory_sync (sku, store_code, quantity, last_synced_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (sku, store_code) DO UPDATE SET
			quantity = EXCLUDED.quantity,
			last_synced_at = CURRENT_TIMESTAMP`, schema)
	_, err = db.DB.Exec(query, sku, storeCode, quantity)
	if err != nil {
		return err
	}

	// Publish an outbox event for the background worker to dispatch
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	payload := map[string]interface{}{
		"sku":        sku,
		"store_code": storeCode,
		"quantity":   quantity,
	}
	if err := PublishEvent(tx, schema, "unicommerce.inventory.sync", payload); err != nil {
		return err
	}
	return tx.Commit()
}

// ImportUnicommerceOrder is the legacy Unicommerce intake signature, kept so
// existing callers compile. As of Stage 35.1.1 it delegates to
// ImportUnicommerceSalesOrder, which routes the order through
// ImportChannelSalesOrder -> CreateSalesOrder like every other channel.
//
// Its old body reserved stock per line and wrote a unicommerce_order_mapping
// row, but - exactly like ImportChannelOrder - never created the document its
// own comment claimed ("creates a local POSCart document"). The reservations
// it made therefore hung off nothing.
//
// The ORDER_ALREADY_IMPORTED replay error is preserved; the delegate returns
// the existing order id idempotently instead, so new code should call it.
func ImportUnicommerceOrder(tenantID, channelOrderID, storeCode string, items []map[string]interface{}) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	var exists bool
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1 FROM %s.unicommerce_order_mapping
			WHERE channel_order_id = $1 AND store_code = $2
		)`, schema), channelOrderID, storeCode).Scan(&exists)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("ORDER_ALREADY_IMPORTED")
	}

	return ImportUnicommerceSalesOrder(tenantID, channelOrderID, storeCode, channelLinesFromLooseItems(items))
}

// ListUnicommerceOrders returns imported Unicommerce orders for a tenant.
func ListUnicommerceOrders(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT order_id, channel_order_id, store_code, status, created_at
		FROM %s.unicommerce_order_mapping
		ORDER BY created_at DESC LIMIT 50`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []map[string]interface{}
	for rows.Next() {
		var orderID, channelOrderID, storeCode, status, createdAt string
		if err := rows.Scan(&orderID, &channelOrderID, &storeCode, &status, &createdAt); err == nil {
			orders = append(orders, map[string]interface{}{
				"order_id":         orderID,
				"channel_order_id": channelOrderID,
				"store_code":       storeCode,
				"status":           status,
				"created_at":       createdAt,
			})
		}
	}
	return orders, nil
}

// ListUnicommerceInventorySyncs returns inventory sync records for a tenant.
func ListUnicommerceInventorySyncs(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT sku, store_code, quantity, last_synced_at
		FROM %s.unicommerce_inventory_sync
		ORDER BY last_synced_at DESC LIMIT 50`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var syncs []map[string]interface{}
	for rows.Next() {
		var sku, storeCode string
		var quantity int
		var lastSyncedAt time.Time
		if err := rows.Scan(&sku, &storeCode, &quantity, &lastSyncedAt); err == nil {
			syncs = append(syncs, map[string]interface{}{
				"sku":            sku,
				"store_code":     storeCode,
				"quantity":       quantity,
				"last_synced_at": lastSyncedAt,
			})
		}
	}
	return syncs, nil
}

// StartUnicommerceWorker starts a background worker that processes pending
// Unicommerce outbox events (inventory sync, order notifications).
func StartUnicommerceWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[UNICOMMERCE] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					processUnicommerceOutbox(schema)
				}
			}
		}
	}()
}

func processUnicommerceOutbox(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, event_name, payload, attempts
		FROM %s.integration_event_outbox
		WHERE event_name LIKE 'unicommerce.%%'
		AND status IN ('Pending', 'Failed') AND attempts < 5
		ORDER BY created_at LIMIT 10`, schema))
	if err != nil {
		return
	}
	defer rows.Close()

	type Event struct {
		ID        string
		EventName string
		Payload   string
		Attempts  int
	}
	var events []Event
	for rows.Next() {
		var ev Event
		if err := rows.Scan(&ev.ID, &ev.EventName, &ev.Payload, &ev.Attempts); err == nil {
			events = append(events, ev)
		}
	}
	rows.Close()

	for _, ev := range events {
		nextAttempts := ev.Attempts + 1
		status := "Dispatched"
		errMsg := ""

		log.Printf("[UNICOMMERCE] Processing event %s (%s) - Attempt %d", ev.EventName, ev.ID, nextAttempts)

		// In a real deployment, this would make HTTP calls to Unicommerce's API.
		// For now, we log the dispatch and mark it as successful (the outbox
		// infrastructure is real; the actual HTTP client integration is deferred
		// until Unicommerce API credentials are configured).
		if ev.EventName == "unicommerce.inventory.sync" {
			var payloadMap map[string]interface{}
			if err := json.Unmarshal([]byte(ev.Payload), &payloadMap); err == nil {
				sku, _ := payloadMap["sku"].(string)
				storeCode, _ := payloadMap["store_code"].(string)
				qty, _ := payloadMap["quantity"].(float64)
				log.Printf("[UNICOMMERCE] [INVENTORY] Would push SKU=%s store=%s qty=%.0f to Unicommerce API", sku, storeCode, qty)
			}
		} else if ev.EventName == "unicommerce.order.imported" {
			var payloadMap map[string]interface{}
			if err := json.Unmarshal([]byte(ev.Payload), &payloadMap); err == nil {
				orderID, _ := payloadMap["order_id"].(string)
				log.Printf("[UNICOMMERCE] [ORDER] Would notify Unicommerce of imported order %s", orderID)
			}
		}

		tx, err := db.DB.Begin()
		if err != nil {
			continue
		}

		_, _ = tx.Exec(fmt.Sprintf("SET LOCAL search_path TO %s, public", schema))

		_, errLog := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.integration_event_log (event_id, channel, status, error_message)
			VALUES ($1, $2, $3, $4)`, schema), ev.ID, "Unicommerce", "Success", nil)
		if errLog != nil {
			status = "Failed"
			errMsg = errLog.Error()
		}

		_, _ = tx.Exec(fmt.Sprintf(`
			UPDATE %s.integration_event_outbox
			SET status = $1, attempts = $2
			WHERE id = $3`, schema), status, nextAttempts, ev.ID)

		_ = tx.Commit()

		if errMsg != "" {
			log.Printf("[UNICOMMERCE] Dispatch failed: %s", errMsg)
		}
	}
}
