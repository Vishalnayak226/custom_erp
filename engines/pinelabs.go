package engines

import (
	"custom_erp/db"
	"fmt"
	"log"
	"time"
)

// Pine Labs tables are created in migration.sql section 35:
//   tenant_default.pinelabs_credentials  (terminal_id, api_key, merchant_id, base_url, active)
//   tenant_default.pinelabs_transactions  (transaction_id, terminal_id, cart_number, amount, status, payment_mode, reconciled, reconciled_at, created_at)

// SavePineLabsCredential stores or updates Pine Labs Plutus terminal credentials.
func SavePineLabsCredential(tenantID, terminalID, apiKey, merchantID, baseURL string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`
		INSERT INTO %s.pinelabs_credentials (terminal_id, api_key, merchant_id, base_url, active)
		VALUES ($1, $2, $3, $4, TRUE)
		ON CONFLICT (terminal_id) DO UPDATE SET
			api_key = EXCLUDED.api_key,
			merchant_id = EXCLUDED.merchant_id,
			base_url = EXCLUDED.base_url,
			active = TRUE,
			updated_at = CURRENT_TIMESTAMP`, schema)
	_, err = db.DB.Exec(query, terminalID, apiKey, merchantID, baseURL)
	return err
}

// GetPineLabsCredentials retrieves active Pine Labs terminal credentials.
func GetPineLabsCredentials(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT terminal_id, merchant_id, base_url, active, created_at
		FROM %s.pinelabs_credentials
		ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []map[string]interface{}
	for rows.Next() {
		var terminalID, merchantID, baseURL, createdAt string
		var active bool
		if err := rows.Scan(&terminalID, &merchantID, &baseURL, &active, &createdAt); err == nil {
			creds = append(creds, map[string]interface{}{
				"terminal_id": terminalID,
				"merchant_id": merchantID,
				"base_url":    baseURL,
				"active":      active,
				"created_at":  createdAt,
			})
		}
	}
	return creds, nil
}

// RecordPineLabsTransaction records a payment transaction from a Pine Labs
// Plutus terminal into the ERP. This is called when a POS checkout is
// completed via a Pine Labs terminal (the terminal sends a payment
// confirmation callback, or the cashier enters the terminal response code).
func RecordPineLabsTransaction(tenantID, transactionID, terminalID, cartNumber string, amount float64, paymentMode string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	// Check for duplicate transaction
	var exists bool
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1 FROM %s.pinelabs_transactions WHERE transaction_id = $1
		)`, schema), transactionID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("TRANSACTION_ALREADY_RECORDED")
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.pinelabs_transactions (transaction_id, terminal_id, cart_number, amount, status, payment_mode)
		VALUES ($1, $2, $3, $4, 'Completed', $5)`, schema)
	_, err = db.DB.Exec(query, transactionID, terminalID, cartNumber, amount, paymentMode)
	if err != nil {
		return err
	}

	LogAuditEvent(tenantID, "system", "PINELABS_TRANSACTION_RECORDED", "SUCCESS",
		fmt.Sprintf("txn=%s terminal=%s cart=%s amount=%.2f", transactionID, terminalID, cartNumber, amount))
	return nil
}

// ReconcilePineLabsTransactions matches Pine Labs terminal transactions
// against POSCart documents to ensure every terminal payment has a
// corresponding completed sale in the ERP. Returns a reconciliation summary.
func ReconcilePineLabsTransactions(tenantID string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	// Find all unreconciled Pine Labs transactions
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT pt.id, pt.transaction_id, pt.terminal_id, pt.cart_number, pt.amount, pt.payment_mode, pt.created_at
		FROM %s.pinelabs_transactions pt
		WHERE pt.reconciled = FALSE
		ORDER BY pt.created_at ASC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reconciled int
	var failed int
	var errors []string

	for rows.Next() {
		var id int
		var transactionID, terminalID, cartNumber, paymentMode, createdAt string
		var amount float64
		if err := rows.Scan(&id, &transactionID, &terminalID, &cartNumber, &amount, &paymentMode, &createdAt); err != nil {
			continue
		}

		// Check if the corresponding POSCart exists and is Paid
		var cartStatus string
		err = db.DB.QueryRow(fmt.Sprintf(`
			SELECT status FROM %s.documents
			WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber).Scan(&cartStatus)
		if err != nil {
			errors = append(errors, fmt.Sprintf("txn=%s: cart %s not found", transactionID, cartNumber))
			failed++
			continue
		}

		if cartStatus != "Paid" {
			errors = append(errors, fmt.Sprintf("txn=%s: cart %s status is %s (expected Paid)", transactionID, cartNumber, cartStatus))
			failed++
			continue
		}

		// Mark as reconciled
		_, err = db.DB.Exec(fmt.Sprintf(`
			UPDATE %s.pinelabs_transactions
			SET reconciled = TRUE, reconciled_at = CURRENT_TIMESTAMP
			WHERE id = $1`, schema), id)
		if err != nil {
			errors = append(errors, fmt.Sprintf("txn=%s: failed to mark reconciled: %v", transactionID, err))
			failed++
			continue
		}
		reconciled++
	}

	result := map[string]interface{}{
		"reconciled": reconciled,
		"failed":     failed,
		"errors":     errors,
	}
	return result, nil
}

// ListPineLabsTransactions returns all Pine Labs transactions for a tenant.
func ListPineLabsTransactions(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT transaction_id, terminal_id, cart_number, amount, status, payment_mode, reconciled, created_at
		FROM %s.pinelabs_transactions
		ORDER BY created_at DESC LIMIT 50`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []map[string]interface{}
	for rows.Next() {
		var transactionID, terminalID, cartNumber, status, paymentMode, createdAt string
		var amount float64
		var reconciled bool
		if err := rows.Scan(&transactionID, &terminalID, &cartNumber, &amount, &status, &paymentMode, &reconciled, &createdAt); err == nil {
			txns = append(txns, map[string]interface{}{
				"transaction_id": transactionID,
				"terminal_id":    terminalID,
				"cart_number":    cartNumber,
				"amount":         amount,
				"status":         status,
				"payment_mode":   paymentMode,
				"reconciled":     reconciled,
				"created_at":     createdAt,
			})
		}
	}
	return txns, nil
}

// StartPineLabsReconciliationWorker periodically runs reconciliation of
// unreconciled Pine Labs transactions against POSCart documents.
func StartPineLabsReconciliationWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if db.DB == nil {
				continue
			}
			schemas, err := listTenantSchemas()
			if err != nil {
				log.Printf("[PINELABS] Failed to list tenant schemas: %v", err)
				continue
			}
			for _, schema := range schemas {
				tenantID := schemaToTenantID(schema)
				if tenantID == "" {
					continue
				}
				result, err := ReconcilePineLabsTransactions(tenantID)
				if err != nil {
					log.Printf("[PINELABS] Reconciliation failed for %s: %v", schema, err)
					continue
				}
				rec, _ := result["reconciled"].(int)
				fail, _ := result["failed"].(int)
				if rec > 0 || fail > 0 {
					log.Printf("[PINELABS] Reconciliation for %s: %d reconciled, %d failed", schema, rec, fail)
				}
			}
		}
	}()
}

// schemaToTenantID is a helper to resolve a schema name back to a tenant_id.
func schemaToTenantID(schema string) string {
	var tenantID string
	err := db.DB.QueryRow("SELECT tenant_id FROM public.tenants WHERE schema_name = $1", schema).Scan(&tenantID)
	if err != nil {
		return ""
	}
	return tenantID
}
