package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 42.2.1 - the WarehouseTask doctype: one object every floor action
// emits into, closing the second of Stage 42's two foundational holes (the
// first, lot/serial traceability, was 42.1). Before this file,
// PutawayToBin, ExecuteBinReplenishment, ScanPickItem, CrossDockPutaway and
// PostCycleCountAdjustment were five parallel, unrelated functions with no
// shared queue, priority, assignment or ageing - which is why 26.5.13 could
// only instrument three of them, and why there has never been a warehouse
// cockpit (42.2.10).
//
// This file is deliberately just the object and its lifecycle - create, look
// up, transition status. 42.2.2 retrofits the five existing actions to
// additively emit/close a task through these functions; 42.2.3's dispatch
// queue and 42.2.4's ordering-strategy master are their own later items that
// build on top of what is here, not folded in early.

// WarehouseTask status values, matching WarehouseTask.status's Select
// options and the StatusTransitionRule rows the migration seeds.
const (
	WTStatusPending    = "Pending"
	WTStatusAssigned   = "Assigned"
	WTStatusInProgress = "In Progress"
	WTStatusCompleted  = "Completed"
	WTStatusCancelled  = "Cancelled"
	WTStatusException  = "Exception"
)

// WarehouseTaskInfo is one WarehouseTask document, flattened.
type WarehouseTaskInfo struct {
	DocID         string  `json:"doc_id"`
	TaskType      string  `json:"task_type"`
	Status        string  `json:"status"`
	Priority      int     `json:"priority"`
	LocationCode  string  `json:"location_code"`
	FromBin       string  `json:"from_bin,omitempty"`
	ToBin         string  `json:"to_bin,omitempty"`
	Item          string  `json:"item,omitempty"`
	BatchNo       string  `json:"batch_no,omitempty"`
	Qty           float64 `json:"qty,omitempty"`
	UOM           string  `json:"uom,omitempty"`
	AssignedTo    string  `json:"assigned_to,omitempty"`
	Queue         string  `json:"queue,omitempty"`
	WaveID        string  `json:"wave_id,omitempty"`
	SourceDocType string  `json:"source_doc_type,omitempty"`
	SourceDocID   string  `json:"source_doc_id,omitempty"`
	Notes         string  `json:"notes,omitempty"`
}

// NewWarehouseTask is the input shape for CreateWarehouseTask - every field
// WarehouseTaskInfo has except the ones the create call itself decides
// (DocID, Status - always starts Pending).
type NewWarehouseTask struct {
	TaskType      string
	Priority      int
	LocationCode  string
	FromBin       string
	ToBin         string
	Item          string
	BatchNo       string
	Qty           float64
	UOM           string
	Queue         string
	WaveID        string
	SourceDocType string
	SourceDocID   string
	Notes         string
}

var validWarehouseTaskTypes = map[string]bool{
	"Putaway": true, "Pick": true, "Replenish": true, "Count": true,
	"Move": true, "VAS": true, "Load": true, "Unload": true,
}

// CreateWarehouseTask registers a new task, always starting Pending -
// unassigned, undispatched, the state 42.2.3's queue will read from. This is
// the path a real dispatched task (42.2.3 onward) is created through.
func CreateWarehouseTask(tenantID string, in NewWarehouseTask, userID string) (string, error) {
	return insertWarehouseTask(tenantID, in, WTStatusPending, userID)
}

// LogCompletedWarehouseTask (Stage 42.2.2) is the retrofit choke point: the
// five pre-existing floor actions (PutawayToBin, ExecuteBinReplenishment,
// ScanPickItem, CrossDockPutaway, PostCycleCountAdjustment) are synchronous
// and already complete by the time they would create a task, so each calls
// this rather than CreateWarehouseTask + a chain of
// TransitionWarehouseTaskStatus calls to walk a task through a lifecycle
// that, for these five, already happened. Fire-and-forget by design, the
// same contract logTaskCompletion (26.5.13) already has for its own,
// separate productivity log: a warehouse task record is retroactive history
// for 42.2.3's dispatch queue and 42.2.10's cockpit, never a gate on the
// real action, so a logging failure is reported (LogSystemError) and
// swallowed rather than surfaced to the caller - the putaway/pick/count
// itself must never fail because its own instrumentation did.
func LogCompletedWarehouseTask(tenantID string, in NewWarehouseTask, userID string) {
	if _, err := insertWarehouseTask(tenantID, in, WTStatusCompleted, userID); err != nil {
		LogSystemError(tenantID, "", "WARN", "LogCompletedWarehouseTask",
			fmt.Sprintf("failed to log a completed %s WarehouseTask at %s: %v", in.TaskType, in.LocationCode, err), "")
	}
}

func insertWarehouseTask(tenantID string, in NewWarehouseTask, status, userID string) (string, error) {
	if !validWarehouseTaskTypes[in.TaskType] {
		return "", fmt.Errorf("task_type must be one of Putaway, Pick, Replenish, Count, Move, VAS, Load or Unload")
	}
	if strings.TrimSpace(in.LocationCode) == "" {
		return "", fmt.Errorf("location_code is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	taskID := NewDocID("WT")
	data := map[string]interface{}{
		"task_type":       in.TaskType,
		"status":          status,
		"priority":        in.Priority,
		"location_code":   in.LocationCode,
		"from_bin":        in.FromBin,
		"to_bin":          in.ToBin,
		"item":            in.Item,
		"batch_no":        in.BatchNo,
		"qty":             in.Qty,
		"uom":             in.UOM,
		"queue":           in.Queue,
		"wave_id":         in.WaveID,
		"source_doc_type": in.SourceDocType,
		"source_doc_id":   in.SourceDocID,
		"notes":           in.Notes,
	}
	payload, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'WarehouseTask', $2, $3, $4)`, schema),
		taskID, payload, status, userID); err != nil {
		return "", err
	}
	return taskID, nil
}

// warehouseTaskFromData flattens a WarehouseTask document's JSON into a
// WarehouseTaskInfo, the same shape batchFromData/serialFromData use for
// their own doctypes.
func warehouseTaskFromData(docID, dataStr, status string) WarehouseTaskInfo {
	out := WarehouseTaskInfo{DocID: docID, Status: status}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return out
	}
	out.TaskType, _ = data["task_type"].(string)
	if out.Status == "" {
		out.Status, _ = data["status"].(string)
	}
	out.Priority = int(numFromInterface(data["priority"]))
	out.LocationCode, _ = data["location_code"].(string)
	out.FromBin, _ = data["from_bin"].(string)
	out.ToBin, _ = data["to_bin"].(string)
	out.Item, _ = data["item"].(string)
	out.BatchNo, _ = data["batch_no"].(string)
	out.Qty = numFromInterface(data["qty"])
	out.UOM, _ = data["uom"].(string)
	out.AssignedTo, _ = data["assigned_to"].(string)
	out.Queue, _ = data["queue"].(string)
	out.WaveID, _ = data["wave_id"].(string)
	out.SourceDocType, _ = data["source_doc_type"].(string)
	out.SourceDocID, _ = data["source_doc_id"].(string)
	out.Notes, _ = data["notes"].(string)
	return out
}

// GetWarehouseTask looks a task up by its document id. Returns nil, nil for
// an unknown id, the same "not found is not an error" convention GetBatch
// established.
func GetWarehouseTask(tenantID, taskID string) (*WarehouseTaskInfo, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data, COALESCE(status, '') FROM %s.documents WHERE doctype = 'WarehouseTask' AND id = $1`, schema),
		taskID).Scan(&dataStr, &status)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	info := warehouseTaskFromData(taskID, dataStr, status)
	return &info, nil
}

// warehouseTaskTerminal reports whether a status is terminal - a task in one
// of these never transitions again. Completed and Cancelled are both
// terminal; Exception is not, because it is a detour a task can be pulled
// back out of (retried, i.e. Exception -> Assigned) rather than a dead end.
func warehouseTaskTerminal(status string) bool {
	return status == WTStatusCompleted || status == WTStatusCancelled
}

// TransitionWarehouseTaskStatus is the single choke point for every status
// change a WarehouseTask makes, mirroring SetBatchStatus/
// TransitionSerialStatus's own shape: one function every future caller
// (42.2.3's dispatch, a future RF screen) goes through, so the terminal-state
// guard and the reason requirement can never be bypassed by a caller that
// writes the status column directly.
//
// A reason is required moving OUT of Exception or INTO Cancelled - the same
// "a hold release/an abnormal outcome needs a stated reason" instinct
// SetBatchStatus already has for a batch coming off quarantine, here applied
// to "why did this task fail" and "why was this task abandoned".
//
// Stage 42.2.9: moving INTO Exception now also requires a reason - but,
// unlike Cancelled's free text, it must be an Active ReasonCode of category
// 'WMS Exception' (requireActiveReasonCode, the same choke point 26.5.10's
// Cycle Count Variance gate already established), so a genuine exception
// always carries a real, categorised root cause instead of arbitrary notes.
// This is a real behaviour change from 42.2.1-42.2.4, not merely additive -
// acceptable because WarehouseTask only shipped this Stage and has no
// existing Exception transitions on any live tenant to break.
func TransitionWarehouseTaskStatus(tenantID, taskID, newStatus, reason, assignedTo, userID string) error {
	switch newStatus {
	case WTStatusPending, WTStatusAssigned, WTStatusInProgress, WTStatusCompleted, WTStatusCancelled, WTStatusException:
	default:
		return fmt.Errorf("status must be one of Pending, Assigned, In Progress, Completed, Cancelled or Exception")
	}
	task, err := GetWarehouseTask(tenantID, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("warehouse task %s not found", taskID)
	}
	if task.Status == newStatus {
		return nil
	}
	if warehouseTaskTerminal(task.Status) {
		return fmt.Errorf("task %s is %s, a terminal state - it cannot be transitioned further", taskID, task.Status)
	}
	if newStatus == WTStatusCancelled && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("cancelling task %s requires a reason", taskID)
	}
	if task.Status == WTStatusException && newStatus != WTStatusCancelled && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("moving task %s out of Exception requires a reason", taskID)
	}
	if newStatus == WTStatusException {
		if err := requireActiveReasonCode(tenantID, reason, "WMS Exception"); err != nil {
			return fmt.Errorf("flagging task %s as an Exception requires a valid WMS Exception reason code: %w", taskID, err)
		}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	patch := map[string]interface{}{"status": newStatus}
	if strings.TrimSpace(reason) != "" {
		patch["notes"] = reason
	}
	if newStatus == WTStatusException {
		patch["reason_code"] = reason
	}
	if strings.TrimSpace(assignedTo) != "" {
		patch["assigned_to"] = assignedTo
	}
	patchJSON, _ := json.Marshal(patch)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'WarehouseTask' AND id = $3`, schema), patchJSON, newStatus, taskID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WAREHOUSE_TASK_STATUS_CHANGE", "SUCCESS",
		fmt.Sprintf("WarehouseTask %s: %s -> %s%s", taskID, task.Status, newStatus, reasonSuffix(reason)))
	if newStatus == WTStatusException {
		applyExceptionFollowOn(tenantID, task, reason, userID)
	}
	return nil
}

// applyExceptionFollowOn (42.2.9) reads the ReasonCode's process_step/
// follow_on_action and, for the one action with a real concrete mechanism
// (Create Count Task, which is just CreateWarehouseTask), actually takes
// it. Reallocate/Hold have no automatic mechanism to trigger anywhere in
// this codebase yet - creating one would mean guessing at a reallocation/
// hold API this Stage never asked for, so both are recorded as an explicit
// audit event naming the action a human must still take, the same honest-
// gap framing 26.5.13 used for picking instrumentation. Fire-and-forget
// like LogCompletedWarehouseTask: a follow-on action failing must never
// unwind the Exception transition that already succeeded.
func applyExceptionFollowOn(tenantID string, task *WarehouseTaskInfo, reasonCodeID, userID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	var processStep, followOn string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'process_step', ''), COALESCE(data->>'follow_on_action', '') FROM %s.documents WHERE doctype = 'ReasonCode' AND id = $1`, schema),
		reasonCodeID).Scan(&processStep, &followOn); err != nil {
		return
	}
	switch followOn {
	case "Create Count Task":
		countBin := task.ToBin
		if countBin == "" {
			countBin = task.FromBin
		}
		if _, cerr := CreateWarehouseTask(tenantID, NewWarehouseTask{
			TaskType: "Count", Priority: task.Priority, LocationCode: task.LocationCode, FromBin: countBin,
			Item: task.Item, BatchNo: task.BatchNo, Queue: task.Queue,
			SourceDocType: "WarehouseTask", SourceDocID: task.DocID,
			Notes: fmt.Sprintf("Auto-created: exception on task %s (%s)", task.DocID, processStep),
		}, userID); cerr != nil {
			LogSystemError(tenantID, "", "WARN", "applyExceptionFollowOn",
				fmt.Sprintf("failed to auto-create follow-on count task for %s: %v", task.DocID, cerr), "")
		}
	case "Reallocate", "Hold":
		LogAuditEvent(tenantID, userID, "WAREHOUSE_TASK_EXCEPTION_MANUAL_ACTION", "SUCCESS",
			fmt.Sprintf("Task %s exception (%s) calls for %s - no automatic mechanism exists, a human must act", task.DocID, processStep, followOn))
	case "Notify":
		LogAuditEvent(tenantID, userID, "WAREHOUSE_TASK_EXCEPTION_NOTIFY", "SUCCESS",
			fmt.Sprintf("Task %s exception (%s) flagged for notification", task.DocID, processStep))
	}
}

// ---------------------------------------------------------------------------
// 42.2.3 - task queue + dispatch.
// ---------------------------------------------------------------------------

// GetNextTask (42.2.3) hands one Pending WarehouseTask to userID at
// locationCode - the RF/mobile equivalent of a work list, and the dispatch
// choke point 42.2.4's TaskDispatchStrategy master will later pull its
// ordering rule out of. Default ordering (hardcoded until 42.2.4 lifts it
// into config data) is priority descending, then ageing (oldest first) as
// the tie-break - "priority, then whoever has waited longest," the same
// instinct 42.2.4's own plan text names first.
//
// queue and taskType are optional filters (blank = any), for a caller that
// wants "only Pick tasks" or "only this zone's queue" - there is no
// user-eligibility master yet (a picker profile saying which task types a
// user may take), so eligibility today is exactly what the caller asks for,
// nothing implied. Zone-based dispatch is deliberately not here: Bin.zone is
// still free text until 42.2.5 promotes it to a real Zone master, and
// filtering on a string a warehouse could have spelled three ways is worse
// than not filtering at all.
//
// Uses FOR UPDATE SKIP LOCKED (the reservation_sweeper.go precedent) inside
// one transaction with the assignment, so two pickers calling this
// concurrently can never be handed the same task - the second caller's scan
// simply skips the row the first one just locked and finds the next-best
// task instead.
func GetNextTask(tenantID, userID, locationCode, queue, taskType string) (*WarehouseTaskInfo, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("userID is required")
	}
	if strings.TrimSpace(locationCode) == "" {
		return nil, fmt.Errorf("locationCode is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	orderBy, err := resolveDispatchOrder(tenantID, locationCode)
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

	var id, dataStr string
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'WarehouseTask' AND status = $1
		  AND data->>'location_code' = $2
		  AND ($3 = '' OR data->>'queue' = $3)
		  AND ($4 = '' OR data->>'task_type' = $4)
		ORDER BY %s
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, schema, orderBy),
		WTStatusPending, locationCode, queue, taskType).Scan(&id, &dataStr)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	patchJSON, _ := json.Marshal(map[string]interface{}{"status": WTStatusAssigned, "assigned_to": userID})
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3`, schema), patchJSON, WTStatusAssigned, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	info := warehouseTaskFromData(id, dataStr, WTStatusPending)
	info.Status = WTStatusAssigned
	info.AssignedTo = userID
	LogAuditEvent(tenantID, userID, "WAREHOUSE_TASK_DISPATCH", "SUCCESS",
		fmt.Sprintf("Dispatched task %s (%s) at %s to %s", id, info.TaskType, locationCode, userID))
	return &info, nil
}

// ---------------------------------------------------------------------------
// 42.2.4 - TaskDispatchStrategy: GetNextTask's ordering as configurable data.
// ---------------------------------------------------------------------------

// taskDispatchSortFragments maps each allowed sort_order token to a fixed,
// hand-written SQL fragment - a small closed whitelist, not string
// interpolation of tenant-entered text, so a strategy row can only ever
// select among these three orderings, never inject arbitrary SQL. "ageing"
// reads oldest-first (a task that has waited longest sorts first); ties
// within any one criterion fall through to whatever comes after it in the
// configured order.
//
// Deliberately three keys, not the plan's four - "proximity" has no real
// distance data to sort by until 42.2.5's Zone master exists at a minimum,
// and a strategy that claims to order by proximity while actually doing
// something arbitrary would be worse than the option not existing. Add it
// here, and to validateTaskDispatchStrategyMasterRules's whitelist, once
// there is something real for it to read.
var taskDispatchSortFragments = map[string]string{
	"priority": "COALESCE((data->>'priority')::numeric, 0) DESC",
	"ageing":   "created_at ASC",
	"type":     "data->>'task_type' ASC",
}

// defaultDispatchOrder is GetNextTask's original hardcoded ordering (42.2.3),
// and remains what every location gets until a tenant configures a
// TaskDispatchStrategy of its own - byte-identical dispatch behaviour for a
// tenant that never opens this master.
const defaultDispatchOrder = "priority,ageing"

// resolveDispatchOrder looks up the Active TaskDispatchStrategy for
// locationCode (falling back to a blank-location_code "applies everywhere"
// row, then to defaultDispatchOrder), and returns it as a ready-to-splice
// SQL ORDER BY clause. Errors are swallowed in favour of the default rather
// than failing dispatch entirely - a malformed or missing strategy must
// never stop a picker from getting a task, only fail back to the ordering
// that always worked.
func resolveDispatchOrder(tenantID, locationCode string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var sortOrder string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data->>'sort_order' FROM %s.documents
		WHERE doctype = 'TaskDispatchStrategy' AND COALESCE(status, '') = 'Active'
		  AND data->>'location_code' = $1
		LIMIT 1`, schema), locationCode).Scan(&sortOrder)
	if err == sql.ErrNoRows {
		err = db.DB.QueryRow(fmt.Sprintf(`
			SELECT data->>'sort_order' FROM %s.documents
			WHERE doctype = 'TaskDispatchStrategy' AND COALESCE(status, '') = 'Active'
			  AND COALESCE(data->>'location_code', '') = ''
			LIMIT 1`, schema)).Scan(&sortOrder)
	}
	if err == sql.ErrNoRows {
		sortOrder = defaultDispatchOrder
	} else if err != nil {
		return sqlOrderByFromTokens(defaultDispatchOrder), nil
	}
	if built := sqlOrderByFromTokens(sortOrder); built != "" {
		return built, nil
	}
	return sqlOrderByFromTokens(defaultDispatchOrder), nil
}

// sqlOrderByFromTokens turns a comma-separated sort_order string into a SQL
// ORDER BY clause, silently dropping any token not in
// taskDispatchSortFragments (a stale/mistyped token degrades gracefully
// rather than erroring dispatch) and falling back to created_at ASC as the
// final tie-break always, so two tasks equal on every configured criterion
// still return in a stable order.
func sqlOrderByFromTokens(tokens string) string {
	var parts []string
	for _, tok := range strings.Split(tokens, ",") {
		if frag, ok := taskDispatchSortFragments[strings.TrimSpace(tok)]; ok {
			parts = append(parts, frag)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	parts = append(parts, "created_at ASC")
	return strings.Join(parts, ", ")
}

// validateTaskDispatchStrategyMasterRules (Stage 42.2.4) enforces the two
// things TaskDispatchStrategy's generic metadata pass cannot express:
// sort_order names only real criteria, and at most one Active strategy
// applies to any given location_code (including the blank "everywhere"
// value) - two Active rows claiming the same location would make
// resolveDispatchOrder's answer depend on which one the query happens to
// read first.
func validateTaskDispatchStrategyMasterRules(tenantID, docID string, payload map[string]interface{}) error {
	sortOrder := strField(payload, "sort_order")
	if sortOrder == "" {
		return nil // mandatory; ValidateDocument has already said so.
	}
	for _, tok := range strings.Split(sortOrder, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if _, ok := taskDispatchSortFragments[tok]; !ok {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Sort Order",
				Message: fmt.Sprintf("%q is not a recognised sort criterion - expected priority, ageing or type", tok)}
		}
	}

	status := strField(payload, "status")
	if status != "Active" {
		return nil
	}
	locationCode := strField(payload, "location_code")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var existingID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'TaskDispatchStrategy' AND status = 'Active'
		  AND COALESCE(data->>'location_code', '') = $1 AND id != $2
		LIMIT 1`, schema), locationCode, docID).Scan(&existingID)
	if err == nil {
		scope := "every location"
		if locationCode != "" {
			scope = "location " + locationCode
		}
		return &ValidationError{Code: "MASTER-0053", Message: fmt.Sprintf(
			"an Active dispatch strategy already applies to %s (%s) - deactivate it first", scope, existingID)}
	}
	return nil
}
