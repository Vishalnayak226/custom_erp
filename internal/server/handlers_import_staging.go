package server

import (
	"encoding/json"
	"errors"
	"io"
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

// POST /api/v1/import-batches (multipart: doctype, file - a small file in one
// request) or JSON {upload_id, doctype, expected_totals} - a file of any size
// sent first as a resumable upload (297.2); its rows are read in the background.
func handleImportBatchCreate(w http.ResponseWriter, r *http.Request) {
	if refuseExtensionToken(w, r) {
		return
	}
	tenantID, userID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role")
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var in struct {
			UploadID       string `json:"upload_id"`
			Doctype        string `json:"doctype"`
			ExpectedTotals string `json:"expected_totals"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil || strings.TrimSpace(in.UploadID) == "" {
			writeAPIError(w, r, "GLOBAL-0007", "")
			return
		}
		if !requireImportPermission(w, r, tenantID, role, in.Doctype) {
			return
		}
		v, err := engines.StageImportBatchFromUpload(r.Context(), tenantID, in.Doctype, in.UploadID, userID, role, in.ExpectedTotals)
		writeImportBatch(w, v, err, r)
		return
	}
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

// POST /api/v1/import-batches/{id}/validate | preview | approve | commit | reconcile | discard | resume
var (
	// 297.2: validate, commit and reconcile finish inside the call for a small
	// batch and go to a background job for a large one; the batch comes back
	// with its work set and the screen follows it.
	handleImportBatchValidate = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.RunImportBatchStep(r.Context(), t, id, "validate", u, role)
	})
	handleImportBatchPreview = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.PreviewImportBatch(r.Context(), t, id)
	})
	handleImportBatchApprove = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.ApproveImportBatch(r.Context(), t, id, u, role)
	})
	handleImportBatchCommit = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.RunImportBatchStep(r.Context(), t, id, "commit", u, role)
	})
	handleImportBatchReconcile = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.RunImportBatchStep(r.Context(), t, id, "reconcile", u, role)
	})
	handleImportBatchDiscard = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.DiscardImportBatch(t, id, u)
	})
	// POST /api/v1/import-batches/{id}/resume - carries on a step that stopped
	// (a job that failed, a server restart) from where it got to (297.2).
	handleImportBatchResume = importBatchStep(func(r *http.Request, t, id, u, role string) (*engines.ImportBatchView, error) {
		return engines.ResumeImportBatch(r.Context(), t, id, u, role)
	})
)

// ---- 297.2: resumable upload --------------------------------------------------
//
// A large import file goes up in pieces, each a PUT of its bytes with the
// Upload-Offset header saying where in the file it starts. A piece lost on a
// slow or dropped line is sent again; the server keeps what it has and says
// how far it got (GET, or the Upload-Offset header of a 409), so an upload
// carries on after a reload or a day offline. An upload is its starter's own:
// another user's id is not found.

// POST /api/v1/uploads {file_name, size, purpose, doctype}
func handleUploadStart(w http.ResponseWriter, r *http.Request) {
	if refuseExtensionToken(w, r) {
		return
	}
	tenantID, userID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role")
	var in struct {
		FileName string `json:"file_name"`
		Size     int64  `json:"size"`
		Purpose  string `json:"purpose"`
		Doctype  string `json:"doctype"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		writeAPIError(w, r, "GLOBAL-0007", "")
		return
	}
	// The only purpose today is an import file: whoever cannot import the
	// record type cannot fill the server's disk with one.
	if in.Purpose != "" && in.Purpose != "import" {
		writeAPIErrorDetail(w, r, "DATAIM-0489", "", "only import files can be uploaded here")
		return
	}
	if !requireImportPermission(w, r, tenantID, role, in.Doctype) {
		return
	}
	u, err := engines.StartResumableUpload(tenantID, userID, "import", in.FileName, in.Size)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(u.Received, 10))
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(u)
}

// GET /api/v1/uploads/{id} - how far the server got.
func handleUploadGet(w http.ResponseWriter, r *http.Request) {
	if refuseExtensionToken(w, r) {
		return
	}
	u, err := engines.GetResumableUpload(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(u.Received, 10))
	_ = json.NewEncoder(w).Encode(u)
}

// PUT /api/v1/uploads/{id} (application/octet-stream, Upload-Offset: n) - one piece.
func handleUploadPiece(w http.ResponseWriter, r *http.Request) {
	if refuseExtensionToken(w, r) {
		return
	}
	off, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("Upload-Offset")), 10, 64)
	if err != nil || off < 0 {
		writeAPIErrorDetail(w, r, "DATAIM-0488", "", "send where the piece starts in the file as the Upload-Offset header")
		return
	}
	u, err := engines.PutResumableUploadPiece(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.PathValue("id"), off, r.Body)
	var gap engines.ErrUploadOffset
	if errors.As(err, &gap) {
		w.Header().Set("Upload-Offset", strconv.FormatInt(gap.Received, 10))
		writeAPIErrorDetail(w, r, "DATAIM-0488", "", gap.Error())
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(u.Received, 10))
	_ = json.NewEncoder(w).Encode(u)
}
