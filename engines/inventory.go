package engines

import (
	"crypto/rand"
	"custom_erp/db"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
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

// StockLedgerEntry is one append-only inventory-movement record (26.10.1).
// ItemID/WarehouseID/Qty/VoucherType/VoucherID are the original Phase 3
// fields; everything else is additive so a caller that only ever set those
// five still gets identical behavior. FromLocationID/ToLocationID name the
// finer bin/warehouse the stock moved between (distinct from WarehouseID,
// which stays the store/warehouse-level location_code inventory_availability
// itself is keyed on - see engines/wms.go's bin_stock, a separate finer
// breakdown of the same on-hand total); FromStatus/ToStatus name a
// Good/Damaged/QC-Hold/RTV condition change. A pure location or condition
// move that doesn't change on_hand/available (e.g. putaway, a bin-to-bin
// shelf move) is recorded with Qty 0 - GetStockBalance's SUM stays correct
// since a zero contributes nothing, while the movement itself is still on
// the card.
type StockLedgerEntry struct {
	ItemID         string
	WarehouseID    string
	Qty            float64
	VoucherType    string
	VoucherID      string
	IdempotencyKey string // non-empty: a second write with the same key is a no-op, so a retried call (e.g. a replayed offline sale) can't double-count
	FromLocationID string
	ToLocationID   string
	FromStatus     string
	ToStatus       string
	UserID         string
	DeviceID       string
}


// WriteStockLedgerEntry writes an append-only inventory card record.
func WriteStockLedgerEntry(tenantID string, e StockLedgerEntry) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	if e.IdempotencyKey != "" {
		var exists bool
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'idempotency_key' = $1)`, schema),
			e.IdempotencyKey).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
	}

	id := NewDocIDCompact("SLE")
	docData := map[string]interface{}{
		"id":           id,
		"code":         id,
		"item_id":      e.ItemID,
		"warehouse_id": e.WarehouseID,
		"qty":          e.Qty,
		"voucher_type": e.VoucherType,
		"voucher_id":   e.VoucherID,
		"status":       "Active",
	}
	if e.IdempotencyKey != "" {
		docData["idempotency_key"] = e.IdempotencyKey
	}
	if e.FromLocationID != "" {
		docData["from_location_id"] = e.FromLocationID
	}
	if e.ToLocationID != "" {
		docData["to_location_id"] = e.ToLocationID
	}
	if e.FromStatus != "" {
		docData["from_status"] = e.FromStatus
	}
	if e.ToStatus != "" {
		docData["to_status"] = e.ToStatus
	}
	if e.UserID != "" {
		docData["user_id"] = e.UserID
	}
	if e.DeviceID != "" {
		docData["device_id"] = e.DeviceID
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

// PostInventoryLedger keeps its original signature for existing callers
// (engines/manufacturing*.go, engines/scale.go) that have no real voucher to
// tag a movement with - it delegates to PostInventoryLedgerWithVoucher with a
// generic "StockAdjustment" tag rather than every caller needing to change.
// A caller that does have real voucher/actor context should call
// PostInventoryLedgerWithVoucher directly instead (26.10.1 - see
// wms_receiving.go/pos_checkout.go).
func PostInventoryLedger(tenantID string, locationCode string, items []interface{}, allowNegative bool) ([]NegativeStockEvent, error) {
	return PostInventoryLedgerWithVoucher(tenantID, locationCode, items, allowNegative, "StockAdjustment", "", "")
}

// PostInventoryLedgerWithVoucher is PostInventoryLedger plus a StockLedgerEntry
// per line (26.10.1), tagged with a real voucher_type/voucher_id/user_id
// instead of PostInventoryLedger's generic fallback. Ledger writes happen
// after the availability update has already committed and are best-effort -
// a write failure is logged, never allowed to undo or fail a movement that
// already posted, the same "already physically happened" reasoning
// pos_checkout.go's recordOfflineSyncVariance uses for its own post-commit
// bookkeeping.
func PostInventoryLedgerWithVoucher(tenantID string, locationCode string, items []interface{}, allowNegative bool, voucherType, voucherID, userID string) ([]NegativeStockEvent, error) {
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
	type postedLine struct {
		sku string
		qty int
	}
	var postedLines []postedLine

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
		postedLines = append(postedLines, postedLine{sku: sku, qty: qtyVal})
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, p := range postedLines {
		entry := StockLedgerEntry{
			ItemID: p.sku, WarehouseID: locationCode, Qty: float64(p.qty),
			VoucherType: voucherType, VoucherID: voucherID, UserID: userID,
		}
		if voucherID != "" {
			entry.IdempotencyKey = fmt.Sprintf("%s:%s:%s:%s", voucherType, voucherID, locationCode, p.sku)
		}
		if lerr := WriteStockLedgerEntry(tenantID, entry); lerr != nil {
			LogSystemError(tenantID, "", "WARN", "PostInventoryLedgerWithVoucher", fmt.Sprintf("stock ledger write failed for %s at %s: %v", p.sku, locationCode, lerr), "")
		}
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

// ReservationAttribution (Stage 35.3.7) names the order line a reservation was
// made for. Variadic on CreateReservation so the existing call sites that have
// no order - a cart hold, the load-test harness, the manual reservation API -
// need no change and keep writing an unattributed row, which is the honest
// record for them.
type ReservationAttribution struct {
	OrderID string
	LineID  string
}

// CreateReservation reserves stock temporarily for cart holds or online orders
func CreateReservation(tenantID string, sku string, locationCode string, qty int, resType string, expirySec int, attribution ...ReservationAttribution) (string, error) {
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

	// 2. Insert reservation record. expirySec <= 0 means "use the tenant's
	// configured online-reservation hold" (Stage 28) - CreateReservation is the
	// single choke point every online/channel reservation routes its default
	// TTL through, so editing inventory.reservation_ttl_seconds reaches all of
	// them at once. Callers that pass an explicit value (short cart holds,
	// tests) are unaffected.
	if expirySec <= 0 {
		expirySec = GetSettingInt(tenantID, "inventory.reservation_ttl_seconds")
	}
	expiresAt := time.Now().Add(time.Duration(expirySec) * time.Second)
	var orderArg, lineArg interface{}
	if len(attribution) > 0 {
		if strings.TrimSpace(attribution[0].OrderID) != "" {
			orderArg = strings.TrimSpace(attribution[0].OrderID)
		}
		if strings.TrimSpace(attribution[0].LineID) != "" {
			lineArg = strings.TrimSpace(attribution[0].LineID)
		}
	}
	var resID string
	err = tx.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.inventory_reservation (sku, location_code, quantity, reservation_type, expires_at, order_id, line_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text`, schema), sku, locationCode, qty, resType, expiresAt, orderArg, lineArg).Scan(&resID)
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
