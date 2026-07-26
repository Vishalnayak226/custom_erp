package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 26.5 (WMS Enterprise Maturity Sprint): cross-dock staging, LPN/
// carton/pallet grouping, bin-to-bin replenishment, wave/batch picking,
// cartonization suggestions, the ABC cycle-count planner, and the blind-
// recount + variance-reason cycle-count workflow. Kept in its own file
// (rather than appended to handlers_wms.go, which Stage 26.12.3 was mid-
// edit on when this Stage started) - same role-open convention as the rest
// of handlers_wms.go, no separate WMS-operator role exists yet.

func handleCrossDockCheck(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		Sku          string `json:"sku"`
		LocationCode string `json:"location_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Sku == "" || req.LocationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'sku' and 'location_code' are required")
		return
	}
	matchedQty, opportunities, err := engines.CheckCrossDockOpportunity(tenantID, req.Sku, req.LocationCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if opportunities == nil {
		opportunities = []engines.CrossDockOpportunity{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"matched_qty": matchedQty, "opportunities": opportunities})
}

func handleCrossDockPutaway(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		Sku          string `json:"sku"`
		LocationCode string `json:"location_code"`
		Qty          int    `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Sku == "" || req.LocationCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'sku' and 'location_code' are required")
		return
	}
	staged, opportunities, err := engines.CrossDockPutaway(tenantID, req.Sku, req.LocationCode, req.Qty, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if opportunities == nil {
		opportunities = []engines.CrossDockOpportunity{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"staged": staged, "opportunities": opportunities})
}

func handleLPNAssign(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LPNCode   string `json:"lpn_code"`
		BinCode   string `json:"bin_code"`
		Sku       string `json:"sku"`
		Condition string `json:"condition"`
		Qty       int    `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LPNCode == "" || req.BinCode == "" || req.Sku == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'lpn_code', 'bin_code', and 'sku' are required")
		return
	}
	if err := engines.AssignToLPN(tenantID, req.LPNCode, req.BinCode, req.Sku, req.Condition, req.Qty, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleLPNContents(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	lpnCode := r.URL.Query().Get("lpn_code")
	if lpnCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'lpn_code' is required")
		return
	}
	lines, err := engines.GetLPNContents(tenantID, lpnCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if lines == nil {
		lines = []engines.LPNContentLine{}
	}
	_ = json.NewEncoder(w).Encode(lines)
}

func handleBinReplenishmentSuggestions(w http.ResponseWriter, r *http.Request) {
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
	suggestions, err := engines.GetBinReplenishmentSuggestions(tenantID, locationCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if suggestions == nil {
		suggestions = []engines.BinReplenishmentSuggestion{}
	}
	_ = json.NewEncoder(w).Encode(suggestions)
}

func handleBinReplenishmentExecute(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		FromBinCode string `json:"from_bin_code"`
		ToBinCode   string `json:"to_bin_code"`
		Sku         string `json:"sku"`
		Qty         int    `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FromBinCode == "" || req.ToBinCode == "" || req.Sku == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'from_bin_code', 'to_bin_code', and 'sku' are required")
		return
	}
	if err := engines.ExecuteBinReplenishment(tenantID, req.FromBinCode, req.ToBinCode, req.Sku, req.Qty, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleWaveAssign(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		WaveID  string   `json:"wave_id"`
		TaskIDs []string `json:"task_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WaveID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'wave_id' is required")
		return
	}
	tagged, err := engines.AssignTasksToWave(tenantID, req.WaveID, req.TaskIDs, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"tagged": tagged})
}

func handleWavePickList(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	waveID := r.URL.Query().Get("wave_id")
	if waveID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'wave_id' is required")
		return
	}
	pickLines, allocations, err := engines.GenerateWavePickList(tenantID, waveID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if pickLines == nil {
		pickLines = []engines.WavePickLine{}
	}
	if allocations == nil {
		allocations = []engines.WaveOrderAllocation{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"pick_lines": pickLines, "allocations": allocations})
}

func handleCartonizationSuggest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		CartonType string                       `json:"carton_type"`
		Items      []engines.CartonizationItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CartonType == "" || len(req.Items) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'carton_type' and a non-empty 'items' array are required")
		return
	}
	boxes, err := engines.SuggestCartonization(tenantID, req.CartonType, req.Items)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(boxes)
}

func handleABCCycleCountPlan(w http.ResponseWriter, r *http.Request) {
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
	tierA := queryIntOrDefault(r, "tier_a_days", 30)
	tierB := queryIntOrDefault(r, "tier_b_days", 60)
	tierC := queryIntOrDefault(r, "tier_c_days", 90)
	plan, err := engines.GetABCCycleCountPlan(tenantID, locationCode, tierA, tierB, tierC)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if plan == nil {
		plan = []engines.ABCCycleCountSuggestion{}
	}
	_ = json.NewEncoder(w).Encode(plan)
}

func queryIntOrDefault(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func handleRequestRecount(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LineID string `json:"line_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LineID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'line_id' is required")
		return
	}
	newLineID, err := engines.RequestRecount(tenantID, req.LineID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"new_line_id": newLineID})
}

func handleSubmitRecountValue(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LineID     string  `json:"line_id"`
		CountedQty float64 `json:"counted_qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LineID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'line_id' is required")
		return
	}
	if err := engines.SubmitRecountValue(tenantID, req.LineID, req.CountedQty, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleSetCycleCountVarianceReason(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LineID     string `json:"line_id"`
		ReasonCode string `json:"reason_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LineID == "" || req.ReasonCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'line_id' and 'reason_code' are required")
		return
	}
	if err := engines.SetCycleCountVarianceReason(tenantID, req.LineID, req.ReasonCode, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleRetryCycleCountPost lets an admin retry PostCycleCountAdjustment
// after fixing whatever it rejected (most commonly: a missing variance
// reason code) on a line that already reached Approved status via the
// normal maker-checker decision but failed to post - without this, that
// line would be stuck Approved-but-unposted with no way to finish it short
// of direct DB surgery.
func handleRetryCycleCountPost(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		LineID string `json:"line_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LineID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'line_id' is required")
		return
	}
	if err := engines.PostCycleCountAdjustment(tenantID, req.LineID, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "posted"})
}
