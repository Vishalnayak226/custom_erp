package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 73.2.7 - till cash movements, shift handover and the drawer's
// position, for the cashier at the till. The cashier is always the caller's
// resolved identity, never the request body (same rule as session open/close).

// handlePOSCashMovement - POST /api/v1/pos/session/cash-movement
// {session_id, movement_type: Paid In|Paid Out|Safe Drop, amount, reason_code, reference}.
func handlePOSCashMovement(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID    string  `json:"session_id"`
		MovementType string  `json:"movement_type"`
		Amount       float64 `json:"amount"`
		ReasonCode   string  `json:"reason_code"`
		Reference    string  `json:"reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	id, err := engines.RecordPOSCashMovement(tenantID, req.SessionID, r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-User-ID"),
		strings.TrimSpace(req.MovementType), req.Amount, strings.TrimSpace(req.ReasonCode), req.Reference)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	pos, _ := engines.SessionCashPosition(tenantID, req.SessionID)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"movement_id": id, "position": pos})
}

// handlePOSSessionTakeover - POST /api/v1/pos/session/takeover
// {location, counted_cash, variance_reason}: the next cashier takes the till.
func handlePOSSessionTakeover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Location       string  `json:"location"`
		CountedCash    float64 `json:"counted_cash"`
		VarianceReason string  `json:"variance_reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Location) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'location' is required")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	res, newID, err := engines.TakeOverPOSSession(tenantID, strings.TrimSpace(req.Location), r.Header.Get("Resolved-Username"),
		r.Header.Get("Resolved-User-ID"), req.CountedCash, req.VarianceReason)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"previous": res, "session_id": newID, "status": "Open"})
}

// handlePOSSessionSummary - GET /api/v1/pos/session/summary?session_id=
// the drawer's opening float, cash sales, refunds, movements and expected cash.
func handlePOSSessionSummary(w http.ResponseWriter, r *http.Request) {
	pos, err := engines.SessionCashPosition(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("session_id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(pos)
}
