package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 55.15b: document attachments (engines/attachments.go), first used
// for expense receipts.
//
//	POST   /api/v1/attachments/{doctype}/{id}          add a file (multipart, field "file")
//	GET    /api/v1/attachments/{doctype}/{id}          list a document's files
//	GET    /api/v1/attachments/{doctype}?ids=a,b       file counts for a list screen
//	GET    /api/v1/attachments/file/{attachmentID}     the file itself
//	DELETE /api/v1/attachments/file/{attachmentID}     remove (soft delete)
//	POST   /api/v1/attachments/file/{attachmentID}/remove  the same, for clients without DELETE
//
// A file is exactly as private as its document: every route first applies
// the read rule GET /api/v1/doc/{doctype}/{id} applies to that document
// (documentReadRule below), and adding or removing additionally needs update
// permission on the doctype - the document's status and the uploader are then
// checked by the engine, under a lock on the document.

// attachmentUploadLimit is the multipart body this route reads: one 5 MB file
// plus the form's own framing. The shared request envelope allows the same
// for this path (request_envelope.go), since a nested MaxBytesReader can only
// lower the envelope's cap, never raise it.
const attachmentUploadLimit = engines.MaxAttachmentBytes + 1<<20

// documentReadRule is one request's read rule for one doctype, built from the
// same pieces, in the same order, as handleGenericDoc's single-document GET
// (handlers_core_doc_engine.go): role_permissions read, the doctype's module
// gate, the session scope (engines.ResolveSessionScope), and then per document
// the object-level scope (engines.ScopeConstraints/DocumentMatchesScope, or the
// legacy location rule for a doctype the scope registry does not know) and the
// supplier scope. It is never looser than that GET: an extension token, which
// that GET admits read-only for its one doctype, is refused here outright.
type documentReadRule struct {
	doctype      string
	role         string
	location     string
	supplierCode string
	scope        engines.SessionScope
}

// newDocumentReadRule resolves the doctype-level half of the rule. On refusal
// it has already written the response and returns ok=false.
func newDocumentReadRule(w http.ResponseWriter, r *http.Request, tenantID, doctype string) (documentReadRule, bool) {
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	rule := documentReadRule{doctype: doctype, role: role, location: r.Header.Get("Resolved-Location")}
	if r.Header.Get("Resolved-Purpose") == "extension" || role == "" {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return rule, false
	}
	if role == supplierRole {
		code, err := engines.SupplierCodeForUser(tenantID, userID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to resolve the supplier for this account")
			return rule, false
		}
		if code == "" {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return rule, false
		}
		rule.supplierCode = code
	}
	allowed, err := checkPermission(tenantID, role, doctype, "read")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return rule, false
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return rule, false
	}
	moduleKey, err := engines.ModuleForDoctype(tenantID, doctype)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return rule, false
	}
	if err := engines.RequireModules(tenantID, moduleKey); err != nil {
		writeEngineError(w, r, err, http.StatusServiceUnavailable)
		return rule, false
	}
	rule.scope, err = engines.ResolveSessionScope(tenantID, doctype,
		engines.SessionScope{Role: role, UserID: userID, LocationCode: rule.location})
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to resolve the access scope for this session")
		return rule, false
	}
	return rule, true
}

// visible is the object-level half: may this session see this one document.
func (rule documentReadRule) visible(data map[string]interface{}) bool {
	if constraints, known := engines.ScopeConstraints(rule.doctype, rule.scope); known {
		if engines.DocumentMatchesScope(constraints, data) != "" {
			return false
		}
	} else {
		docLoc, hasLoc := data["location"]
		if !hasLoc {
			docLoc, hasLoc = data["location_code"]
		}
		if hasLoc && fmt.Sprintf("%v", docLoc) != rule.location && !engines.IsSuperAdmin(rule.role) {
			return false
		}
	}
	if rule.supplierCode != "" {
		if docSup, ok := data[supplierScopeField]; ok && fmt.Sprintf("%v", docSup) != rule.supplierCode {
			return false
		}
	}
	return true
}

// requireAttachmentDoctype refuses a doctype outside the attachment
// allow-list. It runs after the doctype-level read rule, so a disabled module
// answers SAAS-0191 for every doctype, as every other {doctype} route does.
func requireAttachmentDoctype(w http.ResponseWriter, r *http.Request, doctype string) bool {
	if engines.AttachmentsEnabled(doctype) {
		return true
	}
	writeAPIErrorDetail(w, r, "GLOBAL-0011", "", "Files cannot be attached to this kind of record.")
	return false
}

// authorizeAttachmentParent loads one document and applies the full read
// rule, plus update permission when the caller is changing its files.
func authorizeAttachmentParent(w http.ResponseWriter, r *http.Request, tenantID, doctype, id string, forChange bool) (*engines.AttachmentParent, bool) {
	rule, ok := newDocumentReadRule(w, r, tenantID, doctype)
	if !ok {
		return nil, false
	}
	if !requireAttachmentDoctype(w, r, doctype) {
		return nil, false
	}
	parents, err := engines.LoadAttachmentParents(tenantID, doctype, []string{id})
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return nil, false
	}
	parent, found := parents[id]
	if !found {
		writeAPIError(w, r, "GLOBAL-0004", "")
		return nil, false
	}
	if !rule.visible(parent.Data) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return nil, false
	}
	if forChange {
		allowed, err := checkPermission(tenantID, rule.role, doctype, "update")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return nil, false
		}
		if !allowed {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return nil, false
		}
	}
	return &parent, true
}

type attachmentView struct {
	engines.DocumentAttachment
	CanRemove bool `json:"can_remove"`
}

// handleListAttachments: GET /api/v1/attachments/{doctype}/{id}. can_add and
// can_remove say what this caller may do, so the screen offers only actions
// the server will accept.
func handleListAttachments(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype, id := r.PathValue("doctype"), r.PathValue("id")
	parent, ok := authorizeAttachmentParent(w, r, tenantID, doctype, id, false)
	if !ok {
		return
	}
	files, err := engines.ListAttachments(tenantID, doctype, id)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	role, userID := r.Header.Get("Resolved-Role"), r.Header.Get("Resolved-User-ID")
	canUpdate, err := checkPermission(tenantID, role, doctype, "update")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	editable := canUpdate && engines.AttachmentStatusAllowsChange(doctype, parent.Status)
	views := make([]attachmentView, 0, len(files))
	for _, f := range files {
		views = append(views, attachmentView{f, engines.IsSuperAdmin(role) || (editable && f.UploadedBy == userID)})
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"attachments": views,
		"can_add":     editable && len(files) < engines.MaxAttachmentsPerDocument,
		"max_files":   engines.MaxAttachmentsPerDocument,
		"max_bytes":   engines.MaxAttachmentBytes,
	})
}

// handleAttachmentCounts: GET /api/v1/attachments/{doctype}?ids=a,b,c - how
// many files each listed document holds, for a list screen's paperclip
// column. Only documents this session can read are counted; the rest are
// left out exactly as if they had no files.
func handleAttachmentCounts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")
	var ids []string
	for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) > 500 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Ask for the file counts of at most 500 records at a time.")
		return
	}
	rule, ok := newDocumentReadRule(w, r, tenantID, doctype)
	if !ok || !requireAttachmentDoctype(w, r, doctype) {
		return
	}
	parents, err := engines.LoadAttachmentParents(tenantID, doctype, ids)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	readable := make([]string, 0, len(parents))
	for id, p := range parents {
		if rule.visible(p.Data) {
			readable = append(readable, id)
		}
	}
	counts, err := engines.CountAttachments(tenantID, doctype, readable)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"counts": counts})
}

// handleAddAttachment: POST /api/v1/attachments/{doctype}/{id}.
func handleAddAttachment(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	doctype, id := r.PathValue("doctype"), r.PathValue("id")
	// Authorized before the body is read, so a caller who may not touch the
	// document cannot make the server buffer 5 MB first.
	if _, ok := authorizeAttachmentParent(w, r, tenantID, doctype, id, true); !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, attachmentUploadLimit)
	if err := r.ParseMultipartForm(attachmentUploadLimit); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeAPIErrorDetail(w, r, "EXP-0542", "", "One file can be at most 5 MB.")
			return
		}
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose a file to attach (sent as multipart/form-data in a field named \"file\").")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose a file to attach (sent as multipart/form-data in a field named \"file\").")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, engines.MaxAttachmentBytes+1))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Failed to read the uploaded file")
		return
	}
	att, err := engines.AddAttachment(tenantID, doctype, id, header.Filename, content, userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	engines.LogAuditEvent(tenantID, userID, "DOCUMENT_ATTACHMENT", "ATTACHMENT_ADDED",
		fmt.Sprintf("%s %s: added %q (%s, %d bytes, sha256 %s, attachment %s)", doctype, id, att.FileName, att.ContentType, att.SizeBytes, att.SHA256, att.ID))
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(att)
}

// handleDownloadAttachment: GET /api/v1/attachments/file/{attachmentID}.
// Pictures are served inline, a PDF as a download; the type is the one sniffed
// at upload, and nosniff stops a browser second-guessing it.
func handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	meta, err := engines.GetAttachment(tenantID, r.PathValue("attachmentID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	if _, ok := authorizeAttachmentParent(w, r, tenantID, meta.Doctype, meta.DocumentID, false); !ok {
		return
	}
	att, f, err := engines.OpenAttachment(tenantID, meta.ID)
	if err != nil {
		var verr *engines.ValidationError
		if errors.As(err, &verr) {
			writeEngineError(w, r, err, http.StatusNotFound)
			return
		}
		engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "ERROR", r.URL.Path, fmt.Sprintf("attachment %s: %v", meta.ID, err), "")
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "The stored file is missing. Ask your administrator to restore the media store from backup.")
		return
	}
	defer f.Close()
	disposition := "inline"
	if !strings.HasPrefix(att.ContentType, "image/") {
		disposition = "attachment"
	}
	header := mime.FormatMediaType(disposition, map[string]string{"filename": att.FileName})
	if header == "" {
		header = disposition
	}
	w.Header().Set("Content-Type", att.ContentType)
	w.Header().Set("Content-Disposition", header)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", att.CreatedAt, f)
}

// handleRemoveAttachment: DELETE /api/v1/attachments/file/{attachmentID} and
// POST .../remove.
func handleRemoveAttachment(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	meta, err := engines.GetAttachment(tenantID, r.PathValue("attachmentID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	if _, ok := authorizeAttachmentParent(w, r, tenantID, meta.Doctype, meta.DocumentID, true); !ok {
		return
	}
	att, err := engines.RemoveAttachment(tenantID, meta.ID, userID, engines.IsSuperAdmin(role))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	engines.LogAuditEvent(tenantID, userID, "DOCUMENT_ATTACHMENT", "ATTACHMENT_REMOVED",
		fmt.Sprintf("%s %s: removed %q (sha256 %s, attachment %s, added by %s)", att.Doctype, att.DocumentID, att.FileName, att.SHA256, att.ID, att.UploadedBy))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "removed", "id": att.ID})
}
