package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 20 Track B.4 (20.35-20.40): the generic report-builder framework's
// HTTP surface. Saved filters (20.36) deliberately have NO handler here -
// ReportFilterPreset is a plain registered doctype, so creating/listing/
// deleting a preset already works through the existing generic
// GET/POST/DELETE /api/v1/doc/ReportFilterPreset endpoint, same as every
// other master. Only the actions the generic doc API can't express (run a
// report, drill down, queue/poll an async export) get a handler here.

func handleReportCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	catalog := []engines.ReportDefinition{}
	entitlements, err := engines.ListModuleEntitlements(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusServiceUnavailable)
		return
	}
	enabled := map[string]bool{}
	for _, module := range entitlements {
		enabled[module.ModuleKey] = module.Enabled
	}
	for _, def := range engines.ListReportDefinitions() {
		if enabled["reports"] && enabled[def.ModuleKey] {
			catalog = append(catalog, def)
		}
	}
	_ = json.NewEncoder(w).Encode(catalog)
}

func handleRunReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	reportID := r.PathValue("id")
	params := flattenQueryParams(r)
	def, rows, masked, controls, err := engines.RunReportWithControlTotals(r.Context(), tenantID, reportID, role, userID, params)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	resp := map[string]interface{}{
		"id": def.ID, "label": def.Label, "columns": def.Columns, "has_drill_down": def.HasDrillDown, "rows": rows,
	}
	// Stage 73.2.19: detail = summary = ledger, with its cut-off.
	if controls != nil {
		resp["control_totals"] = controls
	}
	// REPORT-0160 (Stage 25.5): "Report no data" is a Warning/non-blocking
	// scenario in the catalog (an empty result from a legitimate filter
	// isn't an error), so it's an annotation on the normal 200 response,
	// not a rejection. Stage 104.1: and so it is not written to the system
	// error log either - it was 24 of the default client's 36 "errors".
	if len(rows) == 0 {
		entry := errorCatalog["REPORT-0160"]
		resp["code"] = "REPORT-0160"
		resp["message"] = entry.UserMessage
	} else if masked {
		// REPORT-0287 (Stage 25.5): "Sensitive column masked" - Info,
		// non-blocking, purely informational for the frontend to show a
		// small "some fields are restricted" note if it wants to.
		entry := errorCatalog["REPORT-0287"]
		resp["code"] = "REPORT-0287"
		resp["message"] = entry.UserMessage
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func handleReportDrillDown(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	reportID := r.PathValue("id")
	rowKey := r.URL.Query().Get("row")
	params := flattenQueryParams(r)
	rows, err := engines.RunReportDrillDownContext(r.Context(), tenantID, reportID, role, rowKey, params, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rows": rows})
}

func handleCreateReportExport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ReportID string            `json:"report_id"`
		Params   map[string]string `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ReportID == "" {
		http.Error(w, "Field 'report_id' is required", http.StatusUnprocessableEntity)
		return
	}
	jobID, err := engines.CreateReportExportJob(tenantID, req.ReportID, role, req.Params, userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	// REPORT-0184 ("Export started", Info/non-blocking) - annotates the
	// same 200 response every caller already gets, not a new status.
	entry := errorCatalog["REPORT-0184"]
	logForEntry(r, entry, entry.UserMessage)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": jobID, "status": "Pending", "code": "REPORT-0184", "message": entry.UserMessage})
}

func handleGetReportExport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	jobID := r.PathValue("id")
	// Stage 55.20: an export is the requester's own - another user gets the
	// same answer as for a job that does not exist.
	if visible, verr := engines.ReportExportJobVisible(tenantID, jobID, r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role")); verr != nil || !visible {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "export job not found")
		return
	}
	status, csvBytes, code, err := engines.GetReportExportJob(tenantID, jobID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	if status == "Completed" && r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+jobID+".csv\"")
		_, _ = w.Write(csvBytes)
		return
	}
	resp := map[string]string{"id": jobID, "status": status}
	if status == "Failed" && code != "" {
		resp["code"] = code
		resp["message"] = errorCatalog[code].UserMessage
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// flattenQueryParams turns ?a=1&b=2 into a plain map, ignoring the "id"
// path segment isn't part of the query string anyway - report Run/DrillDown
// functions only ever read declared param keys, so passing the full query
// set through is safe (no SQL is built directly from arbitrary keys, unlike
// the generic doc list's filter, which allowlists identifiers for exactly
// that reason).
func flattenQueryParams(r *http.Request) map[string]string {
	out := map[string]string{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// Stage 55.20: "My exports" - the caller's own background exports.
func handleListMyReportExports(w http.ResponseWriter, r *http.Request) {
	jobs, err := engines.ListMyReportExportJobs(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), 20)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"jobs": jobs})
}

func handleCancelReportExport(w http.ResponseWriter, r *http.Request) {
	if err := engines.CancelReportExportJob(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"),
		r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Cancelled"})
}
