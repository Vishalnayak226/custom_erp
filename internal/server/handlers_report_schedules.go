package server

import (
	"context"
	"custom_erp/engines"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// Stage 55.20: the Schedules tab and a report's recent runs
// (engines/report_schedule_view.go). Schedules are still created through the
// generic doc API; these routes only read them and pause, resume or run one.

// handleListReportSchedules lists the caller's schedules and digests (a Super
// Admin's: everyone's).
func handleListReportSchedules(w http.ResponseWriter, r *http.Request) {
	list, err := engines.ListReportSchedules(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"schedules": list})
}

// handleReportScheduleAction is pause, resume or run on one schedule. Run is
// claimed here (so "already running" comes back to the caller) and delivered
// in the background - a report can outlast the request deadline.
func handleReportScheduleAction(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role")
	kind, id := r.PathValue("kind"), r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "pause", "resume":
		paused := r.PathValue("action") == "pause"
		if err = engines.SetReportSchedulePaused(tenantID, kind, id, userID, role, paused); err == nil {
			status := "Active"
			if paused {
				status = "Paused"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
			return
		}
	case "run":
		var run *engines.ReportScheduleRun
		if run, err = engines.ClaimReportScheduleRun(tenantID, kind, id, userID, role); err == nil {
			go run.Run(context.Background())
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "Running"})
			return
		}
	default:
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "unknown schedule action")
		return
	}
	var busy *engines.ReportScheduleBusyError
	switch {
	case errors.Is(err, engines.ErrReportScheduleNotFound):
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
	case errors.As(err, &busy):
		writeAPIErrorGeneric(w, r, http.StatusConflict, err.Error())
	default:
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
	}
}

// handleRecentReportRuns is one report's recent runs - the caller's own, or
// everyone's for a Super Admin.
func handleRecentReportRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := engines.ListRecentReportRuns(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"),
		r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"), limit)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"runs": runs})
}
