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

// handleBillChargeEntries books a carrier's bill against its estimates (Stage 148.8).
func handleBillChargeEntries(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "VendorInvoice", "create", "You do not have permission to book vendor bills.") {
		return
	}
	var in engines.ChargeBillInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	res, err := engines.BillChargeEntries(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// handleGRNChargeLines is each received line's charges, or why it has none (Stage 148.8).
func handleGRNChargeLines(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "read", "") {
		return
	}
	lines, err := engines.GRNChargeLines(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"lines": lines})
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
