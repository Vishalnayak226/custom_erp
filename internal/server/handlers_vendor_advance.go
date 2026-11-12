package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 148.15 / 148.17: vendor advances and volume rebate claims. Lists and
// single documents are read through the generic /api/v1/doc routes; these
// routes are the actions, which post money.

func handleGiveVendorAdvance(w http.ResponseWriter, r *http.Request) {
	var req engines.VendorAdvanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'vendor_id' and 'amount' are required")
		return
	}
	res, err := engines.GiveVendorAdvance(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleApplyVendorAdvance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InvoiceID string  `json:"invoice_id"`
		Amount    float64 `json:"amount"`
		Reference string  `json:"reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.InvoiceID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'invoice_id' is required")
		return
	}
	res, err := engines.ApplyVendorAdvance(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.PathValue("id"), strings.TrimSpace(req.InvoiceID), req.Amount, req.Reference)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleRefundVendorAdvance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount    float64 `json:"amount"`
		Reference string  `json:"reference"`
		Date      string  `json:"date"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	res, err := engines.RefundVendorAdvance(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.PathValue("id"), req.Amount, req.Reference, strings.TrimSpace(req.Date))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleCancelVendorAdvance(w http.ResponseWriter, r *http.Request) {
	res, err := engines.CancelVendorAdvance(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleCalculateVendorRebates(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From     string `json:"from"`
		To       string `json:"to"`
		VendorID string `json:"vendor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'from' and 'to' are required")
		return
	}
	res, err := engines.CalculateVendorRebates(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), strings.TrimSpace(req.From), strings.TrimSpace(req.To), req.VendorID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"claims": res})
}

func handleSubmitVendorRebateClaim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := engines.SubmitVendorRebateClaim(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"), id); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Pending Approval"})
}

func handleSettleVendorRebateClaim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	note, err := engines.SettleVendorRebateClaim(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), id)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Settled", "debit_note_id": note})
}
