package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 146.3.3 - the handheld work queue's one route. The body carries an
// action (list, next, start, done, split); the engine does the work.
func handleWorkQueue(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		Action       string  `json:"action"`
		LocationCode string  `json:"location_code"`
		SourceDoc    string  `json:"source_doc_id"`
		TaskID       string  `json:"task_id"`
		Bin          string  `json:"bin"`
		Qty          float64 `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if strings.EqualFold(req.Action, "probe") { // the RF menu asks whether to offer My next task; 200 either way, so a switched-off queue logs no error
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"on": engines.WorkQueueOn(tenantID)})
		return
	}
	if !engines.WorkQueueOn(tenantID) {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The work queue is off")
		return
	}
	fail := func(err error) { writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error()) }
	need := func(ok bool, msg string) bool {
		if !ok {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, msg)
		}
		return ok
	}
	switch strings.ToLower(req.Action) {
	case "list":
		if !need(req.LocationCode != "", "Field 'location_code' is required") {
			return
		}
		tasks, err := engines.ListWorkTasks(tenantID, userID, req.LocationCode, req.SourceDoc)
		if err != nil {
			fail(err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"tasks": tasks})
	case "next":
		if !need(req.LocationCode != "", "Field 'location_code' is required") {
			return
		}
		task, err := engines.NextWorkTask(tenantID, userID, req.LocationCode, req.SourceDoc)
		if err != nil {
			fail(err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"task": task})
	case "start":
		if !need(req.TaskID != "", "Field 'task_id' is required") {
			return
		}
		task, err := engines.StartWorkTask(tenantID, req.TaskID, userID)
		if err != nil {
			fail(err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"task": task})
	case "done":
		if !need(req.TaskID != "", "Field 'task_id' is required") {
			return
		}
		if err := engines.CompleteWorkTask(tenantID, req.TaskID, userID, req.Bin); err != nil {
			fail(err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "done", "task_id": req.TaskID})
	case "split":
		if !need(req.TaskID != "" && req.Qty > 0, "Fields 'task_id' and 'qty' are required") {
			return
		}
		id, err := engines.SplitWorkTask(tenantID, req.TaskID, req.Qty, userID)
		if err != nil {
			fail(err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "split", "task_id": req.TaskID, "new_task_id": id})
	default:
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'action' must be list, next, start, done or split")
	}
}

// Stage 146.3.2 - RF receiving asks for codes to stick on pieces. mint:true
// hands out n new codes from the PieceCode series; a scanned code needs no
// call (it is sent with the receipt line as typed). Off for items and stores
// that have not opted in.
func handlePieceCodeMint(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		SKU          string `json:"sku"`
		LocationCode string `json:"location_code"`
		Count        int    `json:"count"`
		Probe        bool   `json:"probe"`
	}
	decErr := json.NewDecoder(r.Body).Decode(&req)
	if decErr == nil && req.Probe { // the RF menu asks whether to show Piece codes; 200 either way
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"on": engines.PieceCodesSwitchedOn(tenantID)})
		return
	}
	if !engines.PieceCodesSwitchedOn(tenantID) {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Codes per piece are off")
		return
	}
	if decErr != nil || req.SKU == "" || req.LocationCode == "" || req.Count < 1 || req.Count > 500 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'sku', 'location_code' and 'count' (1-500) are required")
		return
	}
	if !engines.PieceCodesOn(tenantID, req.SKU, req.LocationCode) {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Codes per piece are off for this item here")
		return
	}
	codes, err := engines.MintPieceCodes(tenantID, req.Count)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"codes": codes})
}
