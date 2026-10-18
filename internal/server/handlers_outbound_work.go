package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
)

// Stage 55.8: the outbound queue and wave-from-selection, and the labour and
// billing explanations (read-only - capture and invoicing keep their own
// endpoints).

func handleOutboundWork(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "FulfillmentTask", "read") {
		return
	}
	out, err := engines.OutboundWork(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("location"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func handleWaveFromSelection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Location string   `json:"location"`
		WaveID   string   `json:"wave_id"`
		TaskIDs  []string `json:"task_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !rfqAllowed(w, r, "FulfillmentTask", "update") {
		return
	}
	out, err := engines.CreateWaveFromSelection(r.Header.Get("Resolved-Tenant-ID"), req.Location, req.WaveID, req.TaskIDs, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func handleLaborStandardPreview(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "LaborStandard", "read") {
		return
	}
	qty, _ := strconv.ParseFloat(r.URL.Query().Get("qty"), 64)
	out, err := engines.ExplainLaborStandard(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("code"), qty)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func handleBillingReview(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "CapturedCharge", "read") {
		return
	}
	q := r.URL.Query()
	out, err := engines.BillingReview(r.Header.Get("Resolved-Tenant-ID"), q.Get("owner"), q.Get("start"), q.Get("end"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
