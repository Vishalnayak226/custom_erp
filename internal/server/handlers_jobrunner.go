package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 38.6 - the async job runner's visibility/control surface. No extra
// role gate beyond apiMiddleware, matching handleGetIntegrationLogs/
// handleRetryIntegrationEvent (internal/server/handlers_integrations_admin.go)
// exactly, since this is a 4th pane on that same Activity Log screen.

func handleListJobs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	jobs, err := engines.ListJobs(tenantID, r.URL.Query().Get("status"), r.URL.Query().Get("job_type"), 0)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"jobs": jobs})
}

func handleRetryJob(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := engines.ReplayJob(tenantID, r.PathValue("id")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "queued_for_retry"})
}

func handleCancelJob(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := engines.CancelJob(tenantID, r.PathValue("id")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}
