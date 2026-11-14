package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Ask 169: carrier claims (73.4.2.6) and freight invoice reconciliation
// (73.4.2.7) on OMS -> Freight. The lists are the generic document API.

func handleRaiseCarrierClaim(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "CarrierClaim", "create"); err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var in struct {
		BookingID string  `json:"booking_id"`
		ClaimType string  `json:"claim_type"`
		Amount    float64 `json:"amount"`
		Notes     string  `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	id, err := engines.RaiseCarrierClaim(tenantID, in.BookingID, in.ClaimType, in.Amount, in.Notes, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func handleMoveCarrierClaim(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "CarrierClaim", "update"); err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var mv engines.CarrierClaimMove
	if err := json.NewDecoder(r.Body).Decode(&mv); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if err := engines.MoveCarrierClaim(tenantID, r.PathValue("id"), mv, r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleReconcileFreightInvoices(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "FreightInvoiceLine", "update"); err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	res, err := engines.ReconcileFreightInvoices(tenantID, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
