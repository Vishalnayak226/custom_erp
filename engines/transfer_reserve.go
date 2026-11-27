package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
)

// Stage 146.1.7 (D20): an approved transfer holds its source stock, so online
// sales and other orders cannot take what the transfer is about to move. Off
// by default; with it off nothing here runs and transfers behave as before.
//
// The hold is a row in inventory_reservation (type "Transfer", line_id = the
// transfer id, no order_id) plus the matching bump of inventory_availability
// .reserved, made through CreateReservation - the one choke point every
// reservation uses - with an expiry so far off the sweeper never drops it.
// Dispatch consumes it, cancel releases it, and a lowered quantity is
// reconciled by SyncTransferReservation.

const (
	// TransferReserveSetting switches the hold on.
	TransferReserveSetting = "inventory.transfer_reserve_on_approval"
	// TransferHoldStaleDaysSetting is the age after which a still-unmoved
	// transfer hold shows on WMS home.
	TransferHoldStaleDaysSetting = "inventory.transfer_hold_stale_days"

	transferReservationType = "Transfer"
	// ten years: the sweeper drops unattributed rows only once expired.
	transferReservationTTLSeconds = 10 * 365 * 24 * 3600
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: TransferReserveSetting, Module: "Warehouse",
		Label: "Approved transfers hold their stock", Type: SettingTypeBool, Default: "false",
		Description: "An approved transfer reserves its stock at the sending location until it is dispatched or cancelled, so sales cannot take it. Off = stock stays sellable until dispatch.",
	})
	RegisterSetting(SettingDefinition{
		Key: TransferHoldStaleDaysSetting, Module: "Warehouse",
		Label: "Days before a transfer hold shows as stale", Type: SettingTypeInt, Default: "3",
		Description: "A held transfer not dispatched after this many days shows on WMS home. 0 = never.",
	})
}

// releaseTransferReservationTx drops every reservation the transfer holds and
// gives the quantity back to the availability read model.
func releaseTransferReservationTx(tx *sql.Tx, schema, transferID string) error {
	rows, err := tx.Query(fmt.Sprintf(`
		DELETE FROM %s.inventory_reservation
		 WHERE reservation_type = $1 AND line_id = $2 AND order_id IS NULL
		RETURNING sku, location_code, quantity`, schema), transferReservationType, transferID)
	if err != nil {
		return err
	}
	type held struct {
		sku, loc string
		qty      int
	}
	var gone []held
	for rows.Next() {
		var h held
		if err := rows.Scan(&h.sku, &h.loc, &h.qty); err != nil {
			rows.Close()
			return err
		}
		gone = append(gone, h)
	}
	rows.Close()
	for _, h := range gone {
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.inventory_availability SET reserved = GREATEST(reserved - $1, 0), updated_at = CURRENT_TIMESTAMP
			 WHERE sku = $2 AND location_code = $3`, schema), h.qty, h.sku, h.loc); err != nil {
			return err
		}
	}
	return nil
}

// SyncTransferReservation makes the transfer's hold match the transfer as it
// stands: an Approved or Packed transfer holds its current line quantities
// (capped at what can be sold), anything else - cancelled, dispatched,
// closed - holds nothing. Call it after approval, cancel or an edit. A no-op
// with the setting off. Best effort: a refusal is logged, never fails the
// approval it follows.
func SyncTransferReservation(tenantID, transferID string) {
	if !GetSettingBool(tenantID, TransferReserveSetting) {
		return
	}
	if err := syncTransferReservation(tenantID, transferID); err != nil {
		LogSystemError(tenantID, "", "WARN", "SyncTransferReservation", fmt.Sprintf("transfer %s: %v", transferID, err), "")
	}
}

func syncTransferReservation(tenantID, transferID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockTransferOrderTx(tx, schema, transferID)
	if err != nil {
		return err
	}
	if err := releaseTransferReservationTx(tx, schema, transferID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if status != "Approved" && status != "Packed" {
		return nil
	}
	from, _ := data["from_warehouse"].(string)
	itemsStr, _ := data["items"].(string)
	lines, err := parseTransferItems(itemsStr)
	if err != nil || from == "" {
		return err
	}
	for _, l := range lines {
		if l.Qty <= 0 {
			continue
		}
		m, err := GetAvailableToSell(tenantID, l.Sku, from)
		if err != nil {
			return err
		}
		qty := l.Qty
		if ats, _ := m["ats"].(int); ats < qty {
			qty = ats
		}
		if qty <= 0 {
			continue
		}
		if _, err := CreateReservation(tenantID, l.Sku, from, qty, transferReservationType, transferReservationTTLSeconds,
			ReservationAttribution{LineID: transferID}); err != nil {
			return err
		}
	}
	return nil
}

// StaleTransferHold is a transfer hold that has sat unmoved past the stale
// age - stock kept out of sale for a transfer nobody dispatched.
type StaleTransferHold struct {
	TransferID string `json:"transfer_id"`
	Location   string `json:"location_code"`
	Qty        int    `json:"qty"`
	Days       int    `json:"days"`
}

// cockpitStaleTransferHolds lists transfer holds older than the stale age
// whose transfer is still Approved or Packed.
func cockpitStaleTransferHolds(tenantID, schema, locationCode string) []StaleTransferHold {
	out := []StaleTransferHold{}
	days := GetSettingInt(tenantID, TransferHoldStaleDaysSetting)
	if days <= 0 || !GetSettingBool(tenantID, TransferReserveSetting) {
		return out
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT r.line_id, r.location_code, SUM(r.quantity)::int,
		       GREATEST(EXTRACT(DAY FROM (CURRENT_TIMESTAMP - MIN(r.created_at))), 0)::int
		  FROM %s.inventory_reservation r
		  JOIN %s.documents d ON d.doctype = 'TransferOrder' AND d.id = r.line_id AND d.status IN ('Approved', 'Packed')
		 WHERE r.reservation_type = $1 AND r.order_id IS NULL
		   AND ($2 = '' OR r.location_code = $2)
		 GROUP BY r.line_id, r.location_code
		HAVING MIN(r.created_at) < CURRENT_TIMESTAMP - make_interval(days => $3)
		 ORDER BY MIN(r.created_at)
		 LIMIT 50`, schema, schema), transferReservationType, locationCode, days)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var h StaleTransferHold
		if rows.Scan(&h.TransferID, &h.Location, &h.Qty, &h.Days) == nil {
			out = append(out, h)
		}
	}
	return out
}
