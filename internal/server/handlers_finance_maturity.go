package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 20 Track B.3: Finance Maturity handlers (20.25-20.29, 20.32-20.34).
// BankAccount/BankStatementLine/TDSSection/DebitNote/CreditNote creation and
// listing use the existing generic doc API (GET/POST /api/v1/doc/{doctype}),
// same as Vendor/Customer/POSProfile - only the actions the generic CRUD
// endpoint can't express (reconcile, execute a payment run, post a GL
// reversal, ageing/GST reports, the close checklist) get a dedicated handler
// here, matching the rest of this codebase's convention.
//
// Stage 23.9: swept onto writeAPIErrorGeneric (this file was excluded from
// the original Stage 23.4 sweep - a concurrent session was actively writing
// it at the time). No precise catalog codes attached here, matching every
// sibling handlers_*.go file's still-generic treatment of the same
// validation scenarios (e.g. GLOBAL-0001 "Mandatory value missing") - those
// stay generic-fallback everywhere until a dedicated follow-up pass
// re-attaches precise codes across the whole codebase at once, not
// piecemeal in just this one file. Statuses converted from 400 to 422 per
// the now-decided 400-vs-422 convention (docs/micro_checklist.md 23.12).

func handleBankReconcile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		BankAccount string `json:"bank_account"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BankAccount == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'bank_account' is required")
		return
	}
	result, err := engines.ReconcileBankStatement(tenantID, req.BankAccount, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func handlePaymentProposal(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		InvoiceIDs []string `json:"invoice_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.InvoiceIDs) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'invoice_ids' (non-empty array) is required")
		return
	}
	proposalID, total, err := engines.CreatePaymentProposal(tenantID, req.InvoiceIDs, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": proposalID, "total_amount": total, "status": "Draft"})
}

func handleExecutePaymentProposal(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	proposalID := r.PathValue("id")
	results, err := engines.ExecutePaymentProposal(tenantID, proposalID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": proposalID, "results": results})
}

func handlePayVendorInvoiceWithTDS(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		InvoiceID  string `json:"invoice_id"`
		TDSSection string `json:"tds_section"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InvoiceID == "" || req.TDSSection == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'invoice_id' and 'tds_section' are required")
		return
	}
	netPaid, tdsAmount, err := engines.PayVendorInvoiceWithTDS(tenantID, req.InvoiceID, req.TDSSection, userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "Paid", "invoice_id": req.InvoiceID, "net_paid": netPaid, "tds_amount": tdsAmount,
	})
}

func handlePostDebitNote(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	noteID := r.PathValue("id")
	amount, err := engines.PostDebitNote(tenantID, noteID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Posted", "id": noteID, "amount": amount})
}

func handlePostCreditNote(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	noteID := r.PathValue("id")
	amount, err := engines.PostCreditNote(tenantID, noteID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Posted", "id": noteID, "amount": amount})
}

func handlePostSalesInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	invoiceID := r.PathValue("id")
	amount, err := engines.PostSalesInvoice(tenantID, invoiceID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Approved", "id": invoiceID, "amount": amount})
}

func handleSettleSalesInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	invoiceID := r.PathValue("id")
	amount, err := engines.SettleSalesInvoice(tenantID, invoiceID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Paid", "id": invoiceID, "amount": amount})
}

func handleReceivablesAgeingReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	buckets, err := engines.GetReceivablesAgeingReport(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(buckets)
}

func handleGSTReturnSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	startDate := r.URL.Query().Get("start")
	endDate := r.URL.Query().Get("end")
	summary, err := engines.GetGSTReturnSummary(tenantID, startDate, endDate)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(summary)
}

func handlePeriodCloseChecklist(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	periodID := r.PathValue("id")
	checklist, err := engines.GetPeriodCloseChecklist(tenantID, periodID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(checklist)
}
