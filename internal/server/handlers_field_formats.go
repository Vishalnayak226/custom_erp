package server

import (
	"encoding/json"
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
func handleMasterCodeSeries(w http.ResponseWriter, r *http.Request) {
	series, err := engines.GetMasterCodeSeries(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("doctype"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(series)
}
