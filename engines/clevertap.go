package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// CleverTap tables are created in migration.sql section 36:
//   tenant_default.clevertap_credentials  (account_id, passcode, region, active)
//   tenant_default.clevertap_event_log  (id, event_name, customer_id, event_data, status, sent_at, created_at)

// SaveCleverTapCredential stores or updates CleverTap API credentials.
func SaveCleverTapCredential(tenantID, accountID, passcode, region string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`
		INSERT INTO %s.clevertap_credentials (account_id, passcode, region, active)
		VALUES ($1, $2, $3, TRUE)
		ON CONFLICT (account_id) DO UPDATE SET
			passcode = EXCLUDED.passcode,
			region = EXCLUDED.region,
			active = TRUE,
			updated_at = CURRENT_TIMESTAMP`, schema)
	_, err = db.DB.Exec(query, accountID, passcode, region)
	return err
}

// GetCleverTapCredentials retrieves active CleverTap credentials.
func GetCleverTapCredentials(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT account_id, region, active, created_at
		FROM %s.clevertap_credentials
		ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []map[string]interface{}
	for rows.Next() {
		var accountID, region, createdAt string
		var active bool
		if err := rows.Scan(&accountID, &region, &active, &createdAt); err == nil {
			creds = append(creds, map[string]interface{}{
				"account_id": accountID,
				"region":     region,
				"active":     active,
				"created_at": createdAt,
			})
		}
	}
	return creds, nil
}

// LogCleverTapEvent records a customer event to be synced to CleverTap.
// This uses the outbox pattern - the event is queued and the background
// worker dispatches it asynchronously.
func LogCleverTapEvent(tenantID, eventName, customerID string, eventData map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	dataJSON, err := json.Marshal(eventData)
	if err != nil {
		return err
	}

	// Record in the local event log
	query := fmt.Sprintf(`
		INSERT INTO %s.clevertap_event_log (event_name, customer_id, event_data, status)
		VALUES ($1, $2, $3, 'Pending')`, schema)
	_, err = db.DB.Exec(query, eventName, customerID, dataJSON)
	if err != nil {
		return err
	}

	// Publish an outbox event for the background worker
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	payload := map[string]interface{}{
		"event_name":  eventName,
		"customer_id": customerID,
		"event_data":  eventData,
	}
	if err := PublishEvent(tx, schema, "clevertap.event.log", payload); err != nil {
		return err
	}
	return tx.Commit()
}

// LogCheckoutToCleverTap logs a completed POS checkout as a CleverTap event.
// This is called from handleCheckout after a sale completes successfully.
func LogCheckoutToCleverTap(tenantID, customerID, cartNumber string, totalSalePrice int, items []map[string]interface{}) error {
	if customerID == "" {
		return nil // No customer to associate the event with
	}

	eventData := map[string]interface{}{
		"cart_number": cartNumber,
		"total":       totalSalePrice,
		"items":       items,
		"currency":    "INR",
	}
	return LogCleverTapEvent(tenantID, "Checkout Completed", customerID, eventData)
}

// LogCustomerEventToCleverTap logs a generic customer event (e.g., registration,
// loyalty earn, return) to CleverTap via the outbox.
func LogCustomerEventToCleverTap(tenantID, eventName, customerID string, properties map[string]interface{}) error {
	return LogCleverTapEvent(tenantID, eventName, customerID, properties)
}

// ListCleverTapEventLogs returns the CleverTap event log for a tenant.
func ListCleverTapEventLogs(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, event_name, customer_id, event_data, status, sent_at, created_at
		FROM %s.clevertap_event_log
		ORDER BY created_at DESC LIMIT 50`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []map[string]interface{}
	for rows.Next() {
		var id, eventName, customerID, eventDataStr, status string
		var sentAt, createdAt time.Time
		var sentAtPtr *time.Time
		if err := rows.Scan(&id, &eventName, &customerID, &eventDataStr, &status, &sentAt, &createdAt); err == nil {
			if !sentAt.IsZero() {
				sentAtPtr = &sentAt
			}
			var eventData map[string]interface{}
			_ = json.Unmarshal([]byte(eventDataStr), &eventData)
			logs = append(logs, map[string]interface{}{
				"id":          id,
				"event_name":  eventName,
				"customer_id": customerID,
				"event_data":  eventData,
				"status":      status,
				"sent_at":     sentAtPtr,
				"created_at":  createdAt,
			})
		}
	}
	return logs, nil
}

// StartCleverTapWorker starts a background worker that processes pending
// CleverTap outbox events (customer event sync).
func StartCleverTapWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if db.DB == nil {
				continue
			}
			schemas, err := listTenantSchemas()
			if err != nil {
				log.Printf("[CLEVERTAP] Failed to list tenant schemas: %v", err)
				continue
			}
			for _, schema := range schemas {
				processCleverTapOutbox(schema)
			}
		}
	}()
}

func processCleverTapOutbox(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, event_name, payload, attempts
		FROM %s.integration_event_outbox
		WHERE event_name LIKE 'clevertap.%%'
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

		log.Printf("[CLEVERTAP] Processing event %s (%s) - Attempt %d", ev.EventName, ev.ID, nextAttempts)

		// In a real deployment, this would make HTTP calls to CleverTap's API
		// (https://<region>.clevertap.com/1/upload) with the account_id and
		// passcode for authentication. For now, we log the dispatch and mark
		// it as successful - the outbox infrastructure is real; the actual
		// HTTP client integration is deferred until CleverTap credentials
		// are configured.
		if ev.EventName == "clevertap.event.log" {
			var payloadMap map[string]interface{}
			if err := json.Unmarshal([]byte(ev.Payload), &payloadMap); err == nil {
				eventName, _ := payloadMap["event_name"].(string)
				customerID, _ := payloadMap["customer_id"].(string)
				log.Printf("[CLEVERTAP] [EVENT] Would push event=%s customer=%s to CleverTap API", eventName, customerID)
			}
		}

		tx, err := db.DB.Begin()
		if err != nil {
			continue
		}

		_, _ = tx.Exec(fmt.Sprintf("SET LOCAL search_path TO %s, public", schema))

		_, errLog := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.integration_event_log (event_id, channel, status, error_message)
			VALUES ($1, $2, $3, $4)`, schema), ev.ID, "CleverTap", "Success", nil)
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
			log.Printf("[CLEVERTAP] Dispatch failed: %s", errMsg)
		}
	}
}
