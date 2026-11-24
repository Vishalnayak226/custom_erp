package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Stage 146.1.5: a short pick keeps the missing pieces out of sale until a
// count says where they are. With the setting off, a short pick only releases
// its reservation, as before.

const (
	// ShortPickHoldSetting switches the "not found" hold on.
	ShortPickHoldSetting = "wms.short_pick_hold"
	// NotFoundHoldCode is the HoldCode these holds carry.
	NotFoundHoldCode = "NOT-FOUND"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: ShortPickHoldSetting, Module: "Warehouse",
		Label: "Hold short-picked pieces until counted", Type: SettingTypeBool, Default: "false",
		Description: "A short pick holds the missing pieces out of sale and asks for a count of that bin; the count result releases the hold. Off = a short pick only releases its reservation.",
	})
}

// holdShortPickTx puts qty of sku at location under a NOT-FOUND hold, inside
// the short pick's transaction. It returns "" (and holds nothing) when there
// is no stock record to hold against or the hold code is switched off - a
// short pick must never fail because of its hold.
func holdShortPickTx(tx *sql.Tx, schema, taskID, sku, location string, qty int, userID string) (string, error) {
	if qty <= 0 || location == "" {
		return "", nil
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ('HOLDCODE-NOT-FOUND', 'HoldCode', $1, 'Active', 'system') ON CONFLICT (id) DO NOTHING`, schema),
		fmt.Sprintf(`{"code":"%s","description":"Short pick: not found in its bin","category":"Investigation","status":"Active"}`, NotFoundHoldCode)); err != nil {
		return "", err
	}
	var onHand, held int
	err := tx.QueryRow(fmt.Sprintf(
		`SELECT on_hand, hold_qty FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
		sku, location).Scan(&onHand, &held)
	if err == sql.ErrNoRows {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if free := onHand - held; qty > free {
		qty = free
	}
	if qty <= 0 {
		return "", nil
	}
	if _, err := tx.Exec("SAVEPOINT short_pick_hold"); err != nil {
		return "", err
	}
	id, err := placeHoldTx(tx, schema, NotFoundHoldCode, sku, location, "", qty, "Short pick on "+taskID, userID)
	if err != nil {
		_, _ = tx.Exec("ROLLBACK TO SAVEPOINT short_pick_hold")
		return "", nil
	}
	return id, nil
}

// raiseShortPickCount opens the count task for the bin the pieces went
// missing from. Best effort: the hold already protects the stock.
func raiseShortPickCount(tenantID, holdID, taskID, sku, location, bin string, qty int, userID string) {
	if _, err := CreateWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "Count", Priority: 1, LocationCode: location, FromBin: bin, Item: sku, Qty: float64(qty),
		SourceDocType: "Hold", SourceDocID: holdID,
		Notes: fmt.Sprintf("Short pick on %s: %d not found - count this bin", taskID, qty),
	}, userID); err != nil {
		LogSystemError(tenantID, "", "WARN", "ShortPickLine", fmt.Sprintf("count task for short pick %s: %v", taskID, err), "")
	}
}

// releaseShortPickHolds is the count result: once a count of sku at location
// has posted (no difference, or the approved difference), its NOT-FOUND holds
// are released and their count tasks closed. Best effort and logged.
func releaseShortPickHolds(tenantID, sku, location string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id FROM %s.documents WHERE doctype = 'Hold' AND status = 'Active'
		   AND data->>'hold_code' = $1 AND data->>'sku' = $2 AND data->>'location_code' = $3`, schema),
		NotFoundHoldCode, sku, location)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if err := ReleaseHold(tenantID, id); err != nil {
			LogSystemError(tenantID, "", "WARN", "releaseShortPickHolds", fmt.Sprintf("hold %s: %v", id, err), "")
			continue
		}
		patch, _ := json.Marshal(map[string]string{"status": WTStatusCompleted})
		if _, err := db.DB.Exec(fmt.Sprintf(`
			UPDATE %s.documents SET data = data || $1::jsonb, status = 'Completed', updated_at = CURRENT_TIMESTAMP
			 WHERE doctype = 'WarehouseTask' AND status IN ('Pending', 'Assigned', 'In Progress')
			   AND data->>'task_type' = 'Count' AND data->>'source_doc_type' = 'Hold' AND data->>'source_doc_id' = $2`, schema), patch, id); err != nil {
			// 252.1: the hold is released; its count task is left open, so say so.
			LogSystemError(tenantID, "", "WARN", "releaseShortPickHolds", fmt.Sprintf("hold %s released, but its count task was not closed: %v", id, err), "")
		}
	}
}
