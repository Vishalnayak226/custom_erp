package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"time"
)

// Stage 55.14: GET /api/v1/hr/leave-balance?employee_id=&year= - the
// balance a leave form explains. Scoped in the engine: a user without HR's
// self scope reads only their own employee's balance.
func handleLeaveBalance(w http.ResponseWriter, r *http.Request) {
	year := r.URL.Query().Get("year")
	if year == "" {
		year = time.Now().Format("2006")
	}
	session := engines.SessionScope{
		Role: r.Header.Get("Resolved-Role"), UserID: r.Header.Get("Resolved-User-ID"),
		LocationCode: r.Header.Get("Resolved-Location"),
	}
	lines, err := engines.GetLeaveBalance(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("employee_id"), year, session)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"year": year, "balances": lines})
}

// Stage 55.16: GET /api/v1/assets/{id}/lifecycle - one asset's source,
// schedule, GL lines and transfers.
func handleAssetLifecycle(w http.ResponseWriter, r *http.Request) {
	lc, err := engines.GetAssetLifecycle(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(lc)
}
