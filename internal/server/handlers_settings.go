package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

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

// Stage 132.15: a role holding admin.settings without full access (Setup
// Operator) sees and changes only the business settings
// (engines.SettingIsAdminOnly).
func handleGetSettings(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireAdminCapability(w, r, "admin.settings") {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	all := engines.ListSettingsWithValues(tenantID)
	if !engines.HasFullAccess(role) {
		business := all[:0]
		for _, s := range all {
			if !engines.SettingIsAdminOnly(s.Key) {
				business = append(business, s)
			}
		}
		all = business
	}
	_ = json.NewEncoder(w).Encode(all)
}

func handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireAdminCapability(w, r, "admin.settings") {
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
	if !engines.HasFullAccess(role) {
		var refused []string
		for key := range updates {
			if engines.SettingIsAdminOnly(key) {
				refused = append(refused, key)
			}
		}
		if len(refused) > 0 {
			sort.Strings(refused)
			writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only an administrator changes security and platform settings: "+strings.Join(refused, ", "))
			return
		}
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
	var changed []string
	for key, value := range updates {
		if err := engines.SetSetting(tenantID, key, value, actorUsername); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		applied++
		// 47.18.3: the audit line names what changed, so a retention hold (a
		// setting at 0) is on record as such, not as "1 setting".
		if len(value) > 60 {
			value = value[:60] + "..."
		}
		changed = append(changed, key+"="+value)
	}
	sort.Strings(changed)

	engines.LogAuditEventCtx(r.Context(), tenantID, actorUsername, "SETTINGS", "UPDATED", fmt.Sprintf("Updated %d configuration setting(s): %s", applied, strings.Join(changed, ", ")))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "updated": applied})
}
