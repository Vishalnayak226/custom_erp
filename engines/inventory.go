package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"
)

// GenerateBarcode returns a unique 10-digit barcode string prefixing 'BAR'
func GenerateBarcode() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	num := r.Intn(9000000) + 1000000
	return fmt.Sprintf("BAR%d", num)
}

// GetStockBalance derives item current stock by summing ledger entries
func GetStockBalance(tenantID string, itemID string, warehouseID string) (float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}

	query := fmt.Sprintf(`
		SELECT COALESCE(SUM((data->>'qty')::numeric), 0) 
		FROM %s.documents 
		WHERE doctype = 'StockLedgerEntry' 
		  AND data->>'item_id' = $1 
		  AND data->>'warehouse_id' = $2`, schema)

	var balance float64
	err = db.DB.QueryRow(query, itemID, warehouseID).Scan(&balance)
	if err != nil {
		return 0, err
	}
	return balance, nil
}

// WriteStockLedgerEntry writes an append-only inventory card record
func WriteStockLedgerEntry(tenantID string, itemID string, warehouseID string, qty float64, voucherType string, voucherID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	id := fmt.Sprintf("SLE%d", time.Now().UnixNano())
	docData := map[string]interface{}{
		"id":           id,
		"code":         id,
		"item_id":      itemID,
		"warehouse_id": warehouseID,
		"qty":          qty,
		"voucher_type": voucherType,
		"voucher_id":   voucherID,
		"status":       "Active",
	}

	marshaled, err := json.Marshal(docData)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by) 
		VALUES ($1, $2, $3, $4, $5)`, schema)
	_, err = db.DB.Exec(query, id, "StockLedgerEntry", marshaled, "Active", "system")
	return err
}

// PostInventoryLedger updates the available stock levels upon a transaction commit (e.g., GRN posting)
func PostInventoryLedger(tenantID string, locationCode string, items []interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		return nil
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Apply search path scoping
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		sku, _ := itemMap["sku"].(string)
		if sku == "" {
			continue
		}

		qtyVal := 0
		if val, exists := itemMap["qty"]; exists {
			switch v := val.(type) {
			case float64:
				qtyVal = int(v)
			case int:
				qtyVal = v
			}
		}

		if qtyVal == 0 {
			continue
		}

		// Perform atomic upsert for stock availability
		query := fmt.Sprintf(`
			INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) 
			VALUES ($1, $2, $3, $3) 
			ON CONFLICT (sku, location_code) DO UPDATE SET 
				on_hand = %s.inventory_availability.on_hand + EXCLUDED.on_hand, 
				available = %s.inventory_availability.available + EXCLUDED.available,
				updated_at = CURRENT_TIMESTAMP`, schema, schema, schema)

		_, err = tx.Exec(query, sku, locationCode, qtyVal)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CreateReservation reserves stock temporarily for cart holds or online orders
func CreateReservation(tenantID string, sku string, locationCode string, qty int, resType string, expirySec int) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}

	// 1. Calculate Available-to-Sell (ATS) stock first
	var onHand, available, committed, reserved, safetyStock int
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT on_hand, available, committed, reserved, safety_stock 
		FROM %s.inventory_availability 
		WHERE sku = $1 AND location_code = $2`, schema), sku, locationCode).Scan(&onHand, &available, &committed, &reserved, &safetyStock)
	if err != nil {
		// If no inventory availability record exists, we cannot reserve stock
		return "", fmt.Errorf("insufficient stock for reservation of SKU: %s", sku)
	}

	ats := available - reserved - safetyStock
	if ats < qty {
		return "", fmt.Errorf("insufficient stock available for reservation (ATS: %d, requested: %d)", ats, qty)
	}

	// 2. Insert reservation record
	expiresAt := time.Now().Add(time.Duration(expirySec) * time.Second)
	var resID string
	err = tx.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.inventory_reservation (sku, location_code, quantity, reservation_type, expires_at) 
		VALUES ($1, $2, $3, $4, $5) 
		RETURNING id::text`, schema), sku, locationCode, qty, resType, expiresAt).Scan(&resID)
	if err != nil {
		return "", err
	}

	// 3. Update reservation count in availability read model
	_, err = tx.Exec(fmt.Sprintf(`
		UPDATE %s.inventory_availability 
		SET reserved = reserved + $1, updated_at = CURRENT_TIMESTAMP 
		WHERE sku = $2 AND location_code = $3`, schema), qty, sku, locationCode)
	if err != nil {
		return "", err
	}

	err = tx.Commit()
	return resID, err
}

// GetAvailableToSell computes ATS (Available-to-Sell) = Available - Reserved - Safety
func GetAvailableToSell(tenantID string, sku string, locationCode string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	var onHand, available, committed, reserved, safetyStock int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT on_hand, available, committed, reserved, safety_stock 
		FROM %s.inventory_availability 
		WHERE sku = $1 AND location_code = $2`, schema), sku, locationCode).Scan(&onHand, &available, &committed, &reserved, &safetyStock)
	if err == sql.ErrNoRows {
		// Fallback to zeros
		return map[string]interface{}{
			"sku":           sku,
			"location_code": locationCode,
			"on_hand":       0,
			"available":     0,
			"committed":     0,
			"reserved":      0,
			"safety_stock":  0,
			"ats":           0,
		}, nil
	} else if err != nil {
		return nil, err
	}

	ats := available - reserved - safetyStock
	return map[string]interface{}{
		"sku":           sku,
		"location_code": locationCode,
		"on_hand":       onHand,
		"available":     available,
		"committed":     committed,
		"reserved":      reserved,
		"safety_stock":  safetyStock,
		"ats":           ats,
	}, nil
}
