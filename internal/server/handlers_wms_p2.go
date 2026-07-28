package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 26.5.16 (WMS Enterprise Maturity Sprint P2 follow-up) handler.
// Kept in its own file, separate from handlers_wms_enterprise.go, to avoid
// colliding with any concurrent session's in-flight edits to that shared
// file - same precedent Stage 26.5's original split already set.
//
// A single inbound endpoint for robotics/conveyor/scale integrations,
// deliberately generic (no vendor-specific SDK/protocol) - authenticated
// via a per-tenant shared API key (same "per-tenant shared secret in a
// header" shape 26.7.11's CleverTap segment-sync webhook uses), action-
// tagged so one endpoint covers putaway/pick/weight-confirm instead of
// three separate routes a real device integration would have to know
// about ahead of time.
func handleRoboticsEvent(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !engines.VerifyRoboticsAPIKey(tenantID, r.Header.Get("X-Robotics-API-Key")) {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "invalid or missing robotics API key")
		return
	}
	var req struct {
		Action   string `json:"action"` // "putaway" | "pick" | "weight_confirm"
		BinCode  string `json:"bin_code"`
		Sku      string `json:"sku"`
		Qty      int    `json:"qty"`
		TaskID   string `json:"task_id"`
		Scan     string `json:"scan"`
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "invalid request body")
		return
	}

	var result engines.RoboticsEventResult
	var err error
	switch req.Action {
	case "putaway":
		result, err = engines.ProcessRoboticsPutaway(tenantID, req.BinCode, req.Sku, req.Qty, req.DeviceID)
	case "pick":
		result, err = engines.ProcessRoboticsPick(tenantID, req.TaskID, req.Scan, req.DeviceID)
	case "weight_confirm":
		result, err = engines.ProcessRoboticsWeightConfirm(tenantID, req.BinCode, req.Sku, req.Qty, req.DeviceID)
	default:
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "unknown action - expected 'putaway', 'pick', or 'weight_confirm'")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
