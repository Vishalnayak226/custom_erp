package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 146.3.5 (D7): transfers picked from bins, in waves. With wms.transfer_waves
// on, an Approved or Packed transfer can be added to a Wave at its source
// location (the same wave/assign call that tags order tasks). Releasing the
// wave then reserves bins for the transfer exactly as for an order - oldest
// first, under the same per-SKU lock - and the picks reach the work queue
// through the wave pick-task emitter with the transfer as their source.
// Dispatch consumes the transfer's own reservation. Off = a transfer never
// joins a wave and dispatches as before.
//
// A transfer is told from an order task by its id prefix: transfers are
// numbered TO-..., order tasks are not.

// TransferWavesSetting lets transfers join waves.
const TransferWavesSetting = "wms.transfer_waves"

func init() {
	RegisterSetting(SettingDefinition{
		Key: TransferWavesSetting, Module: "Warehouse",
		Label: "Transfers can join waves", Type: SettingTypeBool, Default: "false",
		Description: "An approved transfer can be added to a wave at its source location; the wave reserves its bins and picks it with the orders, and dispatch takes what was picked. Off = transfers dispatch on their own.",
	})
}

func isWaveTransferID(id string) bool { return strings.HasPrefix(id, "TO-") }

// waveSourceDocType is the work-queue source type of a wave reservation.
func waveSourceDocType(id string) string {
	if isWaveTransferID(id) {
		return "TransferOrder"
	}
	return "FulfillmentTask"
}

func transferWaveError(format string, args ...interface{}) error {
	return &ValidationError{Code: "STOCKT-0491", Message: fmt.Sprintf(format, args...)}
}

// AssignTransfersToWave tags Approved / Packed transfers from the wave's
// location into it. All or nothing: one ineligible transfer refuses the call
// and names itself.
func AssignTransfersToWave(tenantID, waveID string, transferIDs []string, userID string) (int, error) {
	if !GetSettingBool(tenantID, TransferWavesSetting) {
		return 0, transferWaveError("transfers cannot join waves - turn on \"Transfers can join waves\" in Configuration")
	}
	if len(transferIDs) == 0 {
		return 0, transferWaveError("name at least one transfer")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	wave, err := getWave(schema, waveID)
	if err != nil {
		return 0, err
	}
	if wave == nil {
		return 0, transferWaveError("wave %s not found", waveID)
	}
	if wave.Status == "Complete" || wave.Status == "Closed" {
		return 0, transferWaveError("wave %s is %s", waveID, wave.Status)
	}
	for _, id := range transferIDs {
		var status, from string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(data->>'from_warehouse', '') FROM %s.documents
			WHERE doctype = 'TransferOrder' AND id = $1 AND deleted_at IS NULL`, schema), id).Scan(&status, &from); err != nil {
			if err == sql.ErrNoRows {
				return 0, transferWaveError("transfer %s not found", id)
			}
			return 0, err
		}
		if status != "Approved" && status != "Packed" {
			return 0, transferWaveError("transfer %s is %s - only an Approved or Packed transfer can join a wave", id, status)
		}
		if from != wave.LocationCode {
			return 0, transferWaveError("transfer %s leaves %s, but wave %s is at %s", id, from, waveID, wave.LocationCode)
		}
	}
	for _, id := range transferIDs {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('wave_id', $1::text), updated_at = CURRENT_TIMESTAMP
			WHERE doctype = 'TransferOrder' AND id = $2`, schema), waveID, id); err != nil {
			return 0, err
		}
	}
	LogAuditEvent(tenantID, userID, "WMS_WAVE_ASSIGN_TRANSFER", "SUCCESS", fmt.Sprintf("Added %d transfer(s) to wave %s", len(transferIDs), waveID))
	return len(transferIDs), nil
}

// waveTransferRemainders adds a wave's transfers to the order tasks
// buildWavePickList has collected. It returns each transfer's open lines and
// their location; with the setting off it returns nothing.
func waveTransferRemainders(tenantID, schema, waveID string) (rem []waveTaskItemRemainder, location string, count int, err error) {
	if !GetSettingBool(tenantID, TransferWavesSetting) {
		return nil, "", 0, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data->>'from_warehouse', data->>'items', created_at FROM %s.documents
		WHERE doctype = 'TransferOrder' AND data->>'wave_id' = $1 AND status IN ('Approved', 'Packed') AND deleted_at IS NULL
		ORDER BY created_at, id`, schema), waveID)
	if err != nil {
		return nil, "", 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var from, items sql.NullString
		var created time.Time
		if err := rows.Scan(&id, &from, &items, &created); err != nil {
			return nil, "", 0, err
		}
		lines, perr := parseTransferItems(items.String)
		if perr != nil {
			continue
		}
		if location == "" {
			location = from.String
		} else if from.String != "" && from.String != location {
			return nil, "", 0, fmt.Errorf("wave %s spans more than one location (%s and %s) - a wave must be single-location", waveID, location, from.String)
		}
		count++
		for _, l := range lines {
			if l.Qty > 0 {
				rem = append(rem, waveTaskItemRemainder{TaskID: id, CreatedAt: created, Sku: l.Sku, Remaining: l.Qty})
			}
		}
	}
	return rem, location, count, rows.Err()
}

// reserveWaveStock reserves bins for one wave entry: an order task as ever, a
// transfer by its own lines.
func reserveWaveStock(tenantID, id string) error {
	if !isWaveTransferID(id) {
		return ReserveTaskBinStock(tenantID, id)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	var status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT status, data FROM %s.documents WHERE doctype = 'TransferOrder' AND id = $1 FOR UPDATE`, schema), id).Scan(&status, &raw); err != nil {
		return err
	}
	if status != "Approved" && status != "Packed" {
		return nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	from, _ := data["from_warehouse"].(string)
	itemsStr, _ := data["items"].(string)
	lines, err := parseTransferItems(itemsStr)
	if err != nil {
		return err
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Sku < lines[j].Sku })
	for _, l := range lines {
		if _, err := reserveTaskSKUTx(tx, tenantID, schema, id, from, "", taskSKUNeed{SKU: l.Sku, Demand: l.Qty}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// hasOpenBinRowsTx: does this transfer hold bin reservations for the SKU?
func hasOpenBinRowsTx(tx *sql.Tx, schema, transferID, sku string) bool {
	var n int
	_ = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.bin_allocation WHERE task_id = $1 AND sku = $2 AND status = 'Open'`, schema), transferID, sku).Scan(&n)
	return n > 0
}

// dispatchFromWaveBinsTx is dispatch's plain-line path when the transfer's
// wave already reserved its bins: it takes exactly what was reserved (topping
// up if the reservation fell short) and hands back any surplus. It reports
// false when the transfer holds nothing for the SKU, so the caller falls back
// to the ordinary draw.
func dispatchFromWaveBinsTx(tx *sql.Tx, tenantID, schema, transferID, from, sku string, qty int) (bool, error) {
	if !hasOpenBinRowsTx(tx, schema, transferID, sku) {
		return false, nil
	}
	if _, err := ConsumeTaskBinStockTx(tx, tenantID, schema, transferID, from, sku, qty); err != nil {
		return true, err
	}
	return true, ReleaseTaskBinStockTx(tx, schema, transferID, sku)
}
