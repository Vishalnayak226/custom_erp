package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 73.2.4 (D05): a purchase order's ordered / accepted / rejected / open
// / invoiced / paid view, and short-closing its open balance.

func handlePOReconciliation(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "PurchaseOrder", "read")
	if err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	rec, err := engines.PurchaseOrderReconciliation(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(rec)
}

func handlePOShortClose(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "PurchaseOrder", "update")
	if err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var req struct {
		ReasonCode string `json:"reason_code"`
		Note       string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	res, err := engines.ShortClosePurchaseOrder(tenantID, r.PathValue("id"), r.Header.Get("Resolved-User-ID"),
		req.ReasonCode, req.Note, r.Header.Get("Resolved-Location"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
