package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"custom_erp/engines"
)

// Stage 28.1: the module-by-module admin Settings screen's backend. GET
// returns every registered SettingDefinition plus this tenant's current
// effective value (the frontend groups them by module); PUT applies a batch of
// {key: value} overrides. HR/Admin only, the same gate every other admin
// configuration endpoint uses (requireHRAdmin). There is deliberately no
// per-setting create/delete endpoint - engines/settings_registry.go is the
// single source of truth for which settings exist; a row in system_settings is
// only ever an override of a registered default.

func handleGetSettings(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	_ = json.NewEncoder(w).Encode(engines.ListSettingsWithValues(tenantID))
}

func handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	actorUsername := r.Header.Get("Resolved-Username")

	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if len(updates) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "No settings provided")
		return
	}

	// Validate the whole batch before writing anything, so one invalid value
	// never leaves a half-applied set. The message is a specific, user-safe
	// string ("Point expiry must be at least 1"), preserved verbatim by
	// writeAPIErrorGeneric (a 422 keeps the caller's own message).
	for key, value := range updates {
		if err := engines.ValidateSetting(key, value); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}

	applied := 0
	for key, value := range updates {
		if err := engines.SetSetting(tenantID, key, value, actorUsername); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		applied++
	}

	engines.LogAuditEvent(tenantID, actorUsername, "SETTINGS", "UPDATED", fmt.Sprintf("Updated %d configuration setting(s)", applied))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "updated": applied})
}
