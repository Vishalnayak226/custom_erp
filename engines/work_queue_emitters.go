package engines

import (
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 146.3.3 (the Left part): the work queue gets its Replenish, Move and
// Count tasks. Each is created where the work is born, only when
// wms.work_queue is on, once (an open task of the same kind is never doubled),
// and closes or cancels with its source:
//
//	Replenish  a pick-face bin below its min (BinReplenishmentRule): after a pick
//	           and on a sweep; done when the stock moves in (any screen), cancelled
//	           when the face is no longer short.
//	Move       a bin move / re-slot request (RequestBinMove); done by the move.
//	Count      one per bin frozen by a count session (StartPhysicalInventory);
//	           completed when the session closes, cancelled when it is. A short
//	           pick's Count task (short_pick_hold.go) already reaches the queue.

const openWorkTaskStatusSQL = `status IN ('Pending', 'Assigned', 'In Progress')`

// openWorkTaskID returns the id of an open task of the same kind and place,
// or "". For a Count task the source must match too.
func openWorkTaskID(schema, taskType, from, to, item, srcType, srcID string) string {
	var id string
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		 WHERE doctype = 'WarehouseTask' AND deleted_at IS NULL AND data->>'task_type' = $1
		   AND COALESCE(data->>'from_bin', '') = $2 AND COALESCE(data->>'to_bin', '') = $3 AND COALESCE(data->>'item', '') = $4
		   AND ($5 = '' OR (COALESCE(data->>'source_doc_type', '') = $5 AND COALESCE(data->>'source_doc_id', '') = $6))
		   AND %s LIMIT 1`, schema, openWorkTaskStatusSQL), taskType, from, to, item, srcType, srcID).Scan(&id)
	return id
}

// EmitReplenishTasks sweeps the pick-face rules at locationCode: a face below
// its min gets one Replenish task per reserve bin that can fill it, a pending
// task whose face is no longer short is cancelled. Returns how many it made.
func EmitReplenishTasks(tenantID, locationCode string) int {
	return emitReplenish(tenantID, locationCode, "", true)
}

func emitReplenish(tenantID, locationCode, onlySKU string, sweep bool) int {
	if !WorkQueueOn(tenantID) || strings.TrimSpace(locationCode) == "" {
		return 0
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0
	}
	sugg, err := GetBinReplenishmentSuggestions(tenantID, locationCode)
	if err != nil {
		LogSystemError(tenantID, "", "WARN", "EmitReplenishTasks", err.Error(), "")
		return 0
	}
	made := 0
	want := map[string]bool{}
	for _, s := range sugg {
		if s.FromBinCode == "" || s.MoveQty <= 0 || (onlySKU != "" && s.Sku != onlySKU) {
			continue
		}
		want[s.FromBinCode+"|"+s.BinCode+"|"+s.Sku] = true
		if openWorkTaskID(schema, "Replenish", s.FromBinCode, s.BinCode, s.Sku, "", "") != "" {
			continue
		}
		if _, err := CreateWarehouseTask(tenantID, NewWarehouseTask{
			TaskType: "Replenish", Priority: 1, LocationCode: locationCode, FromBin: s.FromBinCode, ToBin: s.BinCode,
			Item: s.Sku, Qty: float64(s.MoveQty), SourceDocType: "BinReplenishmentRule", SourceDocID: s.BinCode,
			Notes: fmt.Sprintf("Pick face %s is at %d, min %d", s.BinCode, s.CurrentQty, s.MinQty),
		}, "system"); err != nil {
			LogSystemError(tenantID, "", "WARN", "EmitReplenishTasks", fmt.Sprintf("replenish task for %s not created: %v", s.BinCode, err), "")
			continue
		}
		made++
	}
	if sweep && onlySKU == "" {
		cancelStaleReplenishTasks(tenantID, schema, locationCode, want)
	}
	return made
}

// cancelStaleReplenishTasks cancels the pending replenish tasks at a location
// that the current rules no longer ask for (the face was refilled by hand, the
// rule went inactive, the reserve is gone).
func cancelStaleReplenishTasks(tenantID, schema, locationCode string, want map[string]bool) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'from_bin', ''), COALESCE(data->>'to_bin', ''), COALESCE(data->>'item', '')
		  FROM %s.documents
		 WHERE doctype = 'WarehouseTask' AND deleted_at IS NULL AND data->>'task_type' = 'Replenish'
		   AND data->>'location_code' = $1 AND data->>'source_doc_type' = 'BinReplenishmentRule' AND status = 'Pending'`, schema), locationCode)
	if err != nil {
		return
	}
	var stale []string
	for rows.Next() {
		var id, from, to, item string
		if rows.Scan(&id, &from, &to, &item) == nil && !want[from+"|"+to+"|"+item] {
			stale = append(stale, id)
		}
	}
	rows.Close()
	for _, id := range stale {
		_ = TransitionWarehouseTaskStatus(tenantID, id, WTStatusCancelled, "Pick face is no longer short", "", "system")
	}
}

// emitReplenishAfterPick is the pick hook: a pick may have taken a face of
// sku below its min.
func emitReplenishAfterPick(tenantID, locationCode, sku string) {
	if sku == "" || !WorkQueueOn(tenantID) {
		return
	}
	emitReplenish(tenantID, locationCode, sku, false)
}

// MoveRequest asks for stock to be moved (a re-slot, a drain of a blocked bin).
type MoveRequest struct {
	FromBin string `json:"from_bin"`
	ToBin   string `json:"to_bin"`
	SKU     string `json:"sku"`
	Qty     int    `json:"qty"`
	Reason  string `json:"reason"`
}

// RequestBinMove puts a Move task in the work queue and returns its id. The
// same request again returns the open task. The task completes when the stock
// is moved from the bin to the bin, whichever screen moves it.
func RequestBinMove(tenantID string, req MoveRequest, userID string) (string, error) {
	refuse := func(msg string) (string, error) {
		return "", &ValidationError{Code: "WMS-1315", SubFor: "Move", Message: msg}
	}
	if !WorkQueueOn(tenantID) {
		return refuse("the work queue is off - turn on Handheld work queue first")
	}
	req.FromBin, req.ToBin, req.SKU = strings.TrimSpace(req.FromBin), strings.TrimSpace(req.ToBin), strings.TrimSpace(req.SKU)
	if req.FromBin == "" || req.ToBin == "" || req.SKU == "" || req.Qty <= 0 {
		return refuse("give the from bin, the to bin, the item and a quantity")
	}
	if req.FromBin == req.ToBin {
		return refuse("the two bins must differ")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	binLoc := func(code string) (string, bool) {
		var loc string
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'location', '') FROM %s.documents WHERE doctype = 'Bin' AND deleted_at IS NULL AND data->>'bin_code' = $1 LIMIT 1`, schema), code).Scan(&loc)
		return loc, err == nil
	}
	loc, ok := binLoc(req.FromBin)
	if !ok {
		return refuse(fmt.Sprintf("bin %s does not exist", req.FromBin))
	}
	if _, ok := binLoc(req.ToBin); !ok {
		return refuse(fmt.Sprintf("bin %s does not exist", req.ToBin))
	}
	if loc == "" {
		return refuse(fmt.Sprintf("bin %s has no location", req.FromBin))
	}
	var held int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(qty), 0) FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2`, schema), req.FromBin, req.SKU).Scan(&held)
	if held < req.Qty {
		return refuse(fmt.Sprintf("bin %s holds %d of %s, not %d", req.FromBin, held, req.SKU, req.Qty))
	}
	if id := openWorkTaskID(schema, "Move", req.FromBin, req.ToBin, req.SKU, "", ""); id != "" {
		return id, nil
	}
	return CreateWarehouseTask(tenantID, NewWarehouseTask{
		TaskType: "Move", Priority: 2, LocationCode: loc, FromBin: req.FromBin, ToBin: req.ToBin, Item: req.SKU, Qty: float64(req.Qty),
		SourceDocType: "MoveRequest", SourceDocID: req.FromBin + ">" + req.ToBin + ">" + req.SKU, Notes: strings.TrimSpace(req.Reason),
	}, userID)
}

// closeWorkTasksForMove is the move hook: stock moved into toBin closes the
// open Move task for the same bins and item, and the open Replenish task for
// that face and item (from any reserve bin). A task for more than was moved
// keeps the rest.
func closeWorkTasksForMove(tenantID string, in BinMoveInput) {
	if !WorkQueueOn(tenantID) || in.Qty <= 0 {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(NULLIF(data->>'qty', '')::numeric, 0) FROM %s.documents
		 WHERE doctype = 'WarehouseTask' AND deleted_at IS NULL AND data->>'item' = $1 AND data->>'to_bin' = $2
		   AND ((data->>'task_type' = 'Move' AND data->>'from_bin' = $3) OR data->>'task_type' = 'Replenish')
		   AND %s ORDER BY created_at, id`, schema, openWorkTaskStatusSQL), in.SKU, in.ToBin, in.FromBin)
	if err != nil {
		return
	}
	type open struct {
		id  string
		qty float64
	}
	var tasks []open
	for rows.Next() {
		var o open
		if rows.Scan(&o.id, &o.qty) == nil {
			tasks = append(tasks, o)
		}
	}
	rows.Close()
	left := float64(in.Qty)
	for _, o := range tasks {
		if left <= 0 {
			break
		}
		if o.qty <= left+1e-9 {
			if TransitionWarehouseTaskStatus(tenantID, o.id, WTStatusCompleted, "", "", in.UserID) == nil {
				left -= o.qty
			}
			continue
		}
		if setWorkTaskQty(tenantID, o.id, o.qty-left) == nil {
			left = 0
		}
	}
}

// emitCountTasks makes one Count task per bin a count session froze.
func emitCountTasks(tenantID, locationCode, piID, userID string, bins []string) {
	if piID == "" || !WorkQueueOn(tenantID) {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	for _, b := range bins {
		if b == "" || openWorkTaskID(schema, "Count", b, "", "", "PhysicalInventory", piID) != "" {
			continue
		}
		if _, err := CreateWarehouseTask(tenantID, NewWarehouseTask{
			TaskType: "Count", Priority: 1, LocationCode: locationCode, FromBin: b,
			SourceDocType: "PhysicalInventory", SourceDocID: piID, Notes: "Count session " + piID,
		}, userID); err != nil {
			LogSystemError(tenantID, "", "WARN", "emitCountTasks", fmt.Sprintf("count task for %s in %s not created: %v", b, piID, err), "")
		}
	}
}

// completeWorkTasksForSource completes the open tasks of a source document
// that finished (a closed count session).
func completeWorkTasksForSource(tenantID, sourceType, sourceID, userID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || sourceID == "" {
		return
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id FROM %s.documents WHERE doctype = 'WarehouseTask' AND deleted_at IS NULL
		   AND data->>'source_doc_type' = $1 AND data->>'source_doc_id' = $2 AND %s`, schema, openWorkTaskStatusSQL), sourceType, sourceID)
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
		_ = TransitionWarehouseTaskStatus(tenantID, id, WTStatusCompleted, "", "", userID)
	}
}
