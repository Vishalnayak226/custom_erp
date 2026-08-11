package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 41: the two self-service reads behind the product's setup guidance.
//
// Neither is admin-gated, for the same reason handleMyPermissions is not: the
// answers change what every screen renders for the signed-in user, so gating
// them would only mean a non-admin's screens render without the guidance they
// most need. Neither exposes any record content - one returns counts, the
// other returns configuration the user is about to be validated against.

// handleSetupStatus returns how many records exist for every Master record
// type, so the client can tell "this module is not set up yet" from "this
// module is set up and you just need to pick something" without a query per
// screen.
func handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	entries, err := engines.GetSetupStatus(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"masters": entries})
}

// handleGetLocalization returns the tenant's configured home country and its
// phone rule, plus the full country list.
//
// The point of shipping the rule to the client rather than only validating
// server-side is that "restricted to 10 digits" should be something the user
// experiences while typing - a maxlength and an inline hint - not something
// they discover from a rejection after filling in a whole form. The server
// still enforces it; this makes the client agree with the server instead of
// guessing.
func handleGetLocalization(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	_ = json.NewEncoder(w).Encode(engines.GetLocalizationInfo(tenantID))
}
