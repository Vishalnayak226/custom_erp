package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 128: the Setup Workbook - configure an organisation from one Excel
// file. Download it (blank, or this organisation's current setup), fill it
// in, upload it: Preview lists what each row will do, Apply does it, and the
// same file can be uploaded again at any time to correct something.
//
// Every row is applied by replaying the API call its own screen makes,
// in-process, through that screen's handler, with the uploader's identity
// (swCaller). So the same checks, numbering, side effects and audit entries
// apply as on screen, and nothing here is a second copy of a rule. Rows are
// matched to existing records by a key per sheet and update only the cells
// that are filled; nothing is ever deleted. User guide: Admin Guide §B.3.19.

type swKind int

const (
	swDoc swKind = iota
	swSettings
	swSeries
	swItemFields
	swRoles
	swRoleAccess
	swUsers
	swApproval
)

// swSheet is one sheet of the workbook. A swDoc sheet is a master record
// type: its columns are that record type's own fields, so a field added
// later appears in the next download by itself.
type swSheet struct {
	Name    string
	Kind    swKind
	Doctype string
	// Keys are the natural keys a row is matched by, tried in order; each is
	// a set of fields that must all match.
	Keys [][]string
	// BlankKey: a blank key matches the record whose key is blank (the
	// receipt rule that applies to every vendor).
	BlankKey bool
	Skip     map[string]bool
	About    string
}

// setupWorkbookSheets are applied in this order: each sheet may refer to
// what the sheets before it create.
var setupWorkbookSheets = []swSheet{
	{Name: "Settings", Kind: swSettings, About: "Industry and every Configuration setting (Procurement, Inventory, Security...)."},
	{Name: "Legal Entities", Kind: swDoc, Doctype: "LegalEntity", Keys: [][]string{{"gstin"}, {"name"}}, About: "Your company: one per GSTIN."},
	{Name: "Locations", Kind: swDoc, Doctype: "Location", Keys: [][]string{{"short_code"}, {"name"}}, About: "Stores, warehouses and head office. A cost centre is made for each new one."},
	{Name: "Departments", Kind: swDoc, Doctype: "Department", Keys: [][]string{{"name"}}, About: "Teams: Purchase, Accounts, Stores..."},
	{Name: "HSN Codes", Kind: swDoc, Doctype: "HSNCode", Keys: [][]string{{"code"}, {"hsn"}}, About: "HSN codes and their GST rate."},
	{Name: "Units", Kind: swDoc, Doctype: "UOM", Keys: [][]string{{"code"}}, About: "Units of measure (PCS, KG...)."},
	{Name: "Number Series", Kind: swSeries, About: "How document numbers look (PO, GRN, ...). Existing series only."},
	{Name: "Categories", Kind: swDoc, Doctype: "Category", Keys: [][]string{{"name"}}, About: "Item categories (Antique, Cotton...)."},
	{Name: "MRP Rules", Kind: swDoc, Doctype: "MRPRule", Keys: [][]string{{"name"}}, About: "How the suggested MRP is worked out at the GRN."},
	{Name: "Product Types", Kind: swDoc, Doctype: "ProductType", Keys: [][]string{{"name"}}, About: "Product types (Necklace, Kurta...) and their MRP rule."},
	{Name: "Item Attributes", Kind: swDoc, Doctype: "ItemAttribute", Keys: [][]string{{"label"}}, Skip: map[string]bool{"library_id": true}, About: "Colour, Size, Purity... and their values."},
	{Name: "Item Names", Kind: swDoc, Doctype: "ItemName", Keys: [][]string{{"name"}, {"category", "product_type"}}, About: "Category + product type, with HSN and GST - what the buyer types on a PO line."},
	{Name: "Item Fields", Kind: swItemFields, About: "Which fields the item, the PO line and the GRN line ask."},
	{Name: "Vendors", Kind: swDoc, Doctype: "Vendor", Keys: [][]string{{"gstin"}, {"name"}}, About: "Suppliers."},
	{Name: "Receipt Rules", Kind: swDoc, Doctype: "ReceiptValidationRule", Keys: [][]string{{"vendor"}}, BlankKey: true, About: "Over-delivery tolerance; a row with no vendor is the rule for every vendor."},
	{Name: "Reason Codes", Kind: swDoc, Doctype: "ReasonCode", Keys: [][]string{{"category", "description"}}, About: "Reasons for short-close, rejection, damage and returns."},
	{Name: "TDS Sections", Kind: swDoc, Doctype: "TDSSection", Keys: [][]string{{"section_code"}}, About: "TDS sections and rates, picked when paying a vendor."},
	{Name: "Roles", Kind: swRoles, About: "Roles and the stores each one works at."},
	{Name: "Role Access", Kind: swRoleAccess, About: "What each role may read, create, update and delete - per module or per record type."},
	{Name: "Users", Kind: swUsers, About: "People: role, home store, department, stores. New users only get a first password."},
	{Name: "Approval Rules", Kind: swApproval, About: "Who approves what, by amount. The bands listed for a record type replace its bands."},
}

// swClear in a cell empties that value; a blank cell leaves it as it is.
const swClear = "(none)"

// swMaxUpload caps the uploaded file.
const swMaxUpload = 8 << 20

func swNorm(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// swHeaderNorm reads a header the way a person may have typed it: case,
// spacing, a trailing * and a bracketed note do not matter.
func swHeaderNorm(s string) string {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "*"))
	if i := strings.Index(s, "("); i > 0 {
		s = s[:i]
	}
	return swNorm(s)
}

func swYesNo(s string) (bool, bool) {
	switch swNorm(s) {
	case "yes", "y", "true", "1", "on":
		return true, true
	case "no", "n", "false", "0", "off":
		return false, true
	}
	return false, false
}

func swYesNoText(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func swNumber(s string) (float64, error) {
	clean := strings.NewReplacer(",", "", "₹", "", "%", "", " ", "").Replace(s)
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	return f, nil
}

func swNumText(f float64) string {
	return strconv.FormatFloat(math.Round(f*1e6)/1e6, 'f', -1, 64)
}

// swText prints a stored JSON value for a person.
func swText(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return swYesNoText(t)
	case float64:
		return swNumText(t)
	case json.Number:
		return t.String()
	case map[string]interface{}, []interface{}:
		raw, _ := json.Marshal(t)
		return string(raw)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

// ---------------------------------------------------------------- replay

// swCaller replays API calls in-process with the uploader's identity.
type swCaller struct {
	headers  http.Header
	tenantID string
	role     string
	username string
}

func newSWCaller(r *http.Request) *swCaller {
	h := http.Header{}
	for k, v := range r.Header {
		if strings.HasPrefix(k, "Resolved-") {
			h[k] = append([]string(nil), v...)
		}
	}
	return &swCaller{headers: h, tenantID: r.Header.Get("Resolved-Tenant-ID"), role: r.Header.Get("Resolved-Role"), username: r.Header.Get("Resolved-Username")}
}

type swRecorder struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (w *swRecorder) Header() http.Header { return w.header }
func (w *swRecorder) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
}
func (w *swRecorder) Write(b []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.body.Write(b)
}

func swErrBody(msg string) []byte {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return b
}

// call runs one request through its handler after the route-capability and
// module checks apiMiddleware applies to the same pattern.
func (c *swCaller) call(pattern, method, target string, pathValues map[string]string, body interface{}, h http.HandlerFunc) (int, []byte) {
	if ok, _, _ := checkRouteCapability(pattern, c.role); !ok {
		return http.StatusForbidden, swErrBody("Your role may not do this (" + pattern + ").")
	}
	if dt := pathValues["doctype"]; dt != "" {
		moduleKey, err := engines.ModuleForDoctype(c.tenantID, dt)
		if err != nil {
			return http.StatusInternalServerError, swErrBody(err.Error())
		}
		if err := engines.RequireModules(c.tenantID, moduleKey); err != nil {
			return http.StatusServiceUnavailable, swErrBody(err.Error())
		}
	}
	var rd io.Reader = http.NoBody
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return http.StatusInternalServerError, swErrBody(err.Error())
		}
		rd = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return http.StatusInternalServerError, swErrBody(err.Error())
	}
	for k, v := range c.headers {
		req.Header[k] = append([]string(nil), v...)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	rec := &swRecorder{header: http.Header{}, status: http.StatusOK}
	func() {
		defer func() {
			if p := recover(); p != nil {
				rec.status = http.StatusInternalServerError
				rec.body.Reset()
				rec.body.Write(swErrBody(fmt.Sprint("internal error: ", p)))
			}
		}()
		h(rec, req)
	}()
	return rec.status, rec.body.Bytes()
}

// do is call for a write: nil on success, the handler's own message if not.
func (c *swCaller) do(pattern, method, target string, pathValues map[string]string, body interface{}, h http.HandlerFunc) error {
	status, out := c.call(pattern, method, target, pathValues, body, h)
	if status >= 200 && status < 300 {
		return nil
	}
	return errors.New(swMessage(status, out))
}

func swMessage(status int, body []byte) string {
	var e struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		if e.Detail != "" && !strings.Contains(e.Error, e.Detail) {
			return e.Error + " - " + e.Detail
		}
		return e.Error
	}
	s := strings.TrimSpace(string(body))
	if s == "" {
		s = http.StatusText(status)
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// swOnce makes one write shared by several rows: it runs the first time any
// of them is applied, and every row reports its result.
func swOnce(f func() error) func() error {
	var once sync.Once
	var err error
	return func() error {
		once.Do(func() { err = f() })
		return err
	}
}

// ---------------------------------------------------------------- reading

type swInRow struct {
	Num   int
	Cells []string
}

type swTable struct {
	cols map[string]int
	rows []swInRow
}

func (t *swTable) has(names ...string) bool {
	for _, n := range names {
		if _, ok := t.cols[swHeaderNorm(n)]; ok {
			return true
		}
	}
	return false
}

// get returns the row's cell under the first of names the sheet has.
func (t *swTable) get(r swInRow, names ...string) string {
	for _, n := range names {
		if c, ok := t.cols[swHeaderNorm(n)]; ok {
			if c < len(r.Cells) {
				return strings.TrimSpace(r.Cells[c])
			}
			return ""
		}
	}
	return ""
}

// swParse reads the uploaded workbook into tables by sheet name, and the
// organisation it was downloaded from (the Instructions sheet's stamp).
func swParse(file []byte, headerWords map[string][]string) (map[string]*swTable, string, error) {
	sheets, err := engines.ReadXLSX(file)
	if err != nil {
		return nil, "", err
	}
	tables := map[string]*swTable{}
	stamp := ""
	for _, s := range sheets {
		name := swNorm(s.Name)
		if name == "instructions" {
			for _, row := range s.Rows {
				if len(row) >= 2 && swNorm(row[0]) == "organisation" {
					stamp = strings.TrimSpace(row[1])
				}
			}
			continue
		}
		words, known := headerWords[name]
		if !known {
			continue
		}
		t := &swTable{cols: map[string]int{}}
		header := -1
		for i := 0; i < len(s.Rows) && i < 6 && header < 0; i++ {
			for _, cell := range s.Rows[i] {
				for _, w := range words {
					if swHeaderNorm(cell) == swHeaderNorm(w) {
						header = i
					}
				}
			}
		}
		if header < 0 {
			tables[name] = t
			continue
		}
		for c, cell := range s.Rows[header] {
			if h := swHeaderNorm(cell); h != "" {
				if _, dup := t.cols[h]; !dup {
					t.cols[h] = c
				}
			}
		}
		for i := header + 1; i < len(s.Rows); i++ {
			cells := s.Rows[i]
			blank := true
			for _, c := range cells {
				blank = blank && strings.TrimSpace(c) == ""
			}
			// The hint row under a header starts with "#" in its first cell.
			if blank || (len(cells) > 0 && strings.HasPrefix(strings.TrimSpace(cells[0]), "#")) {
				continue
			}
			t.rows = append(t.rows, swInRow{Num: s.RowNumbers[i], Cells: cells})
		}
		tables[name] = t
	}
	return tables, stamp, nil
}

// ---------------------------------------------------------------- plan

type swChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// swRow is what one row of the workbook will do (preview) or did (apply).
type swRow struct {
	Row     int        `json:"row"`
	Key     string     `json:"key"`
	Action  string     `json:"action"` // new | change | same | error | note
	Changes []swChange `json:"changes,omitempty"`
	Message string     `json:"message,omitempty"`
	Result  string     `json:"result,omitempty"` // done | failed (apply)
	do      func() error
	creates []string // what a "new" row can be named by, for later sheets' links
}

type swSheetResult struct {
	Sheet   string   `json:"sheet"`
	Present bool     `json:"present"`
	New     int      `json:"new"`
	Change  int      `json:"change"`
	Same    int      `json:"same"`
	Errors  int      `json:"errors"`
	Notes   int      `json:"notes"`
	Done    int      `json:"done"`
	Failed  int      `json:"failed"`
	Rows    []*swRow `json:"rows"`
}

type swRec struct {
	ID   string
	Data map[string]interface{}
}

type swDocIndex struct {
	recs   []swRec
	byID   map[string]int
	byText map[string][]int
}

// swLinkText are the fields a record can be named by in a link cell.
var swLinkText = []string{"code", "name", "label", "short_code", "gstin", "section_code", "hsn", "facility_code"}

type swRun struct {
	c       *swCaller
	schema  string
	sameOrg bool
	apply   bool
	docs    map[string]*swDocIndex
	// pending: names earlier sheets will create (preview only), so a link to
	// them is not reported as missing.
	pending map[string]map[string]bool
	// renames: a record an earlier sheet renames, by its new name (preview
	// only), so a later sheet may already use the new name.
	renames map[string]map[string]string
	roles   map[string]string // normalized -> role name, incl. pending
	// industryPending: this workbook sets the industry, which adds item
	// fields a preview cannot see yet.
	industryPending bool
}

func newSWRun(c *swCaller, sameOrg, apply bool) (*swRun, error) {
	schema, err := db.GetTenantSchema(c.tenantID)
	if err != nil {
		return nil, err
	}
	return &swRun{c: c, schema: schema, sameOrg: sameOrg, apply: apply, docs: map[string]*swDocIndex{}, pending: map[string]map[string]bool{}}, nil
}

func (run *swRun) index(doctype string) *swDocIndex {
	if idx, ok := run.docs[doctype]; ok {
		return idx
	}
	idx := &swDocIndex{byID: map[string]int{}, byText: map[string][]int{}}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL ORDER BY id`, run.schema), doctype)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			var raw []byte
			if rows.Scan(&id, &raw) != nil {
				continue
			}
			data := map[string]interface{}{}
			_ = json.Unmarshal(raw, &data)
			n := len(idx.recs)
			idx.recs = append(idx.recs, swRec{ID: id, Data: data})
			idx.byID[strings.ToLower(id)] = n
			seen := map[string]bool{}
			for _, f := range swLinkText {
				if v := swNorm(swText(data[f])); v != "" && !seen[v] {
					seen[v] = true
					idx.byText[v] = append(idx.byText[v], n)
				}
			}
		}
	}
	run.docs[doctype] = idx
	return idx
}

// display names a record for a person: its name, label or code.
func (run *swRun) display(doctype, id string) string {
	idx := run.index(doctype)
	if n, ok := idx.byID[strings.ToLower(id)]; ok {
		d := idx.recs[n].Data
		for _, f := range []string{"name", "label", "description", "section_code", "code"} {
			if s := swText(d[f]); s != "" {
				return s
			}
		}
	}
	return id
}

// resolveLink finds the record a link cell names - by id, code, name,
// label, short code or GSTIN. pending is true when an earlier sheet of this
// upload will create it (preview).
func (run *swRun) resolveLink(target, value string) (string, bool, error) {
	idx := run.index(target)
	if n, ok := idx.byID[strings.ToLower(value)]; ok {
		return idx.recs[n].ID, false, nil
	}
	hits := idx.byText[swNorm(value)]
	if len(hits) == 1 {
		return idx.recs[hits[0]].ID, false, nil
	}
	if len(hits) > 1 {
		return "", false, fmt.Errorf("%d %s records are called %q - use its code", len(hits), swDoctypeLabel(target), value)
	}
	if id, ok := run.renames[target][swNorm(value)]; ok {
		return id, false, nil
	}
	if run.pending[target][swNorm(value)] {
		return value, true, nil
	}
	return "", false, fmt.Errorf("no %s called %q", swDoctypeLabel(target), value)
}

// ---------------------------------------------------------------- run

// process plans every sheet the workbook has, in order, and when applying
// applies each sheet before planning the next, so a later sheet sees what
// an earlier one created.
func (run *swRun) process(tables map[string]*swTable, progress func(done int, sheet string)) []*swSheetResult {
	var out []*swSheetResult
	for i, sh := range setupWorkbookSheets {
		res := &swSheetResult{Sheet: sh.Name, Rows: []*swRow{}}
		out = append(out, res)
		t := tables[swNorm(sh.Name)]
		if t == nil {
			continue
		}
		res.Present = true
		if progress != nil {
			progress(i, sh.Name)
		}
		var rows []*swRow
		switch sh.Kind {
		case swDoc:
			rows = run.planDoc(sh, t)
		case swSettings:
			rows = run.planSettings(t)
		case swSeries:
			rows = run.planSeries(t)
		case swItemFields:
			rows = run.planItemFields(t)
		case swRoles:
			rows = run.planRoles(t)
		case swRoleAccess:
			rows = run.planRoleAccess(t)
		case swUsers:
			rows = run.planUsers(t)
		case swApproval:
			rows = run.planApproval(t)
		}
		if rows != nil {
			res.Rows = rows
		}
		for _, r := range rows {
			switch r.Action {
			case "new":
				res.New++
				if sh.Doctype != "" && len(r.creates) > 0 {
					if run.pending[sh.Doctype] == nil {
						run.pending[sh.Doctype] = map[string]bool{}
					}
					for _, name := range r.creates {
						run.pending[sh.Doctype][swNorm(name)] = true
					}
				}
			case "change":
				res.Change++
			case "same":
				res.Same++
			case "error":
				res.Errors++
			case "note":
				res.Notes++
			}
		}
		if !run.apply {
			continue
		}
		for _, r := range rows {
			if r.do == nil || (r.Action != "new" && r.Action != "change") {
				continue
			}
			if err := r.do(); err != nil {
				r.Result, r.Message = "failed", err.Error()
				res.Failed++
			} else {
				r.Result = "done"
				res.Done++
			}
		}
		// What this sheet created or changed is what the next sheets link to.
		if sh.Doctype != "" {
			delete(run.docs, sh.Doctype)
		}
	}
	if progress != nil {
		progress(len(setupWorkbookSheets), "")
	}
	return out
}

func swErrRow(num int, key, msg string) *swRow {
	return &swRow{Row: num, Key: key, Action: "error", Message: msg}
}

// ---------------------------------------------------------------- records

// swDocFields are the columns of a record sheet: the record type's own
// fields, code first, without the ones the server keeps for itself.
func swDocFields(sh swSheet, meta []engines.FieldMeta) []engines.FieldMeta {
	var code *engines.FieldMeta
	var out []engines.FieldMeta
	for i := range meta {
		f := meta[i]
		if f.Fieldname == "id" || f.Mirrored || sh.Skip[f.Fieldname] {
			continue
		}
		if f.Fieldname == "code" {
			code = &meta[i]
			continue
		}
		out = append(out, f)
	}
	if code != nil {
		out = append([]engines.FieldMeta{*code}, out...)
	}
	return out
}

func swSelectOptions(options string) []string {
	var out []string
	for _, o := range strings.FieldsFunc(options, func(r rune) bool { return r == ',' || r == '\n' }) {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
}

// convert reads a cell as its field's type.
func (run *swRun) convert(f engines.FieldMeta, s string) (interface{}, bool, error) {
	switch f.Fieldtype {
	case "Number", "Int", "Float", "Currency", "Percent":
		n, err := swNumber(s)
		return n, false, err
	case "Check":
		b, ok := swYesNo(s)
		if !ok {
			return nil, false, fmt.Errorf("write Yes or No")
		}
		return b, false, nil
	case "Select":
		opts := swSelectOptions(f.Options)
		for _, o := range opts {
			if swNorm(o) == swNorm(s) {
				return o, false, nil
			}
		}
		if len(opts) == 0 {
			return s, false, nil
		}
		return nil, false, fmt.Errorf("must be one of: %s", strings.Join(opts, ", "))
	case "Link":
		if strings.TrimSpace(f.Options) == "" {
			return s, false, nil
		}
		id, pending, err := run.resolveLink(f.Options, s)
		return id, pending, err
	case "Date":
		if n, err := strconv.ParseFloat(s, 64); err == nil && n > 1 && n < 100000 {
			return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(n)).Format("2006-01-02"), false, nil
		}
		return s, false, nil
	}
	return s, false, nil
}

// swSame reports whether a stored value already equals a cell's value.
func swSame(f engines.FieldMeta, stored, v interface{}) bool {
	switch nv := v.(type) {
	case float64:
		if sv, err := swNumber(swText(stored)); err == nil {
			return math.Abs(sv-nv) < 1e-9
		}
		return false
	case bool:
		sb, ok := swYesNo(swText(stored))
		return ok && sb == nv
	}
	s, nvs := strings.TrimSpace(swText(stored)), strings.TrimSpace(swText(v))
	if f.Fieldtype == "Select" || f.Fieldtype == "Link" {
		return strings.EqualFold(s, nvs)
	}
	return s == nvs
}

func (run *swRun) planDoc(sh swSheet, t *swTable) []*swRow {
	meta, err := engines.GetDocTypeMeta(run.c.tenantID, sh.Doctype)
	if err != nil || len(meta) == 0 {
		return []*swRow{swErrRow(0, sh.Name, fmt.Sprintf("could not read the %s fields: %v", sh.Doctype, err))}
	}
	fields := swDocFields(sh, meta)
	byName := map[string]engines.FieldMeta{}
	cols := map[string]string{} // fieldname -> header used
	for _, f := range fields {
		byName[f.Fieldname] = f
		for _, h := range []string{f.Label, f.Fieldname} {
			if t.has(h) {
				cols[f.Fieldname] = h
				break
			}
		}
	}
	idx := run.index(sh.Doctype)
	typed := engines.IsTypedCodeMaster(sh.Doctype)
	label := func(f string) string {
		if m, ok := byName[f]; ok {
			return m.Label
		}
		return f
	}
	seen := map[string]int{}
	var out []*swRow
	for _, in := range t.rows {
		vals := map[string]interface{}{}
		raw := map[string]string{}
		var problems []string
		// A list value that is no longer on the field's list is only wrong
		// when it changes: a record that already holds it reads back as is.
		offList := map[string]string{}
		for _, f := range fields {
			h, ok := cols[f.Fieldname]
			if !ok {
				continue
			}
			s := t.get(in, h)
			if s == "" {
				continue
			}
			raw[f.Fieldname] = s
			if strings.EqualFold(s, swClear) {
				if f.Mandatory {
					problems = append(problems, f.Label+" is required and cannot be emptied")
				} else {
					vals[f.Fieldname] = ""
				}
				continue
			}
			v, _, err := run.convert(f, s)
			if err != nil && f.Fieldtype == "Select" {
				offList[f.Fieldname] = f.Label + ": " + err.Error()
				vals[f.Fieldname] = s
				continue
			}
			if err != nil {
				problems = append(problems, f.Label+": "+err.Error())
				continue
			}
			vals[f.Fieldname] = v
		}
		if len(raw) == 0 {
			continue
		}
		// An HSN row needs only the HSN number: the server builds its code
		// (HSN-7117). A number typed in the Code column is read as the HSN.
		if sh.Doctype == "HSNCode" && raw["hsn"] == "" && raw["code"] != "" {
			h := strings.TrimPrefix(strings.ToUpper(raw["code"]), "HSN-")
			vals["hsn"], raw["hsn"] = h, h
		}
		matchID, keyText, kerr := run.matchDoc(sh, idx, vals, raw, typed, label)
		if keyText == "" {
			keyText = fmt.Sprintf("row %d", in.Num)
		}
		if kerr != nil {
			problems = append(problems, kerr.Error())
		}
		for f, msg := range offList {
			if matchID != "" {
				stored := swText(idx.recs[idx.byID[strings.ToLower(matchID)]].Data[f])
				if strings.EqualFold(strings.TrimSpace(stored), raw[f]) {
					continue
				}
			}
			problems = append(problems, msg)
		}
		if len(problems) > 0 {
			out = append(out, swErrRow(in.Num, keyText, strings.Join(problems, "; ")))
			continue
		}
		dupKey := "id:" + matchID
		if matchID == "" {
			dupKey = "new:" + swNorm(keyText)
		}
		if first, dup := seen[dupKey]; dup {
			out = append(out, swErrRow(in.Num, keyText, fmt.Sprintf("the same record as row %d - keep one row for it", first)))
			continue
		}
		seen[dupKey] = in.Num

		row := &swRow{Row: in.Num, Key: keyText}
		dt := sh.Doctype
		if matchID == "" {
			if !typed {
				delete(vals, "code")
			}
			var missing []string
			for _, f := range fields {
				if !f.Mandatory || f.AutoGenerated || f.Fieldname == "status" || (f.Fieldname == "code" && (!typed || dt == "HSNCode")) || (dt == "ItemName" && f.Fieldname == "name") {
					continue
				}
				if v, ok := vals[f.Fieldname]; !ok || swText(v) == "" {
					missing = append(missing, f.Label)
				}
			}
			if len(missing) > 0 {
				out = append(out, swErrRow(in.Num, keyText, "a new record needs: "+strings.Join(missing, ", ")))
				continue
			}
			if _, ok := byName["status"]; ok {
				if _, set := vals["status"]; !set {
					vals["status"] = "Active"
				}
			}
			row.Action = "new"
			for _, f := range fields {
				if v, ok := vals[f.Fieldname]; ok {
					to := raw[f.Fieldname]
					if to == "" {
						to = swText(v)
					}
					row.Changes = append(row.Changes, swChange{Field: f.Label, To: to})
				}
			}
			// Every way a link cell may name this record (swLinkText), so a
			// later sheet may write its name, short code or GSTIN.
			for _, f := range swLinkText {
				if s := raw[f]; s != "" {
					row.creates = append(row.creates, s)
				}
			}
			payload := vals
			row.do = func() error {
				return run.c.do("/api/v1/doc/{doctype}", http.MethodPost, "/api/v1/doc/"+url.PathEscape(dt),
					map[string]string{"doctype": dt}, payload, handleGenericDoc)
			}
		} else {
			rec := idx.recs[idx.byID[strings.ToLower(matchID)]]
			payload := map[string]interface{}{}
			var structured []string
			for _, f := range fields {
				v, ok := vals[f.Fieldname]
				if !ok || f.Fieldname == "code" {
					continue
				}
				if swSame(f, rec.Data[f.Fieldname], v) {
					continue
				}
				// A value kept as structured data (a list, a table) is shown in
				// the download as JSON and is not written back from a cell.
				switch rec.Data[f.Fieldname].(type) {
				case map[string]interface{}, []interface{}:
					structured = append(structured, f.Label)
					continue
				}
				payload[f.Fieldname] = v
				from := swText(rec.Data[f.Fieldname])
				to := raw[f.Fieldname]
				if f.Fieldtype == "Link" && from != "" {
					from = run.display(f.Options, from)
				}
				row.Changes = append(row.Changes, swChange{Field: f.Label, From: from, To: to})
			}
			if len(structured) > 0 {
				row.Message = strings.Join(structured, ", ") + " not changed - kept as structured data; change it on its own screen"
			}
			if len(payload) == 0 {
				row.Action = "same"
				if len(structured) > 0 {
					row.Action = "note"
				}
			} else {
				row.Action = "change"
				id := rec.ID
				for _, f := range swLinkText {
					if _, renamed := payload[f]; renamed && raw[f] != "" {
						if run.renames == nil {
							run.renames = map[string]map[string]string{}
						}
						if run.renames[dt] == nil {
							run.renames[dt] = map[string]string{}
						}
						run.renames[dt][swNorm(raw[f])] = id
					}
				}
				row.do = func() error {
					return run.c.do("/api/v1/doc/{doctype}/{id}", http.MethodPost, "/api/v1/doc/"+url.PathEscape(dt)+"/"+url.PathEscape(id),
						map[string]string{"doctype": dt, "id": id}, payload, handleGenericDoc)
				}
			}
		}
		out = append(out, row)
	}
	return out
}

// matchDoc finds the record a row is about: by its code when the workbook
// came from this organisation (or the code is typed, as an HSN number),
// otherwise by the sheet's natural keys. A record whose higher-priority key
// differs from the row's is never matched by a lower one - a vendor with
// another GSTIN is another vendor, whatever its name.
func (run *swRun) matchDoc(sh swSheet, idx *swDocIndex, vals map[string]interface{}, raw map[string]string, typed bool, label func(string) string) (string, string, error) {
	if code := raw["code"]; code != "" && (typed || run.sameOrg) {
		if n, ok := idx.byID[strings.ToLower(code)]; ok {
			return idx.recs[n].ID, "Code " + code, nil
		}
		for _, n := range idx.byText[swNorm(code)] {
			if strings.EqualFold(swText(idx.recs[n].Data["code"]), code) {
				return idx.recs[n].ID, "Code " + code, nil
			}
		}
		// Not found by code: the natural keys below decide (an HSN code is
		// stored as HSN-7117, so its number may still find it).
	}
	keyOf := func(f string) string {
		if v, ok := vals[f]; ok {
			return swNorm(swText(v))
		}
		return ""
	}
	var described []string
	firstKey := ""
	for ai, alt := range sh.Keys {
		var parts, names []string
		usable := true
		for _, f := range alt {
			names = append(names, label(f))
			if keyOf(f) == "" && !sh.BlankKey {
				usable = false
				break
			}
			shown := raw[f]
			if shown == "" {
				shown = "(blank)"
			}
			parts = append(parts, label(f)+" "+shown)
		}
		described = append(described, strings.Join(names, " + "))
		if !usable {
			continue
		}
		keyText := strings.Join(parts, ", ")
		if firstKey == "" {
			firstKey = keyText
		}
		var hits []int
		for n, rec := range idx.recs {
			ok := true
			for _, f := range alt {
				if swNorm(swText(rec.Data[f])) != keyOf(f) {
					ok = false
					break
				}
			}
			// A higher-priority single key that both carry, and that differs.
			for _, prev := range sh.Keys[:ai] {
				if len(prev) == 1 && ok {
					rv, mv := keyOf(prev[0]), swNorm(swText(rec.Data[prev[0]]))
					if rv != "" && mv != "" && rv != mv {
						ok = false
					}
				}
			}
			if ok {
				hits = append(hits, n)
			}
		}
		if len(hits) == 1 {
			return idx.recs[hits[0]].ID, keyText, nil
		}
		if len(hits) > 1 {
			return "", keyText, fmt.Errorf("%d existing records match %s - download this organisation's setup and use the Code column to say which", len(hits), keyText)
		}
	}
	if firstKey == "" {
		return "", "", fmt.Errorf("fill in %s so the row can be matched", strings.Join(described, " or "))
	}
	return "", firstKey, nil
}

// ---------------------------------------------------------------- settings

// swSettingShort is an option's label up to " - ", the words a person types.
func swSettingShort(label string) string {
	if i := strings.Index(label, " - "); i > 0 {
		return strings.TrimSpace(label[:i])
	}
	return label
}

func swSettingValue(def engines.SettingDefinition, s string) (string, error) {
	switch def.Type {
	case engines.SettingTypeBool:
		b, ok := swYesNo(s)
		if !ok {
			return "", fmt.Errorf("write Yes or No")
		}
		return strconv.FormatBool(b), nil
	case engines.SettingTypeSelect:
		for _, o := range def.Options {
			if swNorm(o.Value) == swNorm(s) || swNorm(o.Label) == swNorm(s) || swNorm(swSettingShort(o.Label)) == swNorm(s) {
				return o.Value, nil
			}
		}
		var names []string
		for _, o := range def.Options {
			names = append(names, swSettingShort(o.Label))
		}
		return "", fmt.Errorf("must be one of: %s", strings.Join(names, ", "))
	case engines.SettingTypeInt, engines.SettingTypeFloat:
		n, err := swNumber(s)
		if err != nil {
			return "", err
		}
		return swNumText(n), nil
	}
	return s, nil
}

func swSettingDisplay(def engines.SettingDefinition, v string) string {
	switch def.Type {
	case engines.SettingTypeBool:
		b, _ := strconv.ParseBool(v)
		return swYesNoText(b)
	case engines.SettingTypeSelect:
		for _, o := range def.Options {
			if o.Value == v {
				return swSettingShort(o.Label)
			}
		}
	}
	return v
}

type swIndustry struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func (run *swRun) industries() []swIndustry {
	var list []swIndustry
	if status, out := run.c.call("GET /api/v1/admin/industries", http.MethodGet, "/api/v1/admin/industries", nil, nil, handleGetIndustries); status == http.StatusOK {
		_ = json.Unmarshal(out, &list)
	}
	return list
}

func swIndustryMatch(list []swIndustry, s string) (swIndustry, bool) {
	want := strings.ReplaceAll(swNorm(s), "jewellery", "jewelry")
	for _, ind := range list {
		name := swNorm(ind.Name)
		if want == swNorm(ind.Code) || want == name || want == strings.TrimSuffix(name, " industry") {
			return ind, true
		}
	}
	return swIndustry{}, false
}

func (run *swRun) planSettings(t *swTable) []*swRow {
	all := engines.ListSettingsWithValues(run.c.tenantID)
	var rows []*swRow
	var industry *swRow
	for _, in := range t.rows {
		key := t.get(in, "Key")
		name := t.get(in, "Setting")
		value := t.get(in, "Value")
		if value == "" {
			continue
		}
		shown := name
		if shown == "" {
			shown = key
		}
		if swNorm(key) == "industry" || (key == "" && swNorm(name) == "industry") {
			industry = run.planIndustry(in.Num, value)
			continue
		}
		var def *engines.SettingWithValue
		for i := range all {
			if (key != "" && strings.EqualFold(all[i].Key, key)) || (key == "" && swNorm(all[i].Label) == swNorm(name)) {
				def = &all[i]
				break
			}
		}
		if def == nil {
			rows = append(rows, swErrRow(in.Num, shown, "no setting called "+shown))
			continue
		}
		shown = def.Label
		v, err := swSettingValue(def.SettingDefinition, value)
		if err == nil {
			err = engines.ValidateSetting(def.Key, v)
		}
		if err != nil {
			rows = append(rows, swErrRow(in.Num, shown, err.Error()))
			continue
		}
		same := v == def.Value
		if def.Type == engines.SettingTypeInt || def.Type == engines.SettingTypeFloat {
			a, errA := swNumber(v)
			b, errB := swNumber(def.Value)
			same = same || (errA == nil && errB == nil && math.Abs(a-b) < 1e-9)
		}
		if same {
			rows = append(rows, &swRow{Row: in.Num, Key: shown, Action: "same"})
			continue
		}
		k := def.Key
		rows = append(rows, &swRow{Row: in.Num, Key: shown, Action: "change",
			Changes: []swChange{{Field: def.Label, From: swSettingDisplay(def.SettingDefinition, def.Value), To: swSettingDisplay(def.SettingDefinition, v)}},
			do: func() error {
				return run.c.do("PUT /api/v1/admin/settings", http.MethodPut, "/api/v1/admin/settings", nil, map[string]string{k: v}, handleUpdateSettings)
			}})
	}
	if industry != nil {
		rows = append([]*swRow{industry}, rows...)
	}
	return rows
}

// planIndustry sets the industry once; a set industry is locked and is
// changed only on Configuration → Industry, with a reason.
func (run *swRun) planIndustry(num int, value string) *swRow {
	ind, ok := swIndustryMatch(run.industries(), value)
	if !ok {
		var names []string
		for _, i := range run.industries() {
			names = append(names, strings.TrimSuffix(i.Name, " Industry"))
		}
		return swErrRow(num, "Industry", fmt.Sprintf("%q is not an industry - one of: %s", value, strings.Join(names, ", ")))
	}
	lock, err := engines.GetIndustryLock(run.c.tenantID)
	if err != nil {
		return swErrRow(num, "Industry", err.Error())
	}
	if lock.Locked {
		if strings.EqualFold(lock.IndustryCode, ind.Code) {
			return &swRow{Row: num, Key: "Industry", Action: "same"}
		}
		return swErrRow(num, "Industry", fmt.Sprintf("the industry is already %s and is locked - change it on Configuration → Industry, with a reason", lock.IndustryCode))
	}
	code := ind.Code
	run.industryPending = true
	return &swRow{Row: num, Key: "Industry", Action: "change", Changes: []swChange{{Field: "Industry", To: ind.Name}},
		do: func() error {
			return run.c.do("POST /api/v1/admin/industry", http.MethodPost, "/api/v1/admin/industry", nil, map[string]string{"industry_code": strings.ToLower(code)}, handleSwitchIndustry)
		}}
}

// ---------------------------------------------------------------- number series

var swRestartNames = [][2]string{{"ANNUAL", "Every financial year"}, {"MONTHLY", "Every month"}, {"NEVER", "Never"}}
var swYearFormatNames = [][2]string{{"", "26-27"}, {"FY_COMPACT", "2627"}, {"FY_LONG", "2026-27"}, {"START_YY", "26"}, {"START_YYYY", "2026"}}
var swYearPositionNames = [][2]string{{"", "Before the number"}, {engines.YearPositionEnd, "At the end"}}
var swSeparatorNames = [][2]string{{"/", "/"}, {"-", "-"}, {"", "None"}}

func swPick(pairs [][2]string, s string) (string, bool) {
	for _, p := range pairs {
		if swNorm(p[1]) == swNorm(s) || (p[0] != "" && swNorm(p[0]) == swNorm(s)) {
			return p[0], true
		}
	}
	return "", false
}

func swPickName(pairs [][2]string, v string) string {
	for _, p := range pairs {
		if strings.EqualFold(p[0], v) {
			return p[1]
		}
	}
	return v
}

func swPairNames(pairs [][2]string) []string {
	var out []string
	for _, p := range pairs {
		out = append(out, p[1])
	}
	return out
}

// swSeriesNames are the plain names Prefix Configurations shows for the
// purchase series (view-admin.js SERIES_TRANSACTION_NAMES); others show
// their key. Only for reading - the Series column is the key.
var swSeriesNames = map[string]string{
	"PR": "Purchase requisition", "PO": "Purchase order", "GRN": "Goods receipt", "ASN": "Advance shipping notice",
	"RFQ": "Request for quotation", "QTN": "Vendor quote", "PRT": "Purchase return", "DN": "Debit note",
	"VCN": "Vendor credit note", "LCV": "Landed cost voucher", "TO": "Transfer order", "TI": "Transfer in",
	"SI": "Sales invoice", "CN": "Credit note", "SO": "Sales order", "SR": "Sales return", "POSBILL": "POS bill",
	"ProductFamily": "Design", "Item": "Combination ID / item", "ItemName": "Item name", "GRNBatchSeq": "GRN batch number",
}

func (run *swRun) planSeries(t *swTable) []*swRow {
	all, err := engines.ListNumberSeries(run.c.tenantID)
	if err != nil {
		return []*swRow{swErrRow(0, "Number Series", err.Error())}
	}
	var rows []*swRow
	for _, in := range t.rows {
		key := t.get(in, "Series")
		if key == "" {
			rows = append(rows, swErrRow(in.Num, fmt.Sprintf("row %d", in.Num), "fill in Series (PO, GRN...)"))
			continue
		}
		var cur *engines.NumberSeries
		for i := range all {
			if strings.EqualFold(all[i].DocType, key) {
				cur = &all[i]
				break
			}
		}
		if cur == nil {
			rows = append(rows, swErrRow(in.Num, key, "no number series "+key+" - add it on Prefix Configs (New Series) first"))
			continue
		}
		next := *cur
		includeStore := cur.IncludeStore
		var changes []swChange
		var problems []string
		set := func(field, from, to string) {
			if from != to {
				changes = append(changes, swChange{Field: field, From: from, To: to})
			}
		}
		// Prefix and suffix are compared ignoring case, as the screen saves
		// what is typed in capitals; a record's own series keeps its stored
		// spelling ("Item") until a different prefix is typed.
		if s := t.get(in, "Prefix"); s != "" {
			if strings.EqualFold(s, swClear) {
				s = ""
			}
			if !strings.EqualFold(s, cur.Prefix) {
				s = strings.ToUpper(s)
				set("Prefix", cur.Prefix, s)
				next.Prefix = s
			}
		}
		if s := t.get(in, "Separator"); s != "" {
			if v, ok := swPick(swSeparatorNames, s); ok {
				set("Separator", swPickName(swSeparatorNames, cur.Separator), swPickName(swSeparatorNames, v))
				next.Separator = v
			} else {
				problems = append(problems, "Separator must be /, - or None")
			}
		}
		if s := t.get(in, "Number length"); s != "" {
			if n, err := swNumber(s); err == nil && n >= 1 && n <= 12 && n == math.Trunc(n) {
				set("Number length", strconv.Itoa(cur.PaddingWidth), swNumText(n))
				next.PaddingWidth = int(n)
			} else {
				problems = append(problems, "Number length must be a whole number of digits")
			}
		}
		if s := t.get(in, "Suffix"); s != "" {
			if strings.EqualFold(s, swClear) {
				s = ""
			}
			if !strings.EqualFold(s, cur.Suffix) {
				s = strings.ToUpper(s)
				set("Suffix", cur.Suffix, s)
				next.Suffix = s
			}
		}
		if s := t.get(in, "Restart"); s != "" {
			if v, ok := swPick(swRestartNames, s); ok {
				set("Restart", swPickName(swRestartNames, cur.ResetFrequency), swPickName(swRestartNames, v))
				next.ResetFrequency = v
			} else {
				problems = append(problems, "Restart must be one of: "+strings.Join(swPairNames(swRestartNames), ", "))
			}
		}
		if s := t.get(in, "Year format"); s != "" {
			if v, ok := swPick(swYearFormatNames, s); ok {
				set("Year format", swPickName(swYearFormatNames, cur.YearFormat), swPickName(swYearFormatNames, v))
				next.YearFormat = v
			} else {
				problems = append(problems, "Year format must be one of: "+strings.Join(swPairNames(swYearFormatNames), ", "))
			}
		}
		if s := t.get(in, "Year position"); s != "" {
			if v, ok := swPick(swYearPositionNames, s); ok {
				set("Year position", swPickName(swYearPositionNames, cur.YearPosition), swPickName(swYearPositionNames, v))
				next.YearPosition = v
			} else {
				problems = append(problems, "Year position must be Before the number or At the end")
			}
		}
		if s := t.get(in, "Store in number"); s != "" {
			if b, ok := swYesNo(s); ok {
				set("Store in number", swYesNoText(cur.IncludeStore), swYesNoText(b))
				includeStore = b
			} else {
				problems = append(problems, "Store in number must be Yes or No")
			}
		}
		if s := t.get(in, "Status"); s != "" {
			switch swNorm(s) {
			case "active":
				set("Status", swActive(cur.ActiveStatus), "Active")
				next.ActiveStatus = true
			case "inactive":
				set("Status", swActive(cur.ActiveStatus), "Inactive")
				next.ActiveStatus = false
			default:
				problems = append(problems, "Status must be Active or Inactive")
			}
		}
		if len(problems) > 0 {
			rows = append(rows, swErrRow(in.Num, key, strings.Join(problems, "; ")))
			continue
		}
		if len(changes) == 0 {
			rows = append(rows, &swRow{Row: in.Num, Key: key, Action: "same"})
			continue
		}
		body := map[string]interface{}{
			"doc_type": next.DocType, "prefix": next.Prefix, "suffix": next.Suffix, "separator": next.Separator,
			"padding_width": next.PaddingWidth, "reset_frequency": next.ResetFrequency, "active_status": next.ActiveStatus,
			"include_store": includeStore, "year_format": next.YearFormat, "year_position": next.YearPosition,
		}
		rows = append(rows, &swRow{Row: in.Num, Key: key, Action: "change", Changes: changes, do: func() error {
			return run.c.do("/api/v1/prefix", http.MethodPost, "/api/v1/prefix", nil, body, handlePrefix)
		}})
	}
	return rows
}

func swActive(b bool) string {
	if b {
		return "Active"
	}
	return "Inactive"
}

// ---------------------------------------------------------------- item fields

var swGRNAskNames = [][2]string{{engines.GRNAskOff, "No"}, {engines.GRNAskOptional, "Optional"}, {engines.GRNAskRequired, "Required"}}

func (run *swRun) attributeName(code string) string {
	switch code {
	case "":
		return ""
	case "-":
		return "Typed"
	}
	return run.display("ItemAttribute", code)
}

func (run *swRun) planItemFields(t *swTable) []*swRow {
	rules, err := engines.ItemFieldRules(run.c.tenantID)
	if err != nil {
		return []*swRow{swErrRow(0, "Item Fields", err.Error())}
	}
	merged := make([]engines.ItemFieldRule, len(rules))
	copy(merged, rules)
	var rows []*swRow
	var changed []*swRow
	for _, in := range t.rows {
		key, name := t.get(in, "Key"), t.get(in, "Field")
		shown := name
		if shown == "" {
			shown = key
		}
		at := -1
		for i := range merged {
			if (key != "" && strings.EqualFold(merged[i].Key, key)) || (key == "" && swNorm(merged[i].Label) == swNorm(name)) {
				at = i
				break
			}
		}
		if at < 0 {
			if run.industryPending && !run.apply {
				rows = append(rows, &swRow{Row: in.Num, Key: shown, Action: "note", Message: "checked when applied - the industry this workbook sets adds its own item fields"})
				continue
			}
			rows = append(rows, swErrRow(in.Num, shown, "no item field called "+shown))
			continue
		}
		r := &merged[at]
		shown = r.Label
		var changes []swChange
		var problems []string
		flag := func(col string, cur *bool) {
			s := t.get(in, col)
			if s == "" {
				return
			}
			b, ok := swYesNo(s)
			if !ok {
				problems = append(problems, col+" must be Yes or No")
				return
			}
			if b != *cur {
				changes = append(changes, swChange{Field: col, From: swYesNoText(*cur), To: swYesNoText(b)})
				*cur = b
			}
		}
		if !r.Fixed {
			flag("On the item", &r.Item)
		} else if s := t.get(in, "On the item"); s != "" {
			if b, ok := swYesNo(s); ok && !b {
				problems = append(problems, r.Label+" is always on the item")
			}
		}
		if r.Kind == "attribute" {
			flag("In the PO", &r.PO)
		}
		flag("Hidden", &r.Hidden)
		if s := t.get(in, "At the GRN"); s != "" {
			if v, ok := swPick(swGRNAskNames, s); ok {
				if v != r.GRN {
					changes = append(changes, swChange{Field: "At the GRN", From: swPickName(swGRNAskNames, r.GRN), To: swPickName(swGRNAskNames, v)})
					r.GRN = v
				}
			} else {
				problems = append(problems, "At the GRN must be No, Optional or Required")
			}
		}
		if s := t.get(in, "Values from"); s != "" && r.Kind == "field" {
			v := ""
			switch {
			case strings.EqualFold(s, "typed"):
				v = "-"
			case strings.EqualFold(s, swClear), strings.EqualFold(s, "automatic"):
				v = ""
			default:
				id, _, err := run.resolveLink("ItemAttribute", s)
				if err != nil {
					problems = append(problems, "Values from: "+err.Error())
				} else {
					v = swText(run.index("ItemAttribute").recs[run.index("ItemAttribute").byID[strings.ToLower(id)]].Data["code"])
					if v == "" {
						v = id
					}
				}
			}
			if len(problems) == 0 && v != r.List {
				changes = append(changes, swChange{Field: "Values from", From: run.attributeName(r.List), To: run.attributeName(v)})
				r.List = v
			}
		}
		if len(problems) > 0 {
			rows = append(rows, swErrRow(in.Num, shown, strings.Join(problems, "; ")))
			continue
		}
		if len(changes) == 0 {
			rows = append(rows, &swRow{Row: in.Num, Key: shown, Action: "same"})
			continue
		}
		row := &swRow{Row: in.Num, Key: shown, Action: "change", Changes: changes}
		rows = append(rows, row)
		changed = append(changed, row)
	}
	if len(changed) > 0 {
		save := swOnce(func() error {
			return run.c.do("PUT /api/v1/inventory/item-setup/fields", http.MethodPut, "/api/v1/inventory/item-setup/fields", nil,
				map[string]interface{}{"fields": merged}, handleSaveItemFieldRules)
		})
		for _, r := range changed {
			r.do = save
		}
	}
	return rows
}

// ---------------------------------------------------------------- roles

var swRoleStoreNames = [][2]string{{engines.StoreAccessAll, "All stores"}, {engines.StoreAccessSelected, "Selected stores"}, {engines.StoreAccessHome, "Home store only"}}
var swUserStoreNames = [][2]string{{engines.StoreAccessInherit, "Same as role"}, {engines.StoreAccessAll, "All stores"}, {engines.StoreAccessSelected, "Only these stores"}, {engines.StoreAccessHome, "Home store only"}}

// stores resolves a comma-separated list of store names or short codes.
func (run *swRun) stores(list string) ([]string, error) {
	var out []string
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		// A store this workbook creates is pending in the preview; when
		// applied, the Locations sheet has run and it resolves to its id.
		id, _, err := run.resolveLink("Location", s)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (run *swRun) storeNames(ids []string) string {
	var names []string
	for _, id := range ids {
		names = append(names, run.display("Location", id))
	}
	return strings.Join(names, ", ")
}

func swSameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if !strings.EqualFold(x[i], y[i]) {
			return false
		}
	}
	return true
}

func (run *swRun) roleNames() map[string]string {
	if run.roles == nil {
		run.roles = map[string]string{}
		if names, err := tenantRoleNames(run.schema); err == nil {
			for _, n := range names {
				run.roles[swNorm(n)] = n
			}
		}
	}
	return run.roles
}

func (run *swRun) planRoles(t *swTable) []*swRow {
	known := run.roleNames()
	var rows []*swRow
	for _, in := range t.rows {
		name := strings.Join(strings.Fields(t.get(in, "Role")), " ")
		if name == "" {
			rows = append(rows, swErrRow(in.Num, fmt.Sprintf("row %d", in.Num), "fill in Role"))
			continue
		}
		if engines.HasFullAccess(name) {
			rows = append(rows, &swRow{Row: in.Num, Key: name, Action: "note", Message: name + " is built in and always has full access to every store"})
			continue
		}
		access, stores := "", []string(nil)
		var problems []string
		if s := t.get(in, "Stores"); s != "" {
			v, ok := swPick(swRoleStoreNames, s)
			if !ok {
				problems = append(problems, "Stores must be one of: "+strings.Join(swPairNames(swRoleStoreNames), ", "))
			}
			access = v
		}
		if s := t.get(in, "Selected stores"); s != "" {
			list, err := run.stores(s)
			if err != nil {
				problems = append(problems, err.Error())
			}
			stores = list
			if access == "" {
				access = engines.StoreAccessSelected
			}
		}
		if len(problems) > 0 {
			rows = append(rows, swErrRow(in.Num, name, strings.Join(problems, "; ")))
			continue
		}
		existing, exists := known[swNorm(name)]
		row := &swRow{Row: in.Num, Key: name}
		var steps []func() error
		if !exists {
			row.Action, row.creates = "new", []string{name}
			known[swNorm(name)] = name
			desc := t.get(in, "Description")
			row.Changes = append(row.Changes, swChange{Field: "Role", To: name})
			steps = append(steps, func() error {
				return run.c.do("POST /api/v1/admin/roles", http.MethodPost, "/api/v1/admin/roles", nil, map[string]string{"name": name, "description": desc}, handleCreateRole)
			})
		} else {
			name = existing
		}
		if access != "" {
			cur, _ := engines.GetRoleStoreAccess(run.c.tenantID, name)
			if !exists || cur.Access != access || (access == engines.StoreAccessSelected && !swSameSet(cur.Stores, stores)) {
				to := swPickName(swRoleStoreNames, access)
				if access == engines.StoreAccessSelected {
					to += ": " + run.storeNames(stores)
				}
				from := ""
				if exists {
					from = swPickName(swRoleStoreNames, cur.Access)
				}
				row.Changes = append(row.Changes, swChange{Field: "Stores", From: from, To: to})
				role, acc, list := name, access, stores
				steps = append(steps, func() error {
					return run.c.do("POST /api/v1/admin/roles/stores", http.MethodPost, "/api/v1/admin/roles/stores", nil,
						map[string]interface{}{"role": role, "access": acc, "stores": list}, handleRoleStores)
				})
			}
		}
		if row.Action == "" {
			if len(steps) == 0 {
				row.Action = "same"
			} else {
				row.Action = "change"
			}
		}
		if len(steps) > 0 {
			row.do = func() error {
				for _, s := range steps {
					if err := s(); err != nil {
						return err
					}
				}
				return nil
			}
		}
		rows = append(rows, row)
	}
	return rows
}

type swGrant struct {
	Read, Create, Update, Delete bool
}

func (run *swRun) grants() map[string]map[string]swGrant {
	out := map[string]map[string]swGrant{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete FROM %s.role_permissions`, run.schema))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var role, dt string
		var g swGrant
		if rows.Scan(&role, &dt, &g.Read, &g.Create, &g.Update, &g.Delete) == nil {
			if out[swNorm(role)] == nil {
				out[swNorm(role)] = map[string]swGrant{}
			}
			out[swNorm(role)][dt] = g
		}
	}
	return out
}

// doctypeModules maps each record type to its module.
func (run *swRun) doctypeModules() map[string]string {
	out := map[string]string{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT name, COALESCE(module, '') FROM %s.doctype_meta`, run.schema))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var name, module string
		if rows.Scan(&name, &module) == nil {
			out[name] = module
		}
	}
	return out
}

// swDoctypeKey compares record-type names as people write them:
// "Purchase Order" finds PurchaseOrder.
func swDoctypeKey(s string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s))
}

// swDoctypeLabel spells a record type for a person: PurchaseOrder reads
// "Purchase Order", HSNCode "HSN Code".
func swDoctypeLabel(dt string) string {
	var b strings.Builder
	rs := []rune(dt)
	for i, r := range rs {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prevLower := rs[i-1] >= 'a' && rs[i-1] <= 'z'
			nextLower := i+1 < len(rs) && rs[i+1] >= 'a' && rs[i+1] <= 'z'
			prevUpper := rs[i-1] >= 'A' && rs[i-1] <= 'Z'
			if prevLower || (prevUpper && nextLower) {
				b.WriteRune(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (run *swRun) planRoleAccess(t *swTable) []*swRow {
	known := run.roleNames()
	current := run.grants()
	modules := run.doctypeModules()
	type plan struct {
		role   string
		grants map[string]swGrant
	}
	plans := map[string]*plan{}
	var order []string
	var rows []*swRow
	for _, in := range t.rows {
		roleIn, target := t.get(in, "Role"), t.get(in, "Access to")
		key := roleIn + " · " + target
		if roleIn == "" || target == "" {
			rows = append(rows, swErrRow(in.Num, key, "fill in Role and Access to"))
			continue
		}
		if engines.HasFullAccess(roleIn) {
			rows = append(rows, &swRow{Row: in.Num, Key: key, Action: "note", Message: roleIn + " always has full access"})
			continue
		}
		role, ok := known[swNorm(roleIn)]
		if !ok {
			rows = append(rows, swErrRow(in.Num, key, "no role called "+roleIn+" - add it on the Roles sheet"))
			continue
		}
		var doctypes []string
		for dt, m := range modules {
			if swNorm(m) == swNorm(target) {
				doctypes = append(doctypes, dt)
			}
		}
		if len(doctypes) == 0 {
			for dt := range modules {
				if swDoctypeKey(dt) == swDoctypeKey(target) || swDoctypeKey(swDoctypeLabel(dt)) == swDoctypeKey(target) {
					doctypes = append(doctypes, dt)
				}
			}
		}
		if len(doctypes) == 0 {
			rows = append(rows, swErrRow(in.Num, key, fmt.Sprintf("%q is neither a module nor a record type", target)))
			continue
		}
		sort.Strings(doctypes)
		flags := map[string]*bool{}
		var problems []string
		for _, col := range []string{"Read", "Create", "Update", "Delete"} {
			if s := t.get(in, col); s != "" {
				b, ok := swYesNo(s)
				if !ok {
					problems = append(problems, col+" must be Yes or No")
					continue
				}
				flags[col] = &b
			}
		}
		if len(problems) > 0 {
			rows = append(rows, swErrRow(in.Num, key, strings.Join(problems, "; ")))
			continue
		}
		p := plans[swNorm(role)]
		if p == nil {
			p = &plan{role: role, grants: map[string]swGrant{}}
			plans[swNorm(role)] = p
			order = append(order, swNorm(role))
		}
		changedTypes := 0
		var changes []swChange
		for _, dt := range doctypes {
			cur, had := p.grants[dt]
			if !had {
				cur = current[swNorm(role)][dt]
			}
			next := cur
			if f := flags["Read"]; f != nil {
				next.Read = *f
			}
			if f := flags["Create"]; f != nil {
				next.Create = *f
			}
			if f := flags["Update"]; f != nil {
				next.Update = *f
			}
			if f := flags["Delete"]; f != nil {
				next.Delete = *f
			}
			// As on the Roles screen: create, update or delete given without a
			// Read cell includes read, and Read = No with nothing else given
			// takes everything away. A row that spells all four out is kept
			// exactly as written - an exported grant reads back unchanged.
			cud := flags["Create"] != nil && *flags["Create"] || flags["Update"] != nil && *flags["Update"] || flags["Delete"] != nil && *flags["Delete"]
			if flags["Read"] == nil && (next.Create || next.Update || next.Delete) {
				next.Read = true
			}
			if f := flags["Read"]; f != nil && !*f && !cud {
				next = swGrant{}
			}
			p.grants[dt] = next
			if next != current[swNorm(role)][dt] {
				changedTypes++
			}
		}
		for _, col := range []string{"Read", "Create", "Update", "Delete"} {
			if f := flags[col]; f != nil {
				changes = append(changes, swChange{Field: col, To: swYesNoText(*f)})
			}
		}
		row := &swRow{Row: in.Num, Key: key}
		if changedTypes == 0 {
			row.Action = "same"
		} else {
			row.Action = "change"
			row.Changes = changes
			row.Message = fmt.Sprintf("%d record type(s) change", changedTypes)
		}
		rows = append(rows, row)
	}
	for _, k := range order {
		p := plans[k]
		var grants []map[string]interface{}
		for dt, g := range p.grants {
			if g != current[k][dt] {
				grants = append(grants, map[string]interface{}{"doctype_name": dt, "allow_read": g.Read, "allow_create": g.Create, "allow_update": g.Update, "allow_delete": g.Delete})
			}
		}
		if len(grants) == 0 {
			continue
		}
		role := p.role
		save := swOnce(func() error {
			return run.c.do("POST /api/v1/admin/role-permissions", http.MethodPost, "/api/v1/admin/role-permissions", nil,
				map[string]interface{}{"role": role, "grants": grants}, handleRolePermissions)
		})
		for _, r := range rows {
			if r.Action == "change" && strings.HasPrefix(swNorm(r.Key), k+" ·") {
				r.do = save
			}
		}
	}
	return rows
}

// ---------------------------------------------------------------- users

type swUser struct {
	ID           string   `json:"id"`
	Username     string   `json:"username"`
	Email        string   `json:"email"`
	Role         string   `json:"role"`
	Status       string   `json:"status"`
	LocationCode string   `json:"location_code"`
	StoreAccess  string   `json:"store_access"`
	Stores       []string `json:"stores"`
	Department   string   `json:"department"`
}

func (run *swRun) users() []swUser {
	var list []swUser
	if status, out := run.c.call("GET /api/v1/admin/users", http.MethodGet, "/api/v1/admin/users", nil, nil, handleListUsers); status == http.StatusOK {
		_ = json.Unmarshal(out, &list)
	}
	return list
}

func (run *swRun) planUsers(t *swTable) []*swRow {
	users := run.users()
	known := run.roleNames()
	var rows []*swRow
	for _, in := range t.rows {
		username := t.get(in, "Username")
		if username == "" {
			rows = append(rows, swErrRow(in.Num, fmt.Sprintf("row %d", in.Num), "fill in Username"))
			continue
		}
		var cur *swUser
		for i := range users {
			if strings.EqualFold(users[i].Username, username) {
				cur = &users[i]
				break
			}
		}
		var problems []string
		role := t.get(in, "Role")
		if role != "" {
			// Giving Super Admin is refused; a user who already holds it is
			// only listed (an exported workbook carries them).
			if engines.IsSuperAdmin(role) && (cur == nil || !engines.IsSuperAdmin(cur.Role)) {
				problems = append(problems, "Super Admin is kept for the platform operator - give this person the Admin role")
			} else if r, ok := known[swNorm(role)]; ok {
				role = r
			} else if !engines.HasFullAccess(role) {
				problems = append(problems, "no role called "+role+" - add it on the Roles sheet")
			}
		}
		home := ""
		if s := t.get(in, "Home store"); s != "" {
			id, pending, err := run.resolveLink("Location", s)
			switch {
			case err != nil:
				problems = append(problems, "Home store: "+err.Error())
			case pending:
				home = s
			default:
				home = id
			}
		}
		dept := t.get(in, "Department")
		if dept != "" {
			if _, _, err := run.resolveLink("Department", dept); err != nil {
				problems = append(problems, "Department: "+err.Error())
			}
		}
		access, stores := "", []string(nil)
		if s := t.get(in, "Store access"); s != "" {
			v, ok := swPick(swUserStoreNames, s)
			if !ok {
				problems = append(problems, "Store access must be one of: "+strings.Join(swPairNames(swUserStoreNames), ", "))
			}
			access = v
		}
		if s := t.get(in, "Stores"); s != "" {
			list, err := run.stores(s)
			if err != nil {
				problems = append(problems, err.Error())
			}
			stores = list
			if access == "" {
				access = engines.StoreAccessSelected
			}
		}
		status := ""
		if s := t.get(in, "Status"); s != "" {
			switch swNorm(s) {
			case "active":
				status = "Active"
			case "inactive":
				status = "Inactive"
			default:
				problems = append(problems, "Status must be Active or Inactive")
			}
		}
		password := t.get(in, "First password")
		if len(problems) > 0 {
			rows = append(rows, swErrRow(in.Num, username, strings.Join(problems, "; ")))
			continue
		}
		row := &swRow{Row: in.Num, Key: username}
		var steps []func() error
		id := ""
		if cur == nil {
			if password == "" || role == "" {
				rows = append(rows, swErrRow(in.Num, username, "a new user needs Role and First password"))
				continue
			}
			row.Action = "new"
			row.Changes = []swChange{{Field: "Role", To: role}}
			if home != "" {
				row.Changes = append(row.Changes, swChange{Field: "Home store", To: t.get(in, "Home store")})
			}
			if dept != "" {
				row.Changes = append(row.Changes, swChange{Field: "Department", To: dept})
			}
			row.Message = "Signs in with the first password and must set their own."
			body := map[string]string{"username": username, "password": password, "email": t.get(in, "Email"), "role": role, "location_code": home, "department": dept}
			steps = append(steps, func() error {
				return run.c.do("POST /api/v1/admin/users", http.MethodPost, "/api/v1/admin/users", nil, body, handleCreateUser)
			})
			// The new user's id is the username (handleCreateUser).
			id = strings.TrimSpace(username)
		} else {
			id = cur.ID
			var notes []string
			if password != "" {
				notes = append(notes, "First password ignored - an existing user's password is never changed")
			}
			if role != "" && !strings.EqualFold(role, cur.Role) {
				notes = append(notes, fmt.Sprintf("role is %s; the workbook says %s - roles of existing users are changed on the Users screen", cur.Role, role))
			}
			if e := t.get(in, "Email"); e != "" && !strings.EqualFold(e, cur.Email) {
				notes = append(notes, "email differs - change it on the Users screen")
			}
			deptChanged := dept != "" && !strings.EqualFold(dept, cur.Department) && !strings.EqualFold(dept, run.display("Department", cur.Department))
			if (home != "" && !strings.EqualFold(home, cur.LocationCode)) || deptChanged {
				loc := cur.LocationCode
				if home != "" {
					loc = home
					if !strings.EqualFold(home, cur.LocationCode) {
						row.Changes = append(row.Changes, swChange{Field: "Home store", From: run.display("Location", cur.LocationCode), To: t.get(in, "Home store")})
					}
				}
				body := map[string]interface{}{"id": id, "location_code": loc}
				if deptChanged {
					d := dept
					body["department"] = d
					row.Changes = append(row.Changes, swChange{Field: "Department", From: run.display("Department", cur.Department), To: dept})
				}
				steps = append(steps, func() error {
					return run.c.do("POST /api/v1/admin/users/location", http.MethodPost, "/api/v1/admin/users/location", nil, body, handleSetUserLocation)
				})
			}
			if len(notes) > 0 {
				row.Message = strings.Join(notes, "; ")
			}
		}
		if access != "" {
			if cur == nil || cur.StoreAccess != access || (access == engines.StoreAccessSelected && !swSameSet(cur.Stores, stores)) {
				to := swPickName(swUserStoreNames, access)
				if access == engines.StoreAccessSelected {
					to += ": " + run.storeNames(stores)
				}
				from := ""
				if cur != nil {
					from = swPickName(swUserStoreNames, cur.StoreAccess)
				}
				row.Changes = append(row.Changes, swChange{Field: "Store access", From: from, To: to})
				uid, acc, list := id, access, stores
				steps = append(steps, func() error {
					return run.c.do("POST /api/v1/admin/users/stores", http.MethodPost, "/api/v1/admin/users/stores", nil,
						map[string]interface{}{"id": uid, "access": acc, "stores": list}, handleUserStores)
				})
			}
		}
		if status != "" && (cur == nil && status == "Inactive" || cur != nil && !strings.EqualFold(cur.Status, status)) {
			from := "Active"
			if cur != nil {
				from = cur.Status
			}
			row.Changes = append(row.Changes, swChange{Field: "Status", From: from, To: status})
			uid, st := id, status
			steps = append(steps, func() error {
				return run.c.do("POST /api/v1/admin/users/status", http.MethodPost, "/api/v1/admin/users/status", nil, map[string]string{"id": uid, "status": st}, handleSetUserStatus)
			})
		}
		if row.Action == "" {
			switch {
			case len(steps) > 0:
				row.Action = "change"
			case row.Message != "":
				row.Action = "note"
			default:
				row.Action = "same"
			}
		}
		if len(steps) > 0 {
			row.do = func() error {
				for _, s := range steps {
					if err := s(); err != nil {
						return err
					}
				}
				return nil
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// ---------------------------------------------------------------- approval rules

func swBandText(min float64, max *float64, role string) string {
	if max == nil {
		return fmt.Sprintf("%s and above → %s", swNumText(min), role)
	}
	return fmt.Sprintf("%s – %s → %s", swNumText(min), swNumText(*max), role)
}

func (run *swRun) planApproval(t *swTable) []*swRow {
	existing, err := engines.GetApprovalRules(run.c.tenantID)
	if err != nil {
		return []*swRow{swErrRow(0, "Approval Rules", err.Error())}
	}
	modules := run.doctypeModules()
	known := run.roleNames()
	type band struct {
		row  int
		min  float64
		max  *float64
		role string
	}
	groups := map[string][]band{}
	var order []string
	var rows []*swRow
	for _, in := range t.rows {
		target := t.get(in, "Record type")
		doctype := ""
		for dt := range modules {
			if swDoctypeKey(dt) == swDoctypeKey(target) || swDoctypeKey(swDoctypeLabel(dt)) == swDoctypeKey(target) {
				doctype = dt
				break
			}
		}
		for _, r := range existing {
			if doctype == "" && swDoctypeKey(r.Doctype) == swDoctypeKey(target) {
				doctype = r.Doctype
			}
		}
		key := target
		var problems []string
		if doctype == "" {
			problems = append(problems, fmt.Sprintf("%q is not a record type", target))
		}
		min, err := swNumber(t.get(in, "From amount"))
		if err != nil {
			problems = append(problems, "From amount: "+err.Error())
		}
		var max *float64
		if s := t.get(in, "To amount"); s != "" {
			m, err := swNumber(s)
			if err != nil {
				problems = append(problems, "To amount: "+err.Error())
			} else {
				max = &m
			}
		}
		role := t.get(in, "Approved by")
		if r, ok := known[swNorm(role)]; ok {
			role = r
		} else if !engines.HasFullAccess(role) {
			problems = append(problems, "no role called "+role)
		}
		if max != nil && *max < min {
			problems = append(problems, "To amount is below From amount")
		}
		if len(problems) > 0 {
			rows = append(rows, swErrRow(in.Num, key, strings.Join(problems, "; ")))
			continue
		}
		if _, ok := groups[doctype]; !ok {
			order = append(order, doctype)
		}
		groups[doctype] = append(groups[doctype], band{row: in.Num, min: min, max: max, role: role})
	}
	for _, dt := range order {
		bands := groups[dt]
		sort.Slice(bands, func(i, j int) bool { return bands[i].min < bands[j].min })
		var problem string
		var gaps []string
		for i := 1; i < len(bands); i++ {
			prev := bands[i-1]
			if prev.max == nil || *prev.max >= bands[i].min {
				problem = fmt.Sprintf("the bands from %s and from %s overlap", swNumText(prev.min), swNumText(bands[i].min))
				break
			}
			if bands[i].min-*prev.max > 1 {
				gaps = append(gaps, fmt.Sprintf("%s–%s", swNumText(*prev.max), swNumText(bands[i].min)))
			}
		}
		var cur []engines.ApprovalRule
		for _, r := range existing {
			if r.Doctype == dt {
				cur = append(cur, r)
			}
		}
		sort.Slice(cur, func(i, j int) bool { return cur[i].MinAmount < cur[j].MinAmount })
		var fromText, toText []string
		for _, r := range cur {
			fromText = append(fromText, swBandText(r.MinAmount, r.MaxAmount, r.RequiredRole))
		}
		for _, b := range bands {
			toText = append(toText, swBandText(b.min, b.max, b.role))
		}
		same := len(cur) == len(bands)
		for i := 0; same && i < len(bands); i++ {
			a, b := cur[i], bands[i]
			same = math.Abs(a.MinAmount-b.min) < 1e-9 && strings.EqualFold(a.RequiredRole, b.role) &&
				((a.MaxAmount == nil && b.max == nil) || (a.MaxAmount != nil && b.max != nil && math.Abs(*a.MaxAmount-*b.max) < 1e-9))
		}
		label := swDoctypeLabel(dt)
		var save func() error
		if problem == "" && !same {
			doctype, newBands, oldBands := dt, bands, cur
			save = swOnce(func() error {
				del := func(id int) error {
					return run.c.do("/api/v1/approval/rules", http.MethodDelete, "/api/v1/approval/rules?id="+strconv.Itoa(id), nil, nil, handleApprovalRules)
				}
				add := func(min float64, max *float64, role string) error {
					return run.c.do("/api/v1/approval/rules", http.MethodPost, "/api/v1/approval/rules", nil,
						map[string]interface{}{"doctype": doctype, "min_amount": min, "max_amount": max, "required_role": role}, handleApprovalRules)
				}
				for _, r := range oldBands {
					if err := del(r.ID); err != nil {
						return err
					}
				}
				for _, b := range newBands {
					if err := add(b.min, b.max, b.role); err != nil {
						// Put the bands back as they were, so the record type is
						// never left without its approvals.
						if rules, e := engines.GetApprovalRules(run.c.tenantID); e == nil {
							for _, r := range rules {
								if r.Doctype == doctype {
									_ = del(r.ID)
								}
							}
						}
						for _, r := range oldBands {
							_ = add(r.MinAmount, r.MaxAmount, r.RequiredRole)
						}
						return fmt.Errorf("%v - the earlier bands were put back", err)
					}
				}
				return nil
			})
		}
		for i, b := range bands {
			row := &swRow{Row: b.row, Key: label + ": " + swBandText(b.min, b.max, b.role)}
			switch {
			case problem != "":
				row.Action, row.Message = "error", problem
			case same:
				row.Action = "same"
			default:
				row.Action, row.do = "change", save
				if i == 0 {
					row.Changes = []swChange{{Field: label + " bands", From: strings.Join(fromText, "; "), To: strings.Join(toText, "; ")}}
					if len(gaps) > 0 {
						row.Message = "Amounts between " + strings.Join(gaps, ", ") + " have no approver - a document there cannot be submitted."
					}
				}
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// ---------------------------------------------------------------- handlers

// swHeaderWords are the column names each sheet is recognised by.
func swHeaderWords(tenantID string) map[string][]string {
	out := map[string][]string{}
	for _, sh := range setupWorkbookSheets {
		var words []string
		switch sh.Kind {
		case swDoc:
			if meta, err := engines.GetDocTypeMeta(tenantID, sh.Doctype); err == nil {
				for _, f := range swDocFields(sh, meta) {
					words = append(words, f.Label, f.Fieldname)
				}
			}
		case swSettings:
			words = []string{"Setting", "Key", "Value"}
		case swSeries:
			words = []string{"Series", "Prefix"}
		case swItemFields:
			words = []string{"Field", "Key", "At the GRN"}
		case swRoles:
			words = []string{"Role", "Stores"}
		case swRoleAccess:
			words = []string{"Access to", "Read"}
		case swUsers:
			words = []string{"Username", "First password"}
		case swApproval:
			words = []string{"Record type", "Approved by"}
		}
		out[swNorm(sh.Name)] = words
	}
	return out
}

func swReadUpload(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, swMaxUpload+(1<<20))
	if err := r.ParseMultipartForm(swMaxUpload); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Upload the workbook as an .xlsx file of at most 8 MB.")
		return nil, "", false
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose the workbook to upload.")
		return nil, "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, swMaxUpload+1))
	if err != nil || len(data) > swMaxUpload {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The workbook is larger than 8 MB.")
		return nil, "", false
	}
	return data, hdr.Filename, true
}

type swSummary struct {
	New, Change, Same, Errors, Notes, Done, Failed int
}

func swTotals(results []*swSheetResult) swSummary {
	var s swSummary
	for _, r := range results {
		s.New += r.New
		s.Change += r.Change
		s.Same += r.Same
		s.Errors += r.Errors
		s.Notes += r.Notes
		s.Done += r.Done
		s.Failed += r.Failed
	}
	return s
}

// handleSetupWorkbookPreview reads an uploaded workbook and returns what
// each row would do. Nothing is changed.
func handleSetupWorkbookPreview(w http.ResponseWriter, r *http.Request) {
	file, name, ok := swReadUpload(w, r)
	if !ok {
		return
	}
	c := newSWCaller(r)
	tables, stamp, err := swParse(file, swHeaderWords(c.tenantID))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(tables) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "This workbook has none of the Setup Workbook's sheets - download the template and fill that in.")
		return
	}
	run, err := newSWRun(c, stamp != "" && stamp == c.tenantID, false)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	results := run.process(tables, nil)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"file_name": name, "same_organisation": run.sameOrg, "from_organisation": stamp,
		"sheets": results, "totals": swTotals(results),
	})
}

var swApplying sync.Map // tenant -> *sync.Mutex

// handleSetupWorkbookApply starts applying an uploaded workbook and returns
// the run's id at once; the screen follows it on the run endpoint. A large
// workbook takes longer than one request may last, so the work goes on in
// the background, sheet by sheet, recording its progress.
func handleSetupWorkbookApply(w http.ResponseWriter, r *http.Request) {
	file, name, ok := swReadUpload(w, r)
	if !ok {
		return
	}
	c := newSWCaller(r)
	tables, stamp, err := swParse(file, swHeaderWords(c.tenantID))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(tables) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "This workbook has none of the Setup Workbook's sheets - download the template and fill that in.")
		return
	}
	run, err := newSWRun(c, stamp != "" && stamp == c.tenantID, true)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	lockAny, _ := swApplying.LoadOrStore(c.tenantID, &sync.Mutex{})
	lock := lockAny.(*sync.Mutex)
	if !lock.TryLock() {
		writeAPIErrorGeneric(w, r, http.StatusConflict, "Another workbook is being applied to this organisation - wait for it to finish.")
		return
	}
	runID, err := engines.CreateSetupWorkbookRun(c.tenantID, c.username, name, file)
	if err != nil {
		lock.Unlock()
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	go func() {
		defer lock.Unlock()
		var results []*swSheetResult
		func() {
			defer func() {
				if p := recover(); p != nil {
					_ = engines.UpdateSetupWorkbookRun(c.tenantID, runID, map[string]interface{}{"status": "Failed", "message": fmt.Sprint("internal error: ", p)})
					results = nil
				}
			}()
			results = run.process(tables, func(done int, sheet string) {
				_ = engines.UpdateSetupWorkbookRun(c.tenantID, runID, map[string]interface{}{
					"progress_done": done, "progress_total": len(setupWorkbookSheets), "progress_sheet": sheet,
				})
			})
		}()
		if results == nil {
			return
		}
		totals := swTotals(results)
		var buf bytes.Buffer
		cw := csv.NewWriter(&buf)
		_ = cw.Write([]string{"sheet", "row_number", "key", "message"})
		for _, s := range results {
			for _, row := range s.Rows {
				if row.Action == "error" || row.Result == "failed" {
					_ = cw.Write([]string{s.Sheet, strconv.Itoa(row.Row), row.Key, row.Message})
				}
			}
		}
		cw.Flush()
		status := "Completed"
		if totals.Failed > 0 || totals.Errors > 0 {
			status = "Completed with errors"
		}
		_ = engines.UpdateSetupWorkbookRun(c.tenantID, runID, map[string]interface{}{
			"status": status, "sheets": results, "totals": totals, "same_organisation": run.sameOrg,
			"total_rows":   totals.New + totals.Change + totals.Same + totals.Errors + totals.Notes,
			"success_rows": totals.Done, "failed_rows": totals.Failed + totals.Errors,
			"error_csv": buf.String(), "finished_at": time.Now().UTC().Format(time.RFC3339),
		})
		engines.LogAuditEvent(c.tenantID, c.username, "SETUP_WORKBOOK", "APPLIED",
			fmt.Sprintf("%s: %d new, %d changed, %d failed, %d with errors (run %s)", name, totals.New, totals.Change, totals.Failed, totals.Errors, runID))
	}()
	_ = json.NewEncoder(w).Encode(map[string]string{"run_id": runID, "status": "Running"})
}

func handleSetupWorkbookRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := engines.ListSetupWorkbookRuns(r.Header.Get("Resolved-Tenant-ID"), 50)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	for _, run := range runs {
		delete(run, "sheets")
		delete(run, "error_csv")
	}
	_ = json.NewEncoder(w).Encode(runs)
}

func handleSetupWorkbookRun(w http.ResponseWriter, r *http.Request) {
	run, err := engines.GetSetupWorkbookRun(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(run)
}

func handleSetupWorkbookRunFile(w http.ResponseWriter, r *http.Request) {
	name, file, err := engines.GetSetupWorkbookRunFile(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	swWriteXLSX(w, name, file)
}

func swWriteXLSX(w http.ResponseWriter, name string, file []byte) {
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(name, `"`, "")))
	_, _ = w.Write(file)
}

func handleSetupWorkbookTemplate(w http.ResponseWriter, r *http.Request) {
	c := newSWCaller(r)
	run, err := newSWRun(c, false, false)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	file, err := run.buildWorkbook(false)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	swWriteXLSX(w, "setup-workbook-template.xlsx", file)
}

func handleSetupWorkbookExport(w http.ResponseWriter, r *http.Request) {
	c := newSWCaller(r)
	run, err := newSWRun(c, true, false)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	file, err := run.buildWorkbook(true)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	swWriteXLSX(w, fmt.Sprintf("setup-workbook-%s-%s.xlsx", c.tenantID, time.Now().Format("2006-01-02")), file)
}
