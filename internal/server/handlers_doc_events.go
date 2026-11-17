package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"custom_erp/engines"
)

// handleGenericDoc is the one document handler. After a save that succeeded it
// publishes the document's created / updated / submitted / cancelled event once
// (205.16), so a webhook sees every screen, import and API call alike. With no
// webhook subscription in the company it adds nothing but one cached check.
func handleGenericDoc(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
		handleGenericDocInner(w, r)
		return
	}
	if !engines.DocumentEventsWanted(tenantID) {
		handleGenericDocInner(w, r)
		return
	}
	doctype := r.PathValue("doctype")
	id := r.PathValue("id")
	if r.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err == nil {
			r.Body = io.NopCloser(bytes.NewReader(raw))
			if id == "" {
				var probe struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(raw, &probe)
				id = probe.ID
			}
		}
	}
	prior := engines.DocumentStatusOf(tenantID, doctype, id)
	existed := prior != ""
	cw := &docEventWriter{ResponseWriter: w}
	handleGenericDocInner(cw, r)
	if cw.status != 0 && (cw.status < 200 || cw.status >= 300) {
		return
	}
	var resp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if json.Unmarshal(cw.body.Bytes(), &resp) != nil || resp.ID == "" {
		return
	}
	engines.PublishDocumentEvent(tenantID, doctype, resp.ID, engines.EventForStatusChange(existed, prior, resp.Status), resp.Status)
}

// docEventWriter passes the response through and keeps its first 64 KB.
type docEventWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (c *docEventWriter) WriteHeader(code int) {
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

func (c *docEventWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.body.Len() < 64*1024 {
		c.body.Write(b)
	}
	return c.ResponseWriter.Write(b)
}

func (c *docEventWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }
