package engines

import (
	"crypto/rand"
	"custom_erp/db"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"
)

// GenerateBarcode returns a unique 10-digit barcode string prefixing 'BAR'.
// 24.23: crypto/rand instead of a math/rand source seeded off the wall
// clock, which produces a predictable sequence once an attacker knows
// roughly when a barcode was generated. Barcodes aren't security tokens so
// the practical risk was always low - this is a one-line stdlib swap with
// no change to the output format.
func GenerateBarcode() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	num := binary.BigEndian.Uint32(b[:])%9000000 + 1000000
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
// NegativeStockEvent (20.13) records a floor-check violation that was let
// through instead of rejected - only ever produced when allowNegative is
// true. Currently that's just one caller: FinalizePOSCheckout replaying an
// offline-queued sale, where the goods already physically left the store
// before the server could be asked whether stock covered it. Every other
// PostInventoryLedger caller passes allowNegative=false and keeps the
// original strict reject-on-insufficient-stock behavior unchanged.
type NegativeStockEvent struct {
	SKU                string
	LocationCode       string
	Shortfall          int // how far below zero available ended up (positive number)
	ResultingAvailable int
}

func PostInventoryLedger(tenantID string, locationCode string, items []interface{}, allowNegative bool) ([]NegativeStockEvent, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, nil
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Apply search path scoping
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}

	var negativeEvents []NegativeStockEvent

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

		// Floor-check: a negative delta (checkout/decrement) must not push available
		// stock below zero. Locks the row (FOR UPDATE) so concurrent checkouts against
		// the same SKU/location can't both pass the check before either commits.
		if qtyVal < 0 {
			var currentAvailable int
			err = tx.QueryRow(fmt.Sprintf(`
				SELECT available FROM %s.inventory_availability
				WHERE sku = $1 AND location_code = $2
				FOR UPDATE`, schema), sku, locationCode).Scan(&currentAvailable)
			if err == sql.ErrNoRows {
				if !allowNegative {
					return nil, fmt.Errorf("insufficient stock for SKU %s at %s: no inventory record", sku, locationCode)
				}
				currentAvailable = 0
			} else if err != nil {
				return nil, err
			}
			if currentAvailable+qtyVal < 0 {
				if !allowNegative {
					return nil, fmt.Errorf("insufficient stock for SKU %s at %s: available %d, requested %d", sku, locationCode, currentAvailable, -qtyVal)
				}
				resulting := currentAvailable + qtyVal
				negativeEvents = append(negativeEvents, NegativeStockEvent{
					SKU: sku, LocationCode: locationCode,
					Shortfall: -resulting, ResultingAvailable: resulting,
				})
			}
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
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return negativeEvents, nil
}

// computeATS is the ATP formula's one shared choke point - Available minus
// Reserved/Safety Stock plus the 26.12.6 held-back buckets (Blocked/QC
// Hold/Damaged/Channel Buffer, the blueprint's 7-term ATP - see
// docs/specs/oms_master_blueprint_reference.md §4). CreateReservation's
// admission check, GetAvailableToSell's read, and FindBestFulfillmentNode's
// sourcing comparison (engines/sourcing.go) all call this instead of each
// repeating the formula, so they can't drift out of sync with each other.
func computeATS(available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer int) int {
	return available - reserved - safetyStock - blocked - qcHold - damaged - channelBuffer
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

	// 1. Calculate Available-to-Sell (ATS) stock first. FOR UPDATE (24.7) -
	// without it, two concurrent reservations against the same SKU/location
	// could both read sufficient ATS before either commits and over-reserve;
	// the decrement path above (PostInventoryLedger) already locks this same
	// way, this closes the inconsistency.
	var onHand, available, committed, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer int
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT on_hand, available, committed, reserved, safety_stock, blocked, qc_hold, damaged, channel_buffer
		FROM %s.inventory_availability
		WHERE sku = $1 AND location_code = $2
		FOR UPDATE`, schema), sku, locationCode).Scan(&onHand, &available, &committed, &reserved, &safetyStock, &blocked, &qcHold, &damaged, &channelBuffer)
	if err != nil {
		// If no inventory availability record exists, we cannot reserve stock
		return "", fmt.Errorf("insufficient stock for reservation of SKU: %s", sku)
	}

	ats := computeATS(available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer)
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

// GetAvailableToSell computes ATS (Available-to-Sell) per the 7-term ATP
// formula - see computeATS's own doc comment for the shared formula.
func GetAvailableToSell(tenantID string, sku string, locationCode string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	var onHand, available, committed, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT on_hand, available, committed, reserved, safety_stock, blocked, qc_hold, damaged, channel_buffer
		FROM %s.inventory_availability
		WHERE sku = $1 AND location_code = $2`, schema), sku, locationCode).Scan(&onHand, &available, &committed, &reserved, &safetyStock, &blocked, &qcHold, &damaged, &channelBuffer)
	if err == sql.ErrNoRows {
		// Fallback to zeros
		return map[string]interface{}{
			"sku":            sku,
			"location_code":  locationCode,
			"on_hand":        0,
			"available":      0,
			"committed":      0,
			"reserved":       0,
			"safety_stock":   0,
			"blocked":        0,
			"qc_hold":        0,
			"damaged":        0,
			"channel_buffer": 0,
			"ats":            0,
		}, nil
	} else if err != nil {
		return nil, err
	}

	ats := computeATS(available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer)
	return map[string]interface{}{
		"sku":            sku,
		"location_code":  locationCode,
		"on_hand":        onHand,
		"available":      available,
		"committed":      committed,
		"reserved":       reserved,
		"safety_stock":   safetyStock,
		"blocked":        blocked,
		"qc_hold":        qcHold,
		"damaged":        damaged,
		"channel_buffer": channelBuffer,
		"ats":            ats,
	}, nil
}
