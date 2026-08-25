package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 35.8: Settlement/payment reconciliation ("UniReco" gap). Import
// itself reuses the existing generic POST /api/v1/import/MarketplaceSettlementLine
// path (no new handler needed there - see the migration's own comment);
// these four routes are the matching/dispute/write-off actions on top of it.
// Every handler reads its actor from Resolved-User-ID, the same convention
// handlers_shipping_package.go established for Stage 35.4.

func handleReconcileMarketplaceSettlements(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	result, err := engines.ReconcileMarketplaceSettlements(tenantID, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func handleRaiseSettlementDispute(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	lineID := r.PathValue("id")

	var req struct {
		ReasonCode string `json:"reason_code"`
		Note       string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if err := engines.RaiseSettlementDispute(tenantID, lineID, req.ReasonCode, req.Note, r.Header.Get("Resolved-User-ID")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "disputed"})
}

func handleResolveSettlementDispute(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	lineID := r.PathValue("id")

	var req struct {
		CorrectedGrossAmount float64 `json:"corrected_gross_amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if err := engines.ResolveSettlementDispute(tenantID, lineID, req.CorrectedGrossAmount, r.Header.Get("Resolved-User-ID")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "resolved"})
}

func handleWriteOffSettlementVariance(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	lineID := r.PathValue("id")

	var req struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if err := engines.WriteOffSettlementVariance(tenantID, lineID, req.ReasonCode, r.Header.Get("Resolved-User-ID")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "written_off"})
}
