package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 148.40: read and correct a batch's or serial's attributes (purity,
// karat, weights) with a history. Reading needs read on the piece's doctype;
// correcting needs what a stock adjustment needs (create on InventoryAdjustment).

func pieceDoctypeParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	doctype := r.PathValue("doctype")
	if doctype != "Batch" && doctype != "SerialNumber" {
		writeAPIError(w, r, "PURCHA-5040", "")
		return "", false
	}
	return doctype, true
}

func handleGetPieceAttributes(w http.ResponseWriter, r *http.Request) {
	doctype, ok := pieceDoctypeParam(w, r)
	if !ok {
		return
	}
	allowed, err := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), doctype, "read")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check permissions")
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	view, err := engines.GetPieceAttributes(r.Header.Get("Resolved-Tenant-ID"), doctype, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(view)
}

func handleCorrectPieceAttributes(w http.ResponseWriter, r *http.Request) {
	doctype, ok := pieceDoctypeParam(w, r)
	if !ok || !requireCreate(w, r, engines.InventoryAdjustmentDoctype) {
		return
	}
	var req struct {
		Values map[string]string `json:"values"`
		Reason string            `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	view, err := engines.CorrectPieceAttributes(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), doctype, r.PathValue("id"), req.Values, req.Reason)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(view)
}
