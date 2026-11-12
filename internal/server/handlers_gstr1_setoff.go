package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 148.23 / 148.25 handlers: period hold, GSTR-1 filed months, set-off.
// Each command checks the finance role (the period-close predicate); unmarking
// a filed month is for full access only.

func financeCaller(w http.ResponseWriter, r *http.Request) (tenantID, userID string, ok bool) {
	if !engines.CanManagePeriodClose(r.Header.Get("Resolved-Role")) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return "", "", false
	}
	return r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), true
}

func handleHoldAccountingPeriod(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := financeCaller(w, r)
	if !ok {
		return
	}
	if err := engines.SetAccountingPeriodHold(tenantID, userID, r.PathValue("id"), true); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "On Hold"})
}

func handleReleaseAccountingPeriod(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := financeCaller(w, r)
	if !ok {
		return
	}
	if err := engines.SetAccountingPeriodHold(tenantID, userID, r.PathValue("id"), false); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Open"})
}

func handleGSTR1FiledMonths(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := financeCaller(w, r)
	if !ok {
		return
	}
	list, err := engines.ListGSTR1FiledMonths(tenantID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"months": list})
}

func handleGSTR1MarkFiled(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := financeCaller(w, r)
	if !ok {
		return
	}
	if err := engines.MarkGSTR1Filed(tenantID, userID, r.PathValue("month")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "filed"})
}

func handleGSTR1Unmark(w http.ResponseWriter, r *http.Request) {
	if !engines.HasFullAccess(r.Header.Get("Resolved-Role")) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := engines.UnmarkGSTR1Filed(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.PathValue("month"), req.Reason); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "unmarked"})
}

func handleSetOffSuggestions(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := financeCaller(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("enabled_only") == "1" { // the Finance page's link check; skips the party scan
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"enabled": engines.SetOffEnabled(tenantID)})
		return
	}
	list, err := engines.PartyPairSuggestions(tenantID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"suggestions": list})
}

func handleSetOffBalances(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := financeCaller(w, r)
	if !ok {
		return
	}
	b, err := engines.GetSetOffBalances(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(b)
}

func handleSetOffStatement(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := financeCaller(w, r)
	if !ok {
		return
	}
	s, err := engines.GetSetOffStatement(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(s)
}

func handleSetOffReverse(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := financeCaller(w, r)
	if !ok {
		return
	}
	var req struct {
		Date string `json:"date"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := engines.ReverseSetOff(tenantID, userID, r.PathValue("id"), strings.TrimSpace(req.Date)); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Reversed"})
}
