package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"custom_erp/engines"
)

// handleFieldFormats serves the field-format declarations (Stage 40.2).
//
// The frontend reads this once at boot to decide, per input, what to show as
// a placeholder, what hint to put under it, which keystrokes to refuse, and
// whether to upper-case as you type. Serving it rather than hardcoding the
// same table in app.js is the point: the server is what actually rejects a
// malformed value, so the guidance the user sees has to come from the same
// declarations, or the two drift and the form starts promising a format the
// server does not accept.
//
// Open to any authenticated role. Nothing sensitive here - a description of
// what a GSTIN looks like. Stage 76.1/76.4 made it follow the tenant's home
// country (its tax-ID rule and its state list), so the cache is short: a
// changed Home country shows on the next sign-in, not an hour later.
func handleFieldFormats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, max-age=300")
	country := engines.TenantPhoneCountry(r.Header.Get("Resolved-Tenant-ID"))
	_ = json.NewEncoder(w).Encode(engines.FieldFormatSpecsFor(country))
}

// handleMasterCodeSeries (Stage 76.5) tells a master's New form how its code
// will look: the confirmed format, or the suggestion to offer the first time
// - "V", 5 digits, next V00001. Read-only; the format itself is confirmed by
// the first record's own save (engines.PrepareMasterNumber), which already
// carries that doctype's write permission.
// handleMasterCodeClean (Stage 93.11) is the guided clean-up of a master's
// old long codes: GET previews old -> new under the confirmed format (or the
// one asked for in ?prefix=&length=, else the suggestion); POST, with the
// same format in the body, renumbers them and rewrites every reference.
// Admin only - it changes the key other records point at. Stage 76.6: every
// master, ?detail=1 adds each column it rewrites, and POST needs a backup
// from the last 24 hours (and acknowledge_warning for Item).
func handleMasterCodeClean(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	tenantID, doctype := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("doctype")
	var req struct {
		Prefix string `json:"prefix"`
		Length int    `json:"length"`
		// Stage 76.6: the admin read the warning for codes known outside
		// the ERP (item stickers, channel SKUs).
		AcknowledgeWarning bool `json:"acknowledge_warning"`
	}
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
			return
		}
	} else {
		req.Prefix = r.URL.Query().Get("prefix")
		fmt.Sscanf(r.URL.Query().Get("length"), "%d", &req.Length)
	}
	format := engines.NewMasterCodeFormat(req.Prefix, req.Length)
	var plan engines.MasterRecodePlan
	var err error
	if r.Method == http.MethodPost {
		plan, err = engines.ApplyMasterRecode(tenantID, doctype, format, req.AcknowledgeWarning, r.Header.Get("Resolved-Username"))
	} else if r.URL.Query().Get("detail") == "1" {
		plan, err = engines.PlanMasterRecodeWithTouches(tenantID, doctype, format) // 76.6: every column it rewrites
	} else {
		plan, err = engines.PlanMasterRecode(tenantID, doctype, format)
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(plan)
}

// handleLocationCodeChange (Stage 123.3) gives a store the code typed on its
// Edit form: GET previews ?id=&code= (references, the backup check; &detail=1
// each column it rewrites), POST {id, code} renames the store and every
// reference in one transaction. Admin only, as the clean-up above.
func handleLocationCodeChange(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	var req struct {
		ID   string `json:"id"`
		Code string `json:"code"`
	}
	var ch engines.LocationCodeChange
	var err error
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
			return
		}
		ch, err = engines.ApplyLocationCodeChange(tenantID, req.ID, req.Code, r.Header.Get("Resolved-Username"))
	} else {
		q := r.URL.Query()
		ch, err = engines.PlanLocationCodeChange(tenantID, q.Get("id"), q.Get("code"), q.Get("detail") == "1")
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(ch)
}

func handleMasterCodeSeries(w http.ResponseWriter, r *http.Request) {
	series, err := engines.GetMasterCodeSeries(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("doctype"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(series)
}
