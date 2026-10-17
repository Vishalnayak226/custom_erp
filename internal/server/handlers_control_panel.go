package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// handleControlPanel (Stage 65.5) is the platform operator's one page: every
// client with its status, address, modules, version, users, activity and
// errors, the release ledger and the last backup, plus the running build.
// Read-only; platform operators only, like every other cross-client endpoint
// (Stage 60.8).
func handleControlPanel(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	cp, err := engines.GetControlPanel()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to build the control panel")
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		engines.ControlPanel
		Build map[string]string `json:"build"`
	}{cp, map[string]string{
		"version":      currentAppVersion(),
		"git_commit":   gitCommit,
		"build_time":   buildTime,
		"release_date": releaseDate(),
	}})
}
