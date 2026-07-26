package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 26.6.4: Journal Voucher endpoints. A new file (not
// handlers_finance_maturity.go) purely to avoid colliding with a concurrent
// session's in-flight edits elsewhere in that area this same day - no other
// reason for the split. Submit-for-approval/approve/reject reuse the
// existing generic /api/v1/approval/submit|decide endpoints (doctype
// "JournalVoucher") - no new endpoint needed for that part.

func handleCreateJournalVoucher(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		VoucherDate string                       `json:"voucher_date"`
		Narration   string                       `json:"narration"`
		Lines       []engines.JournalVoucherLine `json:"lines"`
		CostCenter  string                       `json:"cost_center"`
		Department  string                       `json:"department"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	voucherID, err := engines.CreateJournalVoucher(tenantID, req.VoucherDate, req.Narration, req.Lines, userID,
		engines.JournalVoucherOptions{CostCenter: req.CostCenter, Department: req.Department})
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": voucherID, "status": "Draft"})
}

func handleReverseJournalVoucher(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	voucherID := r.PathValue("id")
	newID, err := engines.ReverseJournalVoucher(tenantID, voucherID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"reversal_id": newID, "status": "Draft"})
}

func handleGeneratePaymentFile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	proposalID := r.PathValue("id")
	csvText, err := engines.GeneratePaymentFile(tenantID, proposalID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": proposalID, "csv": csvText})
}

func handleRecordPaymentUTR(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	proposalID := r.PathValue("id")
	var req struct {
		InvoiceID string `json:"invoice_id"`
		UTR       string `json:"utr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InvoiceID == "" || req.UTR == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'invoice_id' and 'utr' are required")
		return
	}
	if err := engines.RecordPaymentUTR(tenantID, proposalID, req.InvoiceID, req.UTR); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": proposalID, "status": "recorded"})
}

func handleListPaymentUTRs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	proposalID := r.PathValue("id")
	utrs, err := engines.ListPaymentUTRs(tenantID, proposalID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(utrs)
}

func handleRetryPostJournalVoucher(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	voucherID := r.PathValue("id")
	if err := engines.RetryPostApprovedJournalVoucher(tenantID, voucherID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": voucherID, "status": "Posted"})
}

func handleCreateRecurringJournalTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Narration   string                       `json:"narration"`
		Frequency   string                       `json:"recurring_frequency"`
		NextRunDate string                       `json:"next_run_date"`
		Lines       []engines.JournalVoucherLine `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	voucherID, err := engines.CreateRecurringJournalTemplate(tenantID, req.Narration, req.Frequency, req.NextRunDate, req.Lines, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": voucherID, "status": "Recurring Template"})
}
