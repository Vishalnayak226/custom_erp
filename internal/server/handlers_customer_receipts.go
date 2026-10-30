package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// 140.12.5 / 140.12.6: back-office receipts - one voucher across a customer's
// invoices, with the bank account, TDS, the customer's advances and an
// on-account remainder (engines/customer_receipt.go).
func handleCustomerReceipts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method == http.MethodGet {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list, err := engines.ListCustomerReceipts(tenantID, r.URL.Query().Get("customer"), limit)
		if err != nil {
			writeEngineError(w, r, err, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(list)
		return
	}
	var in engines.CustomerReceiptInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid receipt")
		return
	}
	if strings.TrimSpace(in.Location) == "" {
		in.Location = r.Header.Get("Resolved-Location")
	}
	rec, err := engines.RecordCustomerReceipt(tenantID, r.Header.Get("Resolved-Username"), in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(rec)
}

// handleCancelSalesInvoice (140.12.8) cancels an invoice that took no money,
// reversing what it posted.
func handleCancelSalesInvoice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	id := r.PathValue("id")
	if err := engines.CancelSalesInvoice(r.Header.Get("Resolved-Tenant-ID"), id, r.Header.Get("Resolved-User-ID"), body.Reason); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "Cancelled", "id": id})
}

// handleMatchAcquirerSettlements (140.12.12) matches imported card / UPI
// settlement rows to their bank credits now and returns what is left over.
func handleMatchAcquirerSettlements(w http.ResponseWriter, r *http.Request) {
	res, err := engines.MatchAcquirerSettlements(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// handleCustomerOpenItems is what a new receipt can be allocated to.
func handleCustomerOpenItems(w http.ResponseWriter, r *http.Request) {
	customer := strings.TrimSpace(r.URL.Query().Get("customer"))
	if customer == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'customer' is required")
		return
	}
	invoices, advances, err := engines.CustomerOpenItems(r.Header.Get("Resolved-Tenant-ID"), customer)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"invoices": invoices, "advances": advances, "modes": engines.ReceiptModes})
}
