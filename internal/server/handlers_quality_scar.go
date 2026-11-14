package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
)

// 170.16 supplier corrective action requests, 170.15 AQL sample plan preview.

func handleOpenSCAR(w http.ResponseWriter, r *http.Request) {
	var in engines.SCARInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !rfqAllowed(w, r, "SupplierCAR", "create") {
		return
	}
	id, err := engines.OpenSCAR(r.Header.Get("Resolved-Tenant-ID"), in, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": engines.ScarOpen})
}

func handleAdvanceSCAR(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Step string `json:"step"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if !rfqAllowed(w, r, "SupplierCAR", "update") {
		return
	}
	id := r.PathValue("id")
	if err := engines.AdvanceSCAR(r.Header.Get("Resolved-Tenant-ID"), id, req.Step, req.Text, r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

func handleAQLPlan(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("lot_size"))
	p, err := engines.AQLSamplePlan(n, r.URL.Query().Get("aql"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(p)
}
