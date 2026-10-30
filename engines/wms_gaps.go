package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 130.13 (80.7, user decision 2026-10-22: close every WMS gap the 80.5
// sweep found). (a) holds at pick allocation are in traceability.go
// (allocateFromStockQ); (c) shipping packages are made and invoiced at the
// OMS Pack step since Stage 88; (e) a posted goods receipt opens Putaway on
// its own items (grn-steps.js). The rest is here.

// --- (b) mobile pick confirmations kept on the server ---------------------

// WavePickLineKey names one line of a wave's pick list.
func WavePickLineKey(sku, bin, batch string) string {
	return strings.TrimSpace(sku) + "|" + strings.TrimSpace(bin) + "|" + strings.TrimSpace(batch)
}

// ConfirmWavePickLine records that user picked a wave line. Repeating it
// keeps the first confirmation.
func ConfirmWavePickLine(tenantID, waveID, lineKey string, qty int, user string) error {
	waveID, lineKey = strings.TrimSpace(waveID), strings.TrimSpace(lineKey)
	if waveID == "" || lineKey == "" {
		return &ValidationError{Code: "WMS-1311", Message: "say which wave and which line was picked"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.wave_pick_confirmations (wave_id, line_key, qty, confirmed_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (wave_id, line_key) DO NOTHING`, schema), waveID, lineKey, qty, user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	sku := strings.SplitN(lineKey, "|", 2)[0]
	return applyWavePickToTasks(tenantID, schema, waveID, sku, qty)
}

// applyWavePickToTasks (140.12.15) counts a confirmed wave line as picked on
// the wave's own orders: the quantity is spread over the open tasks needing
// that item, oldest first, each through the bin consume a scan pick uses, so
// Pack and the bill see the units as picked. A wave confirm used to record
// only its own row, so a waved order could never be packed.
func applyWavePickToTasks(tenantID, schema, waveID, sku string, qty int) error {
	if sku == "" || qty <= 0 {
		return nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'FulfillmentTask' AND deleted_at IS NULL
		AND data->>'wave_id' = $1 AND status IN ('Pending', 'Picking') ORDER BY created_at, id`, schema), waveID)
	if err != nil {
		return err
	}
	var tasks []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			tasks = append(tasks, id)
		}
	}
	rows.Close()
	for _, taskID := range tasks {
		if qty <= 0 {
			break
		}
		took, err := pickTaskSKUQty(tenantID, schema, taskID, sku, qty)
		if err != nil {
			return err
		}
		qty -= took
	}
	return nil
}

// pickTaskSKUQty picks up to qty units of sku on one task, under its lock.
func pickTaskSKUQty(tenantID, schema, taskID, sku string, qty int) (int, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	lockedStatus, lockedData, err := lockFulfillmentTaskTx(tx, schema, taskID)
	if err != nil {
		return 0, err
	}
	task, err := loadPickPackTask(schema, taskID)
	if err != nil || scanGuard(task) != nil {
		return 0, err
	}
	took := 0
	for i := range task.items {
		it := &task.items[i]
		if it.SKU != sku {
			continue
		}
		n := it.Qty - it.ShortQty - it.PickedQty
		if n > qty-took {
			n = qty - took
		}
		if n > 0 {
			it.PickedQty += n
			took += n
		}
	}
	if took == 0 {
		return 0, nil
	}
	if err := ReserveTaskBinStockTx(tx, tenantID, schema, taskID, lockedStatus, lockedData); err != nil {
		return 0, err
	}
	location, _ := task.data["location_code"].(string)
	if _, err := ConsumeTaskBinStockTx(tx, tenantID, schema, taskID, location, sku, took); err != nil {
		return 0, err
	}
	recomputeTaskStatus(task)
	if err := task.saveWith(tx, schema); err != nil {
		return 0, err
	}
	return took, tx.Commit()
}

// WavePickConfirmations are a wave's confirmed lines, by line key.
func WavePickConfirmations(tenantID, waveID string) (map[string]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if to := ""; db.DB.QueryRow(`SELECT COALESCE(to_regclass($1)::text, '')`, schema+".wave_pick_confirmations").Scan(&to) != nil || to == "" {
		return out, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT line_key, confirmed_by FROM %s.wave_pick_confirmations WHERE wave_id = $1`, schema), waveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, by string
		if rows.Scan(&k, &by) == nil {
			out[k] = by
		}
	}
	return out, rows.Err()
}

// --- (d) sortation from the screen, by wave -------------------------------

// AssignWaveToSortSlots gives every open order of a wave its own slot at
// station - the put-wall a wave is sorted into.
func AssignWaveToSortSlots(tenantID, station, waveID, user string) ([]SortSlotInfo, error) {
	station, waveID = strings.TrimSpace(station), strings.TrimSpace(waveID)
	if station == "" || waveID == "" {
		return nil, &ValidationError{Code: "WMS-1311", Message: "choose the sort station and the wave"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'FulfillmentTask' AND deleted_at IS NULL
		AND data->>'wave_id' = $1 AND status IN ('Pending', 'Picking', 'Picked') ORDER BY created_at`, schema), waveID)
	if err != nil {
		return nil, err
	}
	var tasks []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			tasks = append(tasks, id)
		}
	}
	rows.Close()
	if len(tasks) == 0 {
		return nil, &ValidationError{Code: "WMS-1311", Message: fmt.Sprintf("wave %s has no open orders to sort", waveID)}
	}
	var out []SortSlotInfo
	for _, task := range tasks {
		slot, err := AssignSortSlot(tenantID, station, waveID, task, "", 0, user)
		if err != nil {
			return out, err
		}
		if slot != nil {
			out = append(out, *slot)
		}
	}
	return out, nil
}

// --- (f) an approved cycle count moves the bin, not only the location ------

// applyCycleCountToBinTx sets a counted bin's quantity to what was counted,
// in the approval's transaction. A line without a bin changes nothing here.
func applyCycleCountToBinTx(tx *sql.Tx, schema, binCode, sku, locationCode string, counted int) error {
	binCode, sku = strings.TrimSpace(binCode), strings.TrimSpace(sku)
	if binCode == "" || sku == "" {
		return nil
	}
	if counted < 0 {
		counted = 0
	}
	res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.bin_stock SET qty = $3, updated_at = CURRENT_TIMESTAMP
		WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'`, schema), binCode, sku, counted)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 && counted > 0 {
		_, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s.bin_stock (bin_code, sku, location_code, qty, condition) VALUES ($1, $2, $3, $4, 'Good')`, schema),
			binCode, sku, locationCode, counted)
	}
	return err
}

// --- (g) cross-dock plans met on receipt -----------------------------------

// CrossDockPlanMatch is a Planned cross-dock plan a receipt's item meets.
type CrossDockPlanMatch struct {
	PlanID      string `json:"plan_id"`
	SKU         string `json:"sku"`
	Qty         int    `json:"qty"`
	Destination string `json:"destination"`
}

// MatchCrossDockPlansOnReceipt finds the Planned cross-dock plans a receipt
// meets - the plan's PO or ASN is the receipt's (or the plan names neither)
// and its item came in - and marks each with the receipt (arrived_with), so
// the dock sees the goods are in and where they go next. A plan is matched
// once.
func MatchCrossDockPlansOnReceipt(tenantID, receiptID, poID, asnID string, skus []string) ([]CrossDockPlanMatch, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, s := range skus {
		if s = strings.TrimSpace(s); s != "" {
			want[s] = true
		}
	}
	if len(want) == 0 {
		return nil, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'CrossDockPlan' AND deleted_at IS NULL
		AND status = 'Planned' AND COALESCE(data->>'arrived_with', '') = ''
		AND ((COALESCE(data->>'po_id', '') = '' AND COALESCE(data->>'asn_id', '') = '')
		  OR ($1 <> '' AND data->>'po_id' = $1) OR ($2 <> '' AND data->>'asn_id' = $2))`, schema), strings.TrimSpace(poID), strings.TrimSpace(asnID))
	if err != nil {
		return nil, err
	}
	type plan struct {
		id   string
		data map[string]interface{}
	}
	var plans []plan
	for rows.Next() {
		var p plan
		var raw string
		if rows.Scan(&p.id, &raw) == nil && json.Unmarshal([]byte(raw), &p.data) == nil {
			plans = append(plans, p)
		}
	}
	rows.Close()
	var out []CrossDockPlanMatch
	for _, p := range plans {
		sku := strField(p.data, "sku")
		if !want[sku] {
			continue
		}
		patch, _ := json.Marshal(map[string]interface{}{"arrived_with": receiptID})
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'CrossDockPlan' AND id = $1`, schema), p.id, string(patch)); err != nil {
			return out, err
		}
		out = append(out, CrossDockPlanMatch{PlanID: p.id, SKU: sku, Qty: int(numFromInterface(p.data["qty"])),
			Destination: strings.TrimSpace(strField(p.data, "destination_type") + " " + strField(p.data, "destination_ref"))})
	}
	return out, nil
}

// MatchCrossDockPlansForGRN runs the cross-dock match for a posted goods
// receipt and keeps what it found on the receipt (cross_dock_matches), with
// a "Cross-Dock Goods Arrived" notification per plan.
func MatchCrossDockPlansForGRN(tenantID, grnID string) {
	data, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil || data == nil {
		return
	}
	var skus []string
	for _, l := range grnLineMaps(data) {
		skus = append(skus, strField(l, "sku"))
	}
	matches, err := MatchCrossDockPlansOnReceipt(tenantID, grnID, strField(data, "po_id"), strField(data, "asn_id"), skus)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "MatchCrossDockPlansForGRN", fmt.Sprintf("GRN %s: %v", grnID, err), "")
		return
	}
	if len(matches) == 0 {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	b, _ := json.Marshal(map[string]interface{}{"cross_dock_matches": matches})
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb WHERE doctype = 'GRN' AND id = $1`, schema), grnID, string(b))
	for _, m := range matches {
		DispatchNotification(tenantID, "Cross-Dock Goods Arrived", m.PlanID, map[string]string{"grn_id": grnID, "sku": m.SKU, "destination": m.Destination})
	}
}

// --- (h) the yard follows the load ----------------------------------------

// linkOutboundAppointmentToLoad marks the trailer's open Outbound appointment
// In Progress and names it on the load, when loading starts.
func linkOutboundAppointmentToLoad(schema, loadID, trailer string) {
	trailer = strings.TrimSpace(trailer)
	if trailer == "" {
		return
	}
	var apptID string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Appointment' AND deleted_at IS NULL
		AND data->>'appointment_type' = 'Outbound' AND data->>'trailer_no' = $1 AND status IN ('Scheduled', 'CheckedIn')
		ORDER BY created_at DESC LIMIT 1`, schema), trailer).Scan(&apptID) != nil {
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'InProgress', data = data || '{"status":"InProgress"}'::jsonb || jsonb_build_object('loading_task_id', $2::text),
		updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Appointment' AND id = $1`, schema), apptID, loadID)
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('appointment_id', $2::text) WHERE doctype = 'LoadingTask' AND id = $1`, schema), loadID, apptID)
}

// checkOutYardForLoad, when a load departs, checks its trailer out of the
// yard (YardCheckIn → Departed, checked out now) and completes its Outbound
// appointment - the yard no longer waits for a hand check-out.
func checkOutYardForLoad(schema, loadID, trailer string) error {
	trailer = strings.TrimSpace(trailer)
	if trailer == "" {
		return nil
	}
	now := time.Now().Format(time.RFC3339)
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Departed',
		data = data || jsonb_build_object('status', 'Departed', 'checked_out_at', $2::text, 'checked_out_with_load', $3::text), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'YardCheckIn' AND deleted_at IS NULL AND status IN ('InYard', 'AtDoor') AND data->>'trailer_no' = $1`, schema), trailer, now, loadID); err != nil {
		return err
	}
	_, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Completed', data = data || '{"status":"Completed"}'::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'Appointment' AND deleted_at IS NULL AND data->>'appointment_type' = 'Outbound' AND data->>'trailer_no' = $1
		AND status IN ('Scheduled', 'CheckedIn', 'InProgress')`, schema), trailer)
	return err
}
