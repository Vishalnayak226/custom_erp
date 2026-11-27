package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
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

// 297.3: the signed-in user's own long tasks, for the top bar's tasks list.
// A tab calls this every few seconds only while a task of its own runs.
func handleMyTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := engines.ListMyTasks(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"tasks": tasks})
}

// handleCancelMyTask stops one of the caller's own tasks: a queued or running
// job they started, or a report export (cancelled while queued, revoked
// once run). Anyone else's task answers not found.
func handleCancelMyTask(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	kind, id, _ := strings.Cut(r.PathValue("id"), ":")
	var err error
	status := "cancelled"
	switch kind {
	case "job":
		err = engines.CancelMyJob(tenantID, userID, id)
	case "export":
		status, err = engines.CancelReportExportJob(tenantID, id, userID, "")
		status = strings.ToLower(status)
	default:
		err = engines.ErrMyTaskNotFound
	}
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}
