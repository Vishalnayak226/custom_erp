package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"custom_erp/engines"
)

// 205.15: the Reports API. The report catalogue through a key: the list the
// owner's role and the key's modules allow, and one report as JSON or CSV. The
// run re-enters the screen's own report runner as the owner, so a masked column,
// a store cut and a finance right hold as they do on the screen.

// bufferedResponse collects a handler's answer so it can be reshaped.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedResponse() *bufferedResponse { return &bufferedResponse{header: http.Header{}} }

func (b *bufferedResponse) Header() http.Header { return b.header }
func (b *bufferedResponse) WriteHeader(c int)   { b.status = c }
func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}
func (b *bufferedResponse) code() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}

type publicReportRow struct {
	ID       string                 `json:"id"`
	Number   string                 `json:"number,omitempty"`
	Label    string                 `json:"label"`
	Category string                 `json:"category"`
	Module   string                 `json:"module"`
	Question string                 `json:"question,omitempty"`
	Columns  []engines.ReportColumn `json:"columns"`
	Params   []engines.ReportParam  `json:"params"`
	Path     string                 `json:"path"`
}

// handlePublicReports lists the reports this key can run.
func handlePublicReports(w http.ResponseWriter, r *http.Request) {
	call := publicAPICallFrom(r)
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	w.Header().Set("Content-Type", "application/json")
	if call == nil {
		writeAPIError(w, r, engines.APIKeyOwnerInactive, "")
		return
	}
	state, err := engines.OwnerStateForKey(tenantID, call.Access)
	if err != nil {
		writeAPIError(w, r, engines.APIKeyOwnerInactive, "")
		return
	}
	role := engines.CanonicalRole(state.Role)
	moduleOK := map[string]bool{}
	out := []publicReportRow{}
	for _, def := range engines.ListReportDefinitions() {
		ok, seen := moduleOK[def.ModuleKey]
		if !seen {
			refusal, cerr := engines.CheckAPIKeyModule(tenantID, call.Credential, call.Access, def.ModuleKey, false)
			ok = cerr == nil && refusal == nil
			moduleOK[def.ModuleKey] = ok
		}
		if !ok || !engines.ReportAllowedForRole(def, role) {
			continue
		}
		cols := make([]engines.ReportColumn, 0, len(def.Columns))
		for _, c := range def.Columns {
			if !c.Sensitive {
				cols = append(cols, c)
			}
		}
		out = append(out, publicReportRow{
			ID: def.ID, Number: def.Number, Label: def.Label, Category: def.Category, Module: def.ModuleKey,
			Question: def.Question, Columns: cols, Params: def.Params, Path: "/api/public/v1/reports/" + def.ID,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Module < out[j].Module })
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": out, "count": len(out)})
}

// handlePublicReportRun runs one report. page is 1-based; page_size defaults to
// 200 and stops at 1000. Any other query parameter is the report's own filter.
func handlePublicReportRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := engines.GetReportDefinition(id); !ok {
		writeAPIErrorDetail(w, r, engines.APIKeyReportUnknown, "", "No report "+id+". See GET /api/public/v1/reports.")
		return
	}
	owner, ok := asKeyOwner(w, r, "GET /api/v1/reports/run/{id}")
	if !ok {
		return
	}
	q := r.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		writeAPIErrorDetail(w, r, engines.APIKeyInvalidRequest, "", "format is json or csv.")
		return
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(q.Get("page_size"))
	if size < 1 {
		size = 200
	}
	if size > 1000 {
		size = 1000
	}
	inner := url.Values{}
	for k, v := range q {
		if k == "format" || k == "page" || k == "page_size" || len(v) == 0 {
			continue
		}
		inner.Set(k, v[0])
	}
	inner.Set("page_size", strconv.Itoa(size))
	inner.Set("page_offset", strconv.Itoa((page-1)*size))
	u := *owner.URL
	u.RawQuery = inner.Encode()
	owner.URL = &u
	owner.SetPathValue("id", id)

	buf := newBufferedResponse()
	handleRunReport(buf, owner)
	if buf.code() != http.StatusOK {
		// The report's own refusal (a missing filter, a finance right) passes through.
		for k, v := range buf.header {
			w.Header()[k] = v
		}
		w.WriteHeader(buf.code())
		_, _ = w.Write(buf.body.Bytes())
		return
	}
	var resp struct {
		Columns []engines.ReportColumn   `json:"columns"`
		Rows    []map[string]interface{} `json:"rows"`
	}
	_ = json.Unmarshal(buf.body.Bytes(), &resp)
	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buf.body.Bytes())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", attachmentDisposition(id+".csv"))
	cw := engines.NewSafeCSVWriter(w)
	header := make([]string, len(resp.Columns))
	for i, c := range resp.Columns {
		header[i] = c.Label
	}
	_ = cw.Write(header)
	for _, row := range resp.Rows {
		rec := make([]string, len(resp.Columns))
		for i, c := range resp.Columns {
			if v, ok := row[c.Key]; ok && v != nil {
				if f, isNum := v.(float64); isNum {
					rec[i] = strconv.FormatFloat(f, 'f', -1, 64)
				} else {
					rec[i] = fmt.Sprint(v)
				}
			}
		}
		_ = cw.Write(rec)
	}
	cw.Flush()
}
