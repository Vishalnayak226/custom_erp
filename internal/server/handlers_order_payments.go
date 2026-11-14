package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 73.4.2.3: one payment move on an order - capture, refund, chargeback,
// chargeback_won or chargeback_lost. Money moves, so it needs SalesOrder update.
func handleRecordOrderPayment(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "SalesOrder", "update"); err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var in engines.OrderPaymentInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	order, err := engines.RecordOrderPayment(tenantID, r.PathValue("id"), in, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(order)
}
