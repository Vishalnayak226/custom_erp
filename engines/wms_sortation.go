package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Stage 42.4.3 - Sortation / put-wall: closes the "batch pick with no
// sortation" hole in 26.5.6's wave pick list. GenerateWavePickList
// consolidates every order in a wave into one bin-by-bin walk; nothing
// before this item ever un-consolidated that pick back to a specific order.
// A SortSlot is that un-consolidation step: one slot per order on a
// SortStation, filled as the picker (or a dedicated sorter) scans, and
// confirmed once the slot holds what that order needs.

// SortSlotInfo is one SortSlot document, flattened.
type SortSlotInfo struct {
	DocID             string `json:"doc_id"`
	Station           string `json:"station"`
	SlotNo            int    `json:"slot_no"`
	WaveID            string `json:"wave_id,omitempty"`
	FulfillmentTaskID string `json:"fulfillment_task_id,omitempty"`
	Sku               string `json:"sku,omitempty"`
	QtyExpected       int    `json:"qty_expected,omitempty"`
	QtyConfirmed      int    `json:"qty_confirmed"`
	Status            string `json:"status"`
}

// ProvisionSortSlots (42.4.3) creates any missing SortSlot rows 1..numSlots
// for a station - idempotent, so re-running after raising a station's
// num_slots only adds the new ones. This is the one place SortSlot
// documents actually get created; nothing about SortStation itself
// auto-provisions its children.
func ProvisionSortSlots(tenantID, station string, numSlots int, userID string) (int, error) {
	if station == "" || numSlots <= 0 {
		return 0, errors.New("station and a positive num_slots are required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var existing []int
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT (data->>'slot_no')::int FROM %s.documents WHERE doctype = 'SortSlot' AND data->>'station' = $1`, schema), station)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var n int
		if rows.Scan(&n) == nil {
			existing = append(existing, n)
		}
	}
	rows.Close()
	have := map[int]bool{}
	for _, n := range existing {
		have[n] = true
	}
	created := 0
	for n := 1; n <= numSlots; n++ {
		if have[n] {
			continue
		}
		slotID := NewDocID("SLOT")
		data := map[string]interface{}{"station": station, "slot_no": n, "status": "Empty", "qty_confirmed": 0}
		payload, _ := json.Marshal(data)
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SortSlot', $2, 'Empty', $3)`, schema),
			slotID, payload, userID); err != nil {
			return created, err
		}
		created++
	}
	if created > 0 {
		LogAuditEvent(tenantID, userID, "WMS_SORT_STATION_PROVISION", "SUCCESS", fmt.Sprintf("Provisioned %d slot(s) on station %s", created, station))
	}
	return created, nil
}

// AssignSortSlot (42.4.3) claims the next Empty slot on a station for one
// order/sku out of a wave, so a batch-picked wave can be routed order by
// order at the wall. Uses FOR UPDATE SKIP LOCKED, the same reservation
// pattern GetNextTask (42.2.3) uses, so two sorters claiming slots on the
// same station concurrently never collide on one slot.
func AssignSortSlot(tenantID, station, waveID, fulfillmentTaskID, sku string, qtyExpected int, userID string) (*SortSlotInfo, error) {
	if station == "" || fulfillmentTaskID == "" {
		return nil, errors.New("station and fulfillment_task_id are required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}

	// An order already holding an unfilled/uncleared slot on this station
	// reuses it rather than claiming a second one - a re-scan of the same
	// order must not fragment its pick across two slots.
	var id, dataStr, status string
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT id, data, status FROM %s.documents
		WHERE doctype = 'SortSlot' AND data->>'station' = $1 AND data->>'fulfillment_task_id' = $2
		  AND status IN ('Assigned', 'Filled')
		LIMIT 1 FOR UPDATE`, schema), station, fulfillmentTaskID).Scan(&id, &dataStr, &status)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == sql.ErrNoRows {
		err = tx.QueryRow(fmt.Sprintf(`
			SELECT id, data, status FROM %s.documents
			WHERE doctype = 'SortSlot' AND data->>'station' = $1 AND status = 'Empty'
			ORDER BY (data->>'slot_no')::int LIMIT 1 FOR UPDATE SKIP LOCKED`, schema), station).Scan(&id, &dataStr, &status)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no Empty slot available on station %s", station)
		} else if err != nil {
			return nil, err
		}
	}

	patch := map[string]interface{}{
		"status": "Assigned", "wave_id": waveID, "fulfillment_task_id": fulfillmentTaskID,
		"sku": sku, "qty_expected": qtyExpected,
	}
	patchJSON, _ := json.Marshal(patch)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, status = 'Assigned', updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
		patchJSON, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "WMS_SORT_SLOT_ASSIGN", "SUCCESS", fmt.Sprintf("Slot %s on station %s assigned to task %s", id, station, fulfillmentTaskID))
	return getSortSlot(schema, id)
}

// ConfirmSortSlot (42.4.3) records a scan into an Assigned/Filled slot,
// accumulating qty_confirmed, and marks the slot Filled once it meets or
// exceeds qty_expected (0 expected means any positive scan fills it - a
// slot with no known target qty, e.g. a single-unit order).
func ConfirmSortSlot(tenantID, slotID string, qty int, userID string) (*SortSlotInfo, error) {
	if qty <= 0 {
		return nil, errors.New("qty must be positive")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	slot, err := getSortSlot(schema, slotID)
	if err != nil {
		return nil, err
	}
	if slot == nil {
		return nil, fmt.Errorf("sort slot %s not found", slotID)
	}
	if slot.Status != "Assigned" && slot.Status != "Filled" {
		return nil, fmt.Errorf("slot %s is %s, not Assigned/Filled", slotID, slot.Status)
	}
	newConfirmed := slot.QtyConfirmed + qty
	newStatus := "Assigned"
	if slot.QtyExpected <= 0 || newConfirmed >= slot.QtyExpected {
		newStatus = "Filled"
	}
	patch := map[string]interface{}{"qty_confirmed": newConfirmed, "status": newStatus}
	patchJSON, _ := json.Marshal(patch)
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SortSlot' AND id = $3`, schema),
		patchJSON, newStatus, slotID); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "WMS_SORT_SLOT_CONFIRM", "SUCCESS", fmt.Sprintf("Slot %s confirmed +%d (now %d/%d, %s)", slotID, qty, newConfirmed, slot.QtyExpected, newStatus))
	return getSortSlot(schema, slotID)
}

// ClearSortSlot (42.4.3) frees a Filled slot back to Empty once its order
// has been handed off to packing - the step that makes the slot reusable
// for the next order routed to this station.
func ClearSortSlot(tenantID, slotID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	slot, err := getSortSlot(schema, slotID)
	if err != nil {
		return err
	}
	if slot == nil {
		return fmt.Errorf("sort slot %s not found", slotID)
	}
	if slot.Status != "Filled" {
		return fmt.Errorf("slot %s is %s, not Filled - cannot clear", slotID, slot.Status)
	}
	patch := map[string]interface{}{
		"status": "Empty", "wave_id": "", "fulfillment_task_id": "", "sku": "", "qty_expected": 0, "qty_confirmed": 0,
	}
	patchJSON, _ := json.Marshal(patch)
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, status = 'Empty', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SortSlot' AND id = $2`, schema),
		patchJSON, slotID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_SORT_SLOT_CLEAR", "SUCCESS", fmt.Sprintf("Slot %s cleared", slotID))
	return nil
}

func getSortSlot(schema, slotID string) (*SortSlotInfo, error) {
	var dataStr, status string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, COALESCE(status, '') FROM %s.documents WHERE doctype = 'SortSlot' AND id = $1`, schema), slotID).Scan(&dataStr, &status)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &data)
	info := &SortSlotInfo{DocID: slotID, Status: status}
	info.Station, _ = data["station"].(string)
	info.SlotNo = int(numFromInterface(data["slot_no"]))
	info.WaveID, _ = data["wave_id"].(string)
	info.FulfillmentTaskID, _ = data["fulfillment_task_id"].(string)
	info.Sku, _ = data["sku"].(string)
	info.QtyExpected = int(numFromInterface(data["qty_expected"]))
	info.QtyConfirmed = int(numFromInterface(data["qty_confirmed"]))
	return info, nil
}

// ListSortSlots (42.4.3) returns every slot on a station, in slot order -
// the put-wall screen's own live board.
func ListSortSlots(tenantID, station string) ([]SortSlotInfo, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, COALESCE(status, '') FROM %s.documents
		WHERE doctype = 'SortSlot' AND data->>'station' = $1
		ORDER BY (data->>'slot_no')::int`, schema), station)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SortSlotInfo{}
	for rows.Next() {
		var id, dataStr, status string
		if err := rows.Scan(&id, &dataStr, &status); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		info := SortSlotInfo{DocID: id, Status: status}
		info.Station, _ = data["station"].(string)
		info.SlotNo = int(numFromInterface(data["slot_no"]))
		info.WaveID, _ = data["wave_id"].(string)
		info.FulfillmentTaskID, _ = data["fulfillment_task_id"].(string)
		info.Sku, _ = data["sku"].(string)
		info.QtyExpected = int(numFromInterface(data["qty_expected"]))
		info.QtyConfirmed = int(numFromInterface(data["qty_confirmed"]))
		out = append(out, info)
	}
	return out, rows.Err()
}
