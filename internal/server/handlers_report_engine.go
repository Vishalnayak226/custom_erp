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
	_ = json.NewEncoder(w).Encode(engines.ListReportDefinitions())
}

func handleRunReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	reportID := r.PathValue("id")
	params := flattenQueryParams(r)
	def, rows, err := engines.RunReport(tenantID, reportID, role, params)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id": def.ID, "label": def.Label, "columns": def.Columns, "has_drill_down": def.HasDrillDown, "rows": rows,
	})
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
	rows, err := engines.RunReportDrillDown(tenantID, reportID, role, rowKey, params)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
		http.Error(w, "Field 'report_id' is required", http.StatusBadRequest)
		return
	}
	jobID, err := engines.CreateReportExportJob(tenantID, req.ReportID, role, req.Params, userID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": jobID, "status": "Pending"})
}

func handleGetReportExport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	jobID := r.PathValue("id")
	status, csvBytes, err := engines.GetReportExportJob(tenantID, jobID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if status == "Completed" && r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+jobID+".csv\"")
		_, _ = w.Write(csvBytes)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": jobID, "status": status})
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
