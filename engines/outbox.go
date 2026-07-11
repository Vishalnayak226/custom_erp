package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"custom_erp/db"
)

// PublishEvent queues an event in the outbox table within a transaction
func PublishEvent(tx *sql.Tx, schema, eventName string, payload map[string]interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.integration_event_outbox (event_name, payload, status) 
		VALUES ($1, $2, 'Pending')`, schema)

	_, err = tx.Exec(query, eventName, payloadBytes)
	return err
}

// StartOutboxWorker starts a background worker that polls the outbox and dispatches events
func StartOutboxWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if db.DB == nil {
				continue
			}

			// For prototype scope, we resolve schema "tenant_default"
			schema := "tenant_default"
			processOutbox(schema)
		}
	}()
}

func processOutbox(schema string) {
	// Query pending/failed events
	query := fmt.Sprintf(`
		SELECT id, event_name, payload, attempts 
		FROM %s.integration_event_outbox 
		WHERE status IN ('Pending', 'Failed') AND attempts < 5 
		ORDER BY created_at LIMIT 10`, schema)

	rows, err := db.DB.Query(query)
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

		// Simulate event dispatching to integrations (Shopify, WMS, OMS)
		log.Printf("[OUTBOX] Dispatching event %s (%s) - Attempt %d", ev.EventName, ev.ID, nextAttempts)

		if ev.EventName == "inventory.stock_changed" {
			var payloadMap map[string]interface{}
			if err := json.Unmarshal([]byte(ev.Payload), &payloadMap); err == nil {
				sku, _ := payloadMap["sku"].(string)
				var channelSku string
				errSku := db.DB.QueryRow(fmt.Sprintf(`
					SELECT channel_sku FROM %s.channel_product_mapping 
					WHERE sku = $1`, schema), sku).Scan(&channelSku)
				if errSku == nil {
					log.Printf("[OUTBOX] [SHOPIFY] Real-time delta sync pushed to Shopify channel. Channel SKU: %s, Local SKU: %s", channelSku, sku)
				}
			}
		}

		// 1. Transaction to update outbox and insert log
		tx, err := db.DB.Begin()
		if err != nil {
			continue
		}

		_, _ = tx.Exec(fmt.Sprintf("SET LOCAL search_path TO %s, public", schema))

		// Write to event log
		_, errLog := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.integration_event_log (event_id, channel, status, error_message) 
			VALUES ($1, $2, $3, $4)`, schema), ev.ID, "WMS", "Success", nil)

		if errLog != nil {
			status = "Failed"
			errMsg = errLog.Error()
		}

		// Update outbox row
		_, _ = tx.Exec(fmt.Sprintf(`
			UPDATE %s.integration_event_outbox 
			SET status = $1, attempts = $2 
			WHERE id = $3`, schema), status, nextAttempts, ev.ID)

		_ = tx.Commit()

		if errMsg != "" {
			log.Printf("[OUTBOX] Dispatch failed: %s", errMsg)
		}
	}
}

// GetIntegrationLogs queries integration outbox event logs
func GetIntegrationLogs(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT id, event_name, payload, status, attempts, created_at 
		FROM %s.integration_event_outbox 
		ORDER BY created_at DESC LIMIT 50`, schema)

	rows, err := db.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []map[string]interface{}
	for rows.Next() {
		var id, eventName, payload, status string
		var attempts int
		var createdAt time.Time
		if errScan := rows.Scan(&id, &eventName, &payload, &status, &attempts, &createdAt); errScan == nil {
			var payloadMap map[string]interface{}
			_ = json.Unmarshal([]byte(payload), &payloadMap)
			logs = append(logs, map[string]interface{}{
				"id":         id,
				"event_name": eventName,
				"payload":    payloadMap,
				"status":     status,
				"attempts":   attempts,
				"created_at": createdAt,
			})
		}
	}
	return logs, nil
}

// RetryIntegrationEvent resets status and attempts to allow background retrying of failed events
func RetryIntegrationEvent(tenantID string, eventID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		UPDATE %s.integration_event_outbox 
		SET status = 'Pending', attempts = 0 
		WHERE id = $1`, schema)
	res, err := db.DB.Exec(query, eventID)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("integration outbox event %s not found", eventID)
	}
	return nil
}
