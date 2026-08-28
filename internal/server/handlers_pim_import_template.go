package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"custom_erp/engines"
)

// Stage 36.3: import depth. Templates/schedules are plain PIM masters
// authored through the existing generic document API (like
// PIMWorkflowDefinition/PIMTaskTemplate already are); these handlers cover
// what the generic endpoint cannot: the template picker, the 36.3.6 mapping
// preview, running a file through a template (preview or real), rotating a
// hook token, and the inbound webhook itself.

func handlePIMImportTemplates(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodGet) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMImportTemplate", "read")
	if !ok {
		return
	}
	templates, err := engines.ListPIMImportTemplates(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"templates": templates})
}

// handlePIMImportTemplatePreviewMapping is the 36.3.6 "what maps to what"
// preview - resolved column-mapping health before a schedule/hook is ever
// pointed at the template, no file upload needed.
func handlePIMImportTemplatePreviewMapping(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodGet) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMImportTemplate", "read")
	if !ok {
		return
	}
	preview, err := engines.PreviewPIMImportTemplateMapping(tenantID, r.PathValue("id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(preview)
}

// handlePIMImportTemplateRun runs an uploaded file through a template - the
// same shape handleBulkImport/handlePIMImportPreview already give a plain
// CSV upload, with dryRun picked by the route (see routes.go) rather than a
// query flag, so a preview can never accidentally commit.
func handlePIMImportTemplateRun(dryRun bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !pimRequireMethod(w, r, http.MethodPost) {
			return
		}
		requiredAction := "read"
		if !dryRun {
			requiredAction = "create"
		}
		tenantID, userID, ok := pimTaskGuard(w, r, "PIMImportTemplate", requiredAction)
		if !ok {
			return
		}
		if err := r.ParseMultipartForm(5 << 20); err != nil {
			writeAPIError(w, r, "GLOBAL-0007", "")
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "CSV file is mandatory under multipart FormFile 'file'")
			return
		}
		defer file.Close()

		res, err := engines.RunPIMImportTemplate(tenantID, r.PathValue("id"), file, userID, dryRun)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}

		responseMap := map[string]interface{}{}
		if b, mErr := json.Marshal(res); mErr == nil {
			_ = json.Unmarshal(b, &responseMap)
		}
		if !dryRun {
			if jobID, jobErr := engines.RecordImportJob(tenantID, "", res, userID); jobErr == nil && jobID != "" {
				responseMap["import_job_id"] = jobID
			}
		}
		_ = json.NewEncoder(w).Encode(responseMap)
	}
}

func handlePIMImportSchedules(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodGet) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMImportSchedule", "read")
	if !ok {
		return
	}
	schedules, err := engines.ListPIMImportSchedules(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"schedules": schedules})
}

// handlePIMImportScheduleRotateHookToken mints a fresh token and returns it
// exactly once, the same one-time-reveal shape Stage 38.2c's API key issue/
// rotate endpoints already use - the response is the only place this value
// is ever visible again.
func handlePIMImportScheduleRotateHookToken(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodPost) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMImportSchedule", "update")
	if !ok {
		return
	}
	token, err := engines.RotatePIMImportHookToken(tenantID, r.PathValue("id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"hook_token": token})
}

// handlePIMImportHook is the one unauthenticated route in this file - an
// external system holds only the token, never a session. Its path is
// static and listed once in middleware.go's publicRoutes ("a small,
// explicit allowlist rather than a path-prefix rule, so adding a new public
// route is always a one-line, reviewable decision") - a token-in-the-path
// route would defeat that exact-string allowlist, so the token travels as
// a header instead, the same place Stage 35.5's courier webhooks carry
// their per-tenant HMAC signature. X-Tenant-ID scopes the lookup the same
// way Stage 38's public API middleware already requires it of a
// credential-bearing caller; a missing header resolves to "default" the
// same fallback db.GetTenantSchema gives everywhere else. The file itself
// is the raw POST body (Content-Type: text/csv), not a multipart form - a
// webhook integration posting a generated file has no reason to speak
// multipart.
func handlePIMImportHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	// apiMiddleware resolves Resolved-Tenant-ID from X-Tenant-ID/Host/
	// ?tenant_id= (falling back to "default") for every request, including
	// this public one - no Authorization header means the token-claims
	// branch never overrides it, so this is exactly the same resolution
	// every other handler in this codebase already reads.
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	token := r.Header.Get("X-Hook-Token")
	if token == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Missing X-Hook-Token header")
		return
	}
	_, templateID, err := engines.ResolvePIMImportScheduleByHookToken(tenantID, token)
	if err != nil {
		// Deliberately the same generic message for "no such token" as for
		// "token belongs to a different tenant" - which tenant a token
		// belongs to is not information this endpoint should ever confirm.
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Import hook not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	res, err := engines.RunPIMImportTemplate(tenantID, templateID, r.Body, "system", false)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	jobID, jobErr := engines.RecordImportJob(tenantID, "", res, "system")
	if jobErr != nil {
		engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "WARN", r.URL.Path, fmt.Sprintf("hook import job record failed: %v", jobErr), "")
	}
	responseMap := map[string]interface{}{}
	if b, mErr := json.Marshal(res); mErr == nil {
		_ = json.Unmarshal(b, &responseMap)
	}
	if jobID != "" {
		responseMap["import_job_id"] = jobID
	}
	_ = json.NewEncoder(w).Encode(responseMap)
}
