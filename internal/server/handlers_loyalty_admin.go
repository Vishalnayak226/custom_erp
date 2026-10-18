package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 73.2.11 - manual loyalty point adjustments (engines/loyalty_admin.go).
// Raised by a Store Manager or HR/Admin; at or above the approval value they
// wait for another manager under Approvals.

// handleLoyaltyAdjustments - POST /api/v1/loyalty/adjustments
// {customer_id, points (+ credit / - debit), reason_code, note}, and
// GET /api/v1/loyalty/adjustments?customer_id= .
func handleLoyaltyAdjustments(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method == http.MethodGet {
		list, err := engines.ListLoyaltyAdjustments(tenantID, strings.TrimSpace(r.URL.Query().Get("customer_id")))
		if err != nil {
			writeEngineError(w, r, err, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"adjustments": list})
		return
	}
	if !requireStoreManager(w, r) {
		return
	}
	var req struct {
		CustomerID string `json:"customer_id"`
		Points     int    `json:"points"`
		ReasonCode string `json:"reason_code"`
		Note       string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request")
		return
	}
	adj, err := engines.RequestLoyaltyAdjustment(tenantID, req.CustomerID, req.Points, req.ReasonCode, req.Note,
		r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(adj)
}

// handleLoyaltyVoucherOverview - GET /api/v1/loyalty/vouchers (Stage 55.17):
// every voucher in words - what it gives, for whom, where, its dates, whether
// it can be used now and why not, and what it has taken off paid bills.
func handleLoyaltyVoucherOverview(w http.ResponseWriter, r *http.Request) {
	list, err := engines.ListVoucherOverview(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"vouchers": list})
}

// handleLoyaltyCampaignOverview - GET /api/v1/loyalty/campaigns (Stage
// 55.17): every campaign with who it would pick today and what it has brought
// in so far.
func handleLoyaltyCampaignOverview(w http.ResponseWriter, r *http.Request) {
	list, err := engines.ListCampaignOverview(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"campaigns": list})
}
