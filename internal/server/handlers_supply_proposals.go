package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
)

// Stage 73.2.18: the planner's supply proposals - generate from reorder or
// MRP suggestions, list, approve into a Draft PO / transfer / production
// order, or reject with a reason.

func handleGenerateSupplyProposals(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "SupplyProposal", "update") {
		return
	}
	var req struct {
		Location     string `json:"location"`
		Source       string `json:"source"`
		LeadTimeDays int    `json:"lead_time_days"`
		SafetyStock  int    `json:"safety_stock"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if req.LeadTimeDays <= 0 {
		req.LeadTimeDays = 7
	}
	res, err := engines.GenerateSupplyProposals(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"),
		req.Location, req.Source, req.LeadTimeDays, req.SafetyStock)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleListSupplyProposals(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "SupplyProposal", "read") {
		return
	}
	list, err := engines.ListSupplyProposals(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("location"), r.URL.Query().Get("status"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

func handleApproveSupplyProposals(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "SupplyProposal", "update") {
		return
	}
	var in engines.ApproveProposalsInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	target := map[string]string{engines.ProposalPurchase: "PurchaseOrder", engines.ProposalTransfer: "TransferOrder", engines.ProposalProduction: "ProductionOrder"}[in.Action]
	if target != "" && !rfqAllowed(w, r, target, "create") {
		return
	}
	res, err := engines.ApproveSupplyProposals(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Location"), in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleRejectSupplyProposals(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "SupplyProposal", "update") {
		return
	}
	var req struct {
		IDs    []string `json:"ids"`
		Reason string   `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if err := engines.RejectSupplyProposals(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), req.Reason, req.IDs); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "rejected", "count": strconv.Itoa(len(req.IDs))})
}
