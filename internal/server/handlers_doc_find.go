package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"custom_erp/db"
)

// Stage 139.3: GET /api/v1/doc-find?id=<number> - which record type holds a
// document number, for the Ctrl+K box and a scanned printout
// (public/shortcuts.js). documents.id is unique across every record type
// (engines/number_series.go), so one row answers it.
//
// Whether this person may see that record is not decided here: the record's
// own read (handleGenericDoc's GET) runs for it, so permission, store and
// supplier scope, module and read rules all apply exactly as when the record
// is opened. Anything that read refuses answers "not found" too, so the
// lookup never tells anyone that a number they cannot open exists.
func handleDocFind(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || len(id) > 120 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Give a document number to find.")
		return
	}
	schema, err := db.GetTenantSchema(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	var doctype string
	err = db.DB.QueryRowContext(r.Context(), fmt.Sprintf(
		`SELECT doctype FROM %s.documents WHERE id = $1 AND deleted_at IS NULL LIMIT 1`, schema), id).Scan(&doctype)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err == nil && docReadable(r, doctype, id) {
		_ = json.NewEncoder(w).Encode(map[string]string{"doctype": doctype, "id": id})
		return
	}
	writeAPIErrorGeneric(w, r, http.StatusNotFound, "No document with that number that you can open.")
}

// docReadable runs the record's own GET for this session and reports whether
// it would have been shown. Only the status is kept; the body is discarded.
func docReadable(r *http.Request, doctype, id string) bool {
	probe := r.Clone(r.Context())
	probe.Method = http.MethodGet
	probe.URL = &url.URL{Path: "/api/v1/doc/" + url.PathEscape(doctype) + "/" + url.PathEscape(id)}
	probe.SetPathValue("doctype", doctype)
	probe.SetPathValue("id", id)
	rec := &statusOnlyWriter{header: http.Header{}}
	handleGenericDoc(rec, probe)
	return rec.status == 0 || rec.status == http.StatusOK
}

type statusOnlyWriter struct {
	header http.Header
	status int
}

func (s *statusOnlyWriter) Header() http.Header { return s.header }

func (s *statusOnlyWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return len(b), nil
}

func (s *statusOnlyWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
}
