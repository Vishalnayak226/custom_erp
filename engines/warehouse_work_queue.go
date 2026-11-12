package engines

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 146.3.3 - the handheld work queue.
//
// The WarehouseTask engine already exists; this adds what was missing around
// it. Emitters create tasks where the work is born (a posted receipt, a
// released wave), "my next task" serves them by type priority from an open
// pool, starting a task locks it to the worker, a task can be split in two,
// and finishing the work any other way closes the task. All of it runs only
// when the setting wms.work_queue is on; off, no task is created and every
// screen is as it was.
//
// Covered: put-away tasks from a posted GRN, pick tasks from a wave (one per
// reserved bin allocation, source = the order's pick task). Replenish, move
// and count tasks are served and finished when other paths create them, but
// this stage adds no emitter for them.

// WorkQueueOn reports whether the handheld work queue is switched on.
func WorkQueueOn(tenantID string) bool { return GetSettingBool(tenantID, "wms.work_queue") }

// workTaskRankSQL orders task types: Pick, Replenish, Put-away, Move, Count.
const workTaskRankSQL = `CASE data->>'task_type' WHEN 'Pick' THEN 0 WHEN 'Replenish' THEN 1 WHEN 'Putaway' THEN 2 WHEN 'Move' THEN 3 WHEN 'Count' THEN 4 ELSE 5 END`

// workQueueRows lists what a worker may see at a location: what they hold,
// then the open pool by type priority, priority, age.
func workQueueRows(tenantID, worker, locationCode, sourceDoc string, limit int) ([]WarehouseTaskInfo, error) {
	if strings.TrimSpace(worker) == "" {
		return nil, errors.New("user is required")
	}
	if strings.TrimSpace(locationCode) == "" {
		return nil, errors.New("location is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data::text, COALESCE(status, '') FROM %s.documents
		 WHERE doctype = 'WarehouseTask' AND deleted_at IS NULL
		   AND data->>'location_code' = $1
		   AND ($2 = '' OR data->>'source_doc_id' = $2)
		   AND (status = 'Pending' OR (status IN ('Assigned', 'In Progress') AND data->>'assigned_to' = $3))
		 ORDER BY (status = 'Pending'), %s, COALESCE(NULLIF(data->>'priority', '')::numeric, 0) DESC, created_at, id
		 LIMIT $4`, schema, workTaskRankSQL), locationCode, sourceDoc, worker, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WarehouseTaskInfo
	for rows.Next() {
		var id, raw, st string
		if err := rows.Scan(&id, &raw, &st); err != nil {
			return nil, err
		}
		out = append(out, warehouseTaskFromData(id, raw, st))
	}
	return out, rows.Err()
}

// ListWorkTasks is the worker's list at a location, optionally one source
// document's tasks only.
func ListWorkTasks(tenantID, worker, locationCode, sourceDoc string) ([]WarehouseTaskInfo, error) {
	return workQueueRows(tenantID, worker, locationCode, strings.TrimSpace(sourceDoc), 100)
}

// NextWorkTask is "my next task": what the worker already holds, else the top
// of the open pool. It only looks; StartWorkTask takes the task.
func NextWorkTask(tenantID, worker, locationCode, sourceDoc string) (*WarehouseTaskInfo, error) {
	rows, err := workQueueRows(tenantID, worker, locationCode, strings.TrimSpace(sourceDoc), 1)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// StartWorkTask locks a task to the worker. The take is one conditional
// UPDATE, so of two workers starting the same task exactly one wins. Starting
// a task you already hold changes nothing.
func StartWorkTask(tenantID, taskID, worker string) (*WarehouseTaskInfo, error) {
	if strings.TrimSpace(worker) == "" {
		return nil, errors.New("user is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	patch, _ := json.Marshal(map[string]interface{}{
		"status": WTStatusInProgress, "assigned_to": worker, "started_at": time.Now().UTC().Format(time.RFC3339),
	})
	var raw string
	err = db.DB.QueryRow(fmt.Sprintf(`
		UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'WarehouseTask' AND id = $3
		   AND (status = 'Pending' OR (status IN ('Assigned', 'In Progress') AND data->>'assigned_to' = $4))
		RETURNING data::text`, schema), string(patch), WTStatusInProgress, taskID, worker).Scan(&raw)
	if err == sql.ErrNoRows {
		cur, gerr := GetWarehouseTask(tenantID, taskID)
		if gerr != nil {
			return nil, gerr
		}
		switch {
		case cur == nil:
			return nil, fmt.Errorf("task %s not found", taskID)
		case warehouseTaskTerminal(cur.Status):
			return nil, fmt.Errorf("task %s is %s", taskID, cur.Status)
		default:
			return nil, fmt.Errorf("task %s is already taken by %s", taskID, cur.AssignedTo)
		}
	}
	if err != nil {
		return nil, err
	}
	info := warehouseTaskFromData(taskID, raw, WTStatusInProgress)
	LogAuditEvent(tenantID, worker, "WAREHOUSE_TASK_START", "SUCCESS", fmt.Sprintf("Task %s (%s) started by %s", taskID, info.TaskType, worker))
	return &info, nil
}

// heldBy loads a task and checks the worker holds it.
func heldBy(tenantID, taskID, worker string) (*WarehouseTaskInfo, error) {
	t, err := GetWarehouseTask(tenantID, taskID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	if t.Status == WTStatusCompleted {
		return t, nil
	}
	if warehouseTaskTerminal(t.Status) {
		return nil, fmt.Errorf("task %s is %s", taskID, t.Status)
	}
	if (t.Status != WTStatusInProgress && t.Status != WTStatusAssigned) || t.AssignedTo != worker {
		return nil, fmt.Errorf("task %s is not started by you", taskID)
	}
	return t, nil
}

// CompleteWorkTask finishes a held task. A put-away is done into bin (the
// stock is binned through PutawayToBin, which closes the task); a pick scans
// its units through the pick path; any other type is marked done.
func CompleteWorkTask(tenantID, taskID, worker, bin string) error {
	t, err := heldBy(tenantID, taskID, worker)
	if err != nil {
		return err
	}
	if t.Status == WTStatusCompleted {
		return nil
	}
	switch t.TaskType {
	case "Putaway":
		bin = strings.TrimSpace(bin)
		if bin == "" {
			return errors.New("a bin is needed to finish a put-away")
		}
		if t.Item != "" && t.Qty >= 1 {
			if err := PutawayToBin(tenantID, bin, t.Item, int(t.Qty+0.5), worker); err != nil {
				return err
			}
		}
	case "Replenish", "Move":
		// Stage 146.3.3: the stock moves bin to bin; the move closes the task.
		if t.FromBin != "" && t.ToBin != "" && t.Item != "" && t.Qty >= 1 {
			in := BinMoveInput{FromBin: t.FromBin, ToBin: t.ToBin, SKU: t.Item, Qty: int(t.Qty + 0.5), BatchNo: t.BatchNo, Reason: t.Notes, UserID: worker, TaskType: t.TaskType}
			if t.TaskType == "Replenish" {
				in.VoucherType = "BinReplenishment"
			}
			if err := MoveBinStock(tenantID, in); err != nil {
				return err
			}
		}
	case "Pick":
		if t.SourceDocType == "FulfillmentTask" && t.SourceDocID != "" && t.Item != "" {
			n := int(t.Qty + 0.5)
			for done := 0; done < n; done++ {
				if _, _, err := ScanPickItem(tenantID, t.SourceDocID, t.Item); err != nil {
					if done > 0 {
						_ = setWorkTaskQty(tenantID, taskID, t.Qty-float64(done))
					}
					return err
				}
			}
		}
	}
	if cur, _ := GetWarehouseTask(tenantID, taskID); cur != nil && cur.Status == WTStatusCompleted {
		return nil // the put-away closed it
	}
	return TransitionWarehouseTaskStatus(tenantID, taskID, WTStatusCompleted, "", worker, worker)
}

func setWorkTaskQty(tenantID, taskID string, qty float64) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	patch, _ := json.Marshal(map[string]interface{}{"qty": qty})
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'WarehouseTask' AND id = $2`, schema), string(patch), taskID)
	return err
}

// SplitWorkTask cuts qty off an open task into a new pending task; the two
// quantities add up to the original. A task another worker holds cannot be
// split. Returns the new task's id.
func SplitWorkTask(tenantID, taskID string, qty float64, worker string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var raw, status string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data::text, COALESCE(status, '') FROM %s.documents
		WHERE doctype = 'WarehouseTask' AND id = $1 FOR UPDATE`, schema), taskID).Scan(&raw, &status)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("task %s not found", taskID)
	}
	if err != nil {
		return "", err
	}
	t := warehouseTaskFromData(taskID, raw, status)
	if warehouseTaskTerminal(t.Status) {
		return "", fmt.Errorf("task %s is %s", taskID, t.Status)
	}
	if t.Status != WTStatusPending && t.AssignedTo != worker {
		return "", fmt.Errorf("task %s is held by %s", taskID, t.AssignedTo)
	}
	if qty <= 0 || qty >= t.Qty {
		return "", fmt.Errorf("the part split off must be more than 0 and less than %v", t.Qty)
	}
	newID := NewDocID("WT")
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		SELECT $1, 'WarehouseTask',
		       data || jsonb_build_object('qty', $2::numeric, 'status', 'Pending', 'assigned_to', '', 'split_from', id::text), 'Pending', created_by
		  FROM %s.documents WHERE doctype = 'WarehouseTask' AND id = $3`, schema, schema), newID, qty, taskID); err != nil {
		return "", err
	}
	patch, _ := json.Marshal(map[string]interface{}{"qty": t.Qty - qty})
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'WarehouseTask' AND id = $2`, schema), string(patch), taskID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, worker, "WAREHOUSE_TASK_SPLIT", "SUCCESS", fmt.Sprintf("Task %s split: %v kept, %v in %s", taskID, t.Qty-qty, qty, newID))
	return newID, nil
}

// closeWorkTasksForPutaway is the one hook in the put-away path: stock put
// away by any route closes the matching open put-away tasks (the putting-away
// user's own first), or shrinks the last one when only part was binned.
func closeWorkTasksForPutaway(tenantID, location, sku string, qty float64, userID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || qty <= 0 {
		return
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(NULLIF(data->>'qty', '')::numeric, 0) FROM %s.documents
		 WHERE doctype = 'WarehouseTask' AND deleted_at IS NULL AND data->>'task_type' = 'Putaway'
		   AND data->>'location_code' = $1 AND data->>'item' = $2
		   AND status IN ('Pending', 'Assigned', 'In Progress')
		 ORDER BY (COALESCE(data->>'assigned_to', '') = $3) DESC, created_at, id`, schema), location, sku, userID)
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
	left := qty
	for _, o := range tasks {
		if left <= 0 {
			break
		}
		if o.qty <= left+1e-9 {
			if err := TransitionWarehouseTaskStatus(tenantID, o.id, WTStatusCompleted, "", "", userID); err == nil {
				left -= o.qty
			}
			continue
		}
		if setWorkTaskQty(tenantID, o.id, o.qty-left) == nil {
			left = 0
		}
	}
}

// CancelWorkTasksForSource cancels the open tasks of a source document that
// was cancelled (a GRN, an order's pick task). Returns how many it cancelled.
func CancelWorkTasksForSource(tenantID, sourceType, sourceID, userID string) int {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0
	}
	n, _ := cancelWorkTasksExec(db.DB, schema, sourceType, sourceID)
	return n
}

type workExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// cancelWorkTasksExec is the one cancel statement, run on the pool or inside
// the caller's transaction (an order cancel).
func cancelWorkTasksExec(ex workExecer, schema, sourceType, sourceID string) (int, error) {
	if strings.TrimSpace(sourceID) == "" {
		return 0, nil
	}
	patch, _ := json.Marshal(map[string]interface{}{"status": WTStatusCancelled, "notes": "Source document cancelled"})
	res, err := ex.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'WarehouseTask' AND data->>'source_doc_type' = $3 AND data->>'source_doc_id' = $4
		   AND status IN ('Pending', 'Assigned', 'In Progress', 'Exception')`, schema), string(patch), WTStatusCancelled, sourceType, sourceID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// emitGRNPutawayTasks creates one pending put-away task per accepted receipt
// line (same item and lot together). Best-effort: the receipt has posted.
func emitGRNPutawayTasks(tenantID, locationCode, grnID, userID string, accepted []interface{}) {
	if grnID == "" || !WorkQueueOn(tenantID) {
		return
	}
	type key struct{ sku, batch string }
	qty := map[key]float64{}
	var order []key
	for _, raw := range accepted {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		sku, _ := m["sku"].(string)
		batch, _ := m["batch_no"].(string)
		q := numFromInterface(m["qty"])
		if sku == "" || q <= 0 {
			continue
		}
		k := key{sku, batch}
		if _, seen := qty[k]; !seen {
			order = append(order, k)
		}
		qty[k] += q
	}
	for _, k := range order {
		if _, err := CreateWarehouseTask(tenantID, NewWarehouseTask{
			TaskType: "Putaway", LocationCode: locationCode, Item: k.sku, BatchNo: k.batch, Qty: qty[k],
			SourceDocType: "GRN", SourceDocID: grnID,
		}, userID); err != nil {
			LogSystemError(tenantID, "", "WARN", "emitGRNPutawayTasks", fmt.Sprintf("GRN %s: put-away task for %s not created: %v", grnID, k.sku, err), "")
		}
	}
}

// emitWavePickTasks creates one pending pick task per reserved bin allocation
// of a wave, sourced from the order's pick task so cancelling the order
// cancels them. An allocation that already has an open task is left alone.
func emitWavePickTasks(tenantID, schema, waveID, locationCode string, allocations []taskAllocationLine) {
	if !WorkQueueOn(tenantID) {
		return
	}
	for _, a := range allocations {
		var have int
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents
			WHERE doctype = 'WarehouseTask' AND data->>'task_type' = 'Pick' AND data->>'source_doc_id' = $1
			  AND data->>'item' = $2 AND data->>'from_bin' = $3 AND COALESCE(data->>'batch_no', '') = $4
			  AND status IN ('Pending', 'Assigned', 'In Progress')`, schema), a.TaskID, a.SKU, a.BinCode, a.BatchNo).Scan(&have)
		if have > 0 {
			continue
		}
		if _, err := CreateWarehouseTask(tenantID, NewWarehouseTask{
			TaskType: "Pick", LocationCode: locationCode, FromBin: a.BinCode, Item: a.SKU, BatchNo: a.BatchNo, Qty: float64(a.Qty),
			WaveID: waveID, SourceDocType: waveSourceDocType(a.TaskID), SourceDocID: a.TaskID,
		}, "system"); err != nil {
			LogSystemError(tenantID, "", "WARN", "emitWavePickTasks", fmt.Sprintf("wave %s: pick task for %s not created: %v", waveID, a.SKU, err), "")
		}
	}
}
