package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"custom_erp/engines"
)

// Stage 47.13.1 - a staged import (engines/import_staging.go): upload,
// check, preview, approve, commit, reconcile, each its own call. A caller
// needs on the batch's record type what a direct import needs there: read to
// look, create or update to stage, check, approve and commit. A batch id is
// looked up in the caller's own client only, so another client's id is not
// found. Extension tokens are refused everywhere.

func refuseExtensionToken(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Resolved-Purpose") == "extension" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Extension tokens cannot use staged imports")
		return true
	}
	return false
}

// importBatchGuard finds the batch in the caller's client and checks the
// caller's right on its record type ("read" to look, "write" to act).
func importBatchGuard(w http.ResponseWriter, r *http.Request, action string) (tenantID, batchID string, ok bool) {
	if refuseExtensionToken(w, r) {
		return "", "", false
	}
	tenantID, batchID = r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id")
	b, err := engines.LoadImportBatch(tenantID, batchID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return "", "", false
	}
	role := r.Header.Get("Resolved-Role")
	if action == "read" {
		allowed, err := checkPermission(tenantID, role, b.DoctypeName, "read")
		if err != nil || !allowed {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return "", "", false
		}
		return tenantID, batchID, true
	}
	if !requireImportPermission(w, r, tenantID, role, b.DoctypeName) {
		return "", "", false
	}
	return tenantID, batchID, true
}

func writeImportBatch(w http.ResponseWriter, v *engines.ImportBatchView, err error, r *http.Request) {
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// POST /api/v1/import-batches (multipart: doctype, file)
func handleImportBatchCreate(w http.ResponseWriter, r *http.Request) {
	if refuseExtensionToken(w, r) {
		return
	}
	tenantID, userID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role")
	r.Body = http.MaxBytesReader(w, r.Body, 2*engines.ImportStagingMaxBytes)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeAPIError(w, r, "GLOBAL-0007", "")
		return
	}
	doctype := r.FormValue("doctype")
	if !requireImportPermission(w, r, tenantID, role, doctype) {
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Attach the CSV as 'file'.")
		return
	}
	defer file.Close()
	v, err := engines.StageImportBatch(r.Context(), tenantID, doctype, file, header.Filename, userID, role)
	if err == nil && v != nil && strings.TrimSpace(r.FormValue("expected_totals")) != "" {
		v, err = engines.SetImportBatchDeclaredTotals(tenantID, v.ID, r.FormValue("expected_totals"))
	}
	writeImportBatch(w, v, err, r)
}

// GET /api/v1/import-batches
func handleImportBatchList(w http.ResponseWriter, r *http.Request) {
	if refuseExtensionToken(w, r) {
		return
	}
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	list, err := engines.ListImportBatches(tenantID, 200)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	// Only the batches whose record type the caller may read.
	allowed := map[string]bool{}
	out := make([]*engines.ImportBatchView, 0, len(list))
	for _, b := range list {
		ok, seen := allowed[b.DoctypeName]
		if !seen {
			ok, _ = checkPermission(tenantID, role, b.DoctypeName, "read")
			allowed[b.DoctypeName] = ok
		}
		if ok {
			out = append(out, b)
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}

// GET /api/v1/import-batches/{id}
func handleImportBatchGet(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ok := importBatchGuard(w, r, "read")
	if !ok {
		return
	}
	v, err := engines.LoadImportBatch(tenantID, id)
	writeImportBatch(w, v, err, r)
}

// GET /api/v1/import-batches/{id}/rows?status=&limit=&offset=
func handleImportBatchRows(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ok := importBatchGuard(w, r, "read")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	rows, err := engines.ListImportBatchRows(tenantID, id, r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(rows)
}

// importBatchStep is one POST step on a batch: guard, then run.
func importBatchStep(step func(r *http.Request, tenantID, id, userID, role string) (*engines.ImportBatchView, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, id, ok := importBatchGuard(w, r, "write")
		if !ok {
			return
		}
		v, err := step(r, tenantID, id, r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"))
		writeImportBatch(w, v, err, r)
	}
}

// POST /api/v1/import-batches/{id}/validate | preview | approve | commit | reconcile | discard
var (
	handleImportBatchValidate = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.ValidateImportBatch(r.Context(), t, id, u, role)
	})
	handleImportBatchPreview = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.PreviewImportBatch(r.Context(), t, id)
	})
	handleImportBatchApprove = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.ApproveImportBatch(r.Context(), t, id, u, role)
	})
	handleImportBatchCommit = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.CommitImportBatch(r.Context(), t, id, u, role)
	})
	handleImportBatchReconcile = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.ReconcileImportBatch(r.Context(), t, id, u)
	})
	handleImportBatchDiscard = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.DiscardImportBatch(t, id, u)
	})
)
