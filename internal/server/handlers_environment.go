package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"os"
)

// Stage 47.0.4/47.0.5 - the supported-configuration/environment banner the
// UI (public/app.js) reads to show whether this instance delivers real
// external side effects (email/webhook/ops-alert) or simulates them. No
// extra role gate beyond apiMiddleware, matching handleListJobs: this is
// read-only, non-sensitive server posture, not tenant business data.
func handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	env := os.Getenv("ENV")
	if env == "" {
		env = "development"
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"env":                            env,
		"external_side_effects":          engines.ExternalSideEffectsEnabled(),
		"banner":                         engines.EnvironmentBanner(),
		"supported_configuration_notice": engines.SupportedConfigurationNotice(),
	})
}
