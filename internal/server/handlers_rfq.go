package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 73.2.1-73.2.3: RFQ item lines and invitations, comparable quotes
// (D06) and award to a purchase order, once. The older /api/v1/rfq/quotes and
// /api/v1/rfq/select-quote stay as they were.

func rfqAllowed(w http.ResponseWriter, r *http.Request, doctype, action string) bool {
	allowed, err := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), doctype, action)
	if err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return false
	}
	return true
}

func handleSaveRFQ(w http.ResponseWriter, r *http.Request) {
	var in engines.RFQInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	action := "create"
	if in.ID != "" {
		action = "update"
	}
	if !rfqAllowed(w, r, "RFQ", action) {
		return
	}
	id, err := engines.SaveRFQ(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Location"), in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func handleInviteRFQVendors(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "RFQ", "update") {
		return
	}
	var req struct {
		Vendors []string `json:"vendors"`
		DueDate string   `json:"due_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	inv, err := engines.InviteRFQVendors(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-User-ID"), req.DueDate, req.Vendors)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(inv)
}

func handleDeclineRFQ(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "RFQ", "update") {
		return
	}
	var req struct {
		Vendor string `json:"vendor"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if err := engines.DeclineRFQ(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), req.Vendor, req.Reason, r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "declined"})
}

func handleRecordVendorQuote(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "VendorQuote", "create") {
		return
	}
	var in engines.QuoteInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	in.RFQID = r.PathValue("id")
	id, err := engines.RecordVendorQuote(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Location"), in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"quote_id": id})
}

func handleRFQComparison(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "RFQ", "read") {
		return
	}
	cmp, err := engines.GetRFQComparison(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(cmp)
}

func handleAwardRFQ(w http.ResponseWriter, r *http.Request) {
	if !rfqAllowed(w, r, "PurchaseOrder", "create") {
		return
	}
	var req struct {
		QuoteID string `json:"quote_id"`
		engines.AwardOptions
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.QuoteID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'quote_id' is required")
		return
	}
	res, err := engines.AwardRFQ(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), req.QuoteID,
		r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Location"), req.AwardOptions)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
