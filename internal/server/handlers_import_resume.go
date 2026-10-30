package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"custom_erp/engines"
)

// Stage 55.11 - an import job's failed rows: summary, download and resume
// (engines/import_resume.go). The caller needs the same permission on the
// job's record type that the original import needed: read to look, create
// to resume.

func importJobGuard(w http.ResponseWriter, r *http.Request, action string) (tenantID, jobID string, ok bool) {
	tenantID, jobID = r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id")
	doctype, err := engines.ImportJobTargetDoctype(tenantID, jobID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return "", "", false
	}
	allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), doctype, action)
	if err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return "", "", false
	}
	return tenantID, jobID, true
}

// GET /api/v1/import-jobs/{id}
func handleImportJobSummary(w http.ResponseWriter, r *http.Request) {
	tenantID, jobID, ok := importJobGuard(w, r, "read")
	if !ok {
		return
	}
	summary, err := engines.ImportJobSummary(tenantID, jobID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(summary)
}

// GET /api/v1/import-jobs/{id}/failed-rows.csv
func handleImportJobFailedRows(w http.ResponseWriter, r *http.Request) {
	tenantID, jobID, ok := importJobGuard(w, r, "read")
	if !ok {
		return
	}
	body, err := engines.ImportJobFailedRowsCSV(tenantID, jobID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=failed_rows_%s.csv", jobID))
	_, _ = w.Write(body)
}

// POST /api/v1/import-jobs/{id}/resume (multipart "file"; ?dry_run=1 checks only)
func handleResumeImportJob(w http.ResponseWriter, r *http.Request) {
	tenantID, jobID, ok := importJobGuard(w, r, "create")
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeAPIError(w, r, "GLOBAL-0007", "")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Attach the corrected failed-rows CSV as 'file'.")
		return
	}
	defer file.Close()
	dryRun := r.URL.Query().Get("dry_run") == "1"
	res, childID, err := engines.ResumeImportJob(r.Context(), tenantID, jobID, engines.NamedUpload(file, header.Filename), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"), dryRun)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	out := map[string]interface{}{}
	if b, mErr := json.Marshal(res); mErr == nil {
		_ = json.Unmarshal(b, &out)
	}
	if childID != "" {
		out["import_job_id"] = childID
	}
	if summary, sErr := engines.ImportJobSummary(tenantID, jobID); sErr == nil {
		out["job"] = summary
	}
	_ = json.NewEncoder(w).Encode(out)
}
