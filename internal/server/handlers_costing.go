package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 37.3.2: Landed Cost Voucher endpoints. Read/list use the generic
// document API (LandedCostVoucher is a registered doctype); creation and
// applying need dedicated engine logic the generic endpoints can't express,
// the same JournalVoucher/IntercompanyTransaction split.

func handleCreateLandedCostVoucher(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		GRNReference string                   `json:"grn_reference"`
		ChargeLines  []map[string]interface{} `json:"charge_lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	id, err := engines.CreateLandedCostVoucher(tenantID, req.GRNReference, req.ChargeLines, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Draft"})
}

func handleApplyLandedCostVoucher(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id := r.PathValue("id")
	if err := engines.ApplyLandedCostVoucher(tenantID, id, userID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Applied"})
}
