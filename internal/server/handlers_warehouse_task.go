package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"custom_erp/engines"
)

// Stage 42.2 - the warehouse task spine's HTTP surface. Own file, same
// one-file-per-stage convention handlers_traceability.go/handlers_wms*.go
// already follow, behind the same moduleGate("wms", ...) every other
// floor-ops route uses.

// handleNextTask (42.2.3) is the RF/mobile "give me my next task" call.
// POST rather than GET because it has a real side effect - a successful
// call assigns the returned task to the caller, through the same
// FOR UPDATE SKIP LOCKED transaction GetNextTask uses, so two devices
// calling this at once can never be handed the same task.
func handleNextTask(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LocationCode string `json:"location_code"`
		Queue        string `json:"queue"`
		TaskType     string `json:"task_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LocationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'location_code' is required")
		return
	}
	task, err := engines.GetNextTask(tenantID, userID, req.LocationCode, req.Queue, req.TaskType)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if task == nil {
		// Not an error - an empty queue is a real, expected state for a picker
		// who has simply caught up with the work available.
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"task": nil, "message": "no task available"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"task": task})
}

// handleTransitionTask (42.2.1) moves one WarehouseTask through its
// lifecycle - start work, complete it, flag an exception, cancel it - the
// one route every future RF screen action funnels through, mirroring
// handleBatchStatus's own shape for Batch.
func handleTransitionTask(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		TaskID     string `json:"task_id"`
		Status     string `json:"status"`
		Reason     string `json:"reason"`
		AssignedTo string `json:"assigned_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" || req.Status == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'task_id' and 'status' are required")
		return
	}
	if err := engines.TransitionWarehouseTaskStatus(tenantID, req.TaskID, req.Status, req.Reason, req.AssignedTo, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "updated", "task_id": req.TaskID, "new_status": req.Status})
}

// handleSuggestPutawayBin (42.2.7) is the Putaway screen's "Suggest Bin"
// action - GET and side-effect-free, since SuggestPutawayBin only reads. A
// blank bin_code in the response (with a human-readable reason) is not an
// error - it means no strategy is configured or no bin currently qualifies,
// and the operator falls back to typing one in manually.
func handleSuggestPutawayBin(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	sku := r.URL.Query().Get("sku")
	locationCode := r.URL.Query().Get("location_code")
	batchNo := r.URL.Query().Get("batch_no")
	qty, _ := strconv.Atoi(r.URL.Query().Get("qty"))
	if qty <= 0 {
		qty = 1
	}
	if sku == "" || locationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameters 'sku' and 'location_code' are required")
		return
	}
	binCode, reason, err := engines.SuggestPutawayBin(tenantID, sku, locationCode, qty, batchNo)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"bin_code": binCode, "reason": reason})
}

// handleWarehouseCockpit (42.2.10) serves the whole console's data for one
// location in a single call - every section is independently best-effort
// inside GetWarehouseCockpit, so a problem in one never blanks the rest.
func handleWarehouseCockpit(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	locationCode := r.URL.Query().Get("location_code")
	if locationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'location_code' is required")
		return
	}
	cockpit, err := engines.GetWarehouseCockpit(tenantID, locationCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(cockpit)
}
