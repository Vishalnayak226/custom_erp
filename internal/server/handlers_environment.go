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
	// Stage 47.12.6: a sandbox client reads as a simulated environment named
	// "SANDBOX" - the shell's existing banner then says so, with no new UI.
	sideEffects := engines.ExternalSideEffectsEnabled()
	sandbox, _ := engines.IsSandboxTenant(r.Header.Get("Resolved-Tenant-ID"))
	banner := engines.EnvironmentBanner()
	if sandbox {
		env, sideEffects = "SANDBOX", false
		banner = "SANDBOX - nothing leaves the server"
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"env":                            env,
		"sandbox":                        sandbox,
		"external_side_effects":          sideEffects,
		"banner":                         banner,
		"supported_configuration_notice": engines.SupportedConfigurationNotice(),
	})
}
