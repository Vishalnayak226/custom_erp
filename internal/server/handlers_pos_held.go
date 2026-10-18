package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 73.2.5 - hold and resume a bill at the till (engines/pos_held_bills.go).
// The cashier is the caller's resolved identity, never the request body.

// handleHoldPOSBill - POST /api/v1/pos/held-bills {cart_number, location,
// customer_id, coupon_codes, lines:[{sku,name,qty,unit_price}], total, note}.
func handleHoldPOSBill(w http.ResponseWriter, r *http.Request) {
	var req engines.HeldBill
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request")
		return
	}
	bill, err := engines.HoldPOSBill(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-User-ID"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(bill)
}

// handleListHeldPOSBills - GET /api/v1/pos/held-bills?location=
func handleListHeldPOSBills(w http.ResponseWriter, r *http.Request) {
	bills, err := engines.ListHeldPOSBills(r.Header.Get("Resolved-Tenant-ID"), strings.TrimSpace(r.URL.Query().Get("location")))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"bills": bills})
}

// handleHeldPOSBillAction - POST /api/v1/pos/held-bills/{id}/{action}
// {location, reason}: action is resume or discard.
func handleHeldPOSBillAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Location string `json:"location"`
		Reason   string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	tenantID, cashier, id := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), r.PathValue("id")
	switch r.PathValue("action") {
	case "resume":
		bill, err := engines.ResumePOSBill(tenantID, id, strings.TrimSpace(req.Location), cashier)
		if err != nil {
			writeEngineError(w, r, err, http.StatusConflict)
			return
		}
		_ = json.NewEncoder(w).Encode(bill)
	case "discard":
		if err := engines.DiscardPOSBill(tenantID, id, strings.TrimSpace(req.Location), cashier, req.Reason); err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Discarded"})
	default:
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown action - use resume or discard")
	}
}

// handleStartPOSExchange - POST /api/v1/pos/exchange/start (Stage 73.2.6)
// {original_order_id, location, lines:[{sku,qty}], dispositions, idempotency_key}:
// takes the returned items back at the counter and answers the credit the new
// sale is paid with (engines.StartTillExchange).
func handleStartPOSExchange(w http.ResponseWriter, r *http.Request) {
	var req engines.ExchangeReturnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request")
		return
	}
	credit, err := engines.StartTillExchange(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(credit)
}
