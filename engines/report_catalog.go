package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

// The user's report catalog as reports (Stage 75.3.5-75.3.7).
//
// Two spreadsheets are the spec - ERP_Operational_Reports_Master_Catalog.xlsx
// and Enterprise_Extended_Modules_Report_Catalog.xlsx, 290 reports, kept as
// docs/specs/reports/catalog_*.tsv and generated into catalogSpecs. Plan D4:
// "the report columns must match the catalog" - so every report here has the
// catalog's columns, in the catalog's order, under the catalog's module, and
// a column the ERP does not hold stays in the report, blank
// (docs/product/report-catalog-coverage.md lists which).
//
// A report is wired as a source - which records make its rows, one per
// document or one per line - plus the few columns only it knows. The common
// columns (Store Code, Legal Entity, Item Name, HSN Code, Vendor Name, ...)
// come from one shared resolver that reads the fields the source names, and
// master names are fetched in one query per master (repeated while one
// lookup leads to another, e.g. store -> legal entity). Summaries group the
// same rows. Every report gets the standard filters, store scope (75.3.3),
// export, profiles and the run log from the report engine like any other.

type catalogSpec struct {
	ID, Module, Group, Name, Grain, Purpose string
	Columns                                 []string
}

// catalogModuleKeys: the catalog's module -> the module a tenant must have on.
var catalogModuleKeys = map[string]string{
	"Sales & POS": "sales", "Inventory": "inventory", "Inventory Loss & Audit": "inventory", "Procurement & GRN": "procurement",
	"Stock Transfer": "inventory", "External Movement": "inventory", "GST & Compliance": "finance",
	"E-commerce & Integration": "oms", "Master Data & Audit": "reports", "MIS & Management": "reports",
	"OMS": "oms", "PIM / PIMS": "pim", "Accounting": "finance", "Expense Management": "expenses",
	"CRM": "crm_loyalty", "HRMS": "hr", "Manufacturing": "manufacturing", "WMS": "wms",
}

// catFn is one column's value for a row.
type catFn func(r *catRow) interface{}

// catSource is where a report's rows come from.
type catSource struct {
	Label   string // the document type a row is ("Sales Invoice"), for Document Type
	Doctype string
	// Lines is the field holding the document's lines ("" = one row per
	// document); stored either as a JSON array or as JSON text.
	Lines string
	// Where is extra SQL on documents, e.g. "status IN ('Paid','Settled')".
	Where    string
	Statuses []string // offered in the status filter
	// DateField is the document's business date ("" = when it was made);
	// StoreField its store ("" = location, "-" = none).
	DateField, StoreField string
	// Document fields the shared resolver reads (blank = not held).
	Number, Customer, CustomerName, CustomerPhone, Vendor, FromStore, ToStore string
	Amount, Taxable, Tax, Reason, Remarks                                     string
	// Line fields (or document fields, for a one-row-per-document source).
	SKU, Qty, Rate, MRP, LineAmount string
	// Cols are this source's own columns, for every report on it.
	Cols map[string]catFn
	// Rows replaces the document query for a source that is not documents
	// (the GL, stock balances, audit tables...). It applies its own filters.
	Rows func(tenantID, schema string, p map[string]string) ([]*catRow, error)
	// StoreOf finds the store through another record (a credit note's bill)
	// when the document holds none; the Store filter then runs in Go.
	StoreOf func(r *catRow) string
}

func dbQuery(q string, args ...interface{}) (*sql.Rows, error) { return db.DB.Query(q, args...) }

// catReport wires one catalog report.
type catReport struct {
	Src   *catSource
	Union []*catSource // rows of several sources (POS bills and invoices)
	Cols  map[string]catFn
	Keep  func(r *catRow) bool
	// Group makes a summary: rows with the same values in these columns
	// become one; Agg says how each other column folds ("sum" - the default
	// for numbers - "count", "countd" (distinct values), "max", "min",
	// "first"); Post derives ratios from the folded row (keyed by header).
	Group []string
	Agg   map[string]string
	Post  func(row map[string]interface{})
	// PostAll works on the finished rows together (a rank across stores).
	PostAll func(rows []map[string]interface{})
	// Hidden columns are worked out and folded like the others (a margin %
	// needs the summed taxable value) but never shown.
	Hidden []string
	// Params are filters beyond From / To / Store / Status; NoDates drops
	// From / To for a point-in-time report.
	Params  []ReportParam
	NoDates bool
}

func (w *catReport) sources() []*catSource {
	if w.Src != nil {
		return append([]*catSource{w.Src}, w.Union...)
	}
	return w.Union
}

var catalogWiring = map[string]*catReport{}

// catWire registers a report's wiring; used in package-level var blocks so
// every wiring exists before init() registers the catalog.
func catWire(id string, w *catReport) bool {
	if _, dup := catalogWiring[id]; dup {
		panic("catalog report wired twice: " + id)
	}
	catalogWiring[id] = w
	return true
}

var catKeyPattern = regexp.MustCompile(`[^a-z0-9]+`)

func catKey(header string) string {
	return strings.Trim(catKeyPattern.ReplaceAllString(strings.ToLower(header), "_"), "_")
}

// catNumericHeader: a column that sums (a total under the table, and the
// default fold in a summary).
func catNumericHeader(h string) bool {
	l := strings.ToLower(h)
	if strings.Contains(l, "%") || strings.Contains(l, " no") || strings.HasSuffix(l, " id") || strings.Contains(l, "date") {
		return false
	}
	// A total named after the price it is taken at - "Stock Value at Purchase Price" - is
	// still a total. It was folded as "first" (the price exclusion below), so a grouped
	// Stock Valuation Summary kept one item's value per store/category and did not tie to
	// the ledger on app (Stage 132.13 o, found by Guide).
	if strings.Contains(l, "value") || strings.Contains(l, "amount") || strings.Contains(l, "qty") {
		return true
	}
	if strings.Contains(l, "rate") || strings.Contains(l, "price") || l == "mrp" {
		return false
	}
	for _, s := range []string{"qty", "amount", "value", "count", "cost", "charges", "tax", "points", "debit", "credit", "balance", "variance"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// catSensitiveHeader: cost and margin figures are masked for roles that may
// not see them, like the existing reports' cost columns.
func catSensitiveHeader(h string) bool {
	l := strings.ToLower(h)
	for _, s := range []string{"cost", "margin", "purchase price", "landed", "making charge", "silver rate"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

func init() {
	for _, spec := range catalogSpecs {
		w, ok := catalogWiring[spec.ID]
		if !ok {
			continue // waits for a module, or not built: see report-catalog-coverage.md
		}
		cols := make([]ReportColumn, len(spec.Columns))
		storeKey := "-"
		for i, h := range spec.Columns {
			cols[i] = ReportColumn{Key: catKey(h), Label: h, Sensitive: catSensitiveHeader(h), Total: len(w.Group) == 0 && catNumericHeader(h)}
			if h == "Image URL" {
				cols[i].Type = "image" // the item's main image, drawn as a thumbnail (75.3.4)
			}
		}
		var params []ReportParam
		if !w.NoDates {
			params = append(params, ReportParam{Key: "from", Label: "From", Type: "date"}, ReportParam{Key: "to", Label: "To", Type: "date"})
		}
		var statuses []string
		for _, s := range w.sources() {
			if s.StoreField != "-" {
				storeKey = "_store"
			}
			statuses = append(statuses, s.Statuses...)
		}
		if storeKey != "-" {
			params = append(params, ReportParam{Key: "location", Label: "Store", Type: "text"})
		}
		if len(statuses) > 0 {
			params = append(params, ReportParam{Key: "status", Label: "Status (optional)", Type: "select", Options: "," + strings.Join(dedupeStrings(statuses), ",")})
		}
		params = append(params, w.Params...)
		spec, w := spec, w
		RegisterReport(ReportDefinition{
			ID: spec.ID, Label: spec.Name, Category: spec.Module, ModuleKey: catalogModuleKeys[spec.Module],
			Description: spec.Purpose + " (" + spec.Grain + ".)", Columns: cols, Params: params, StoreKey: storeKey,
			Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
				return runCatalogReport(tenantID, spec, w, p)
			},
		})
	}
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Rows and lookups
// ---------------------------------------------------------------------------

// catRow is one row before its columns are worked out.
type catRow struct {
	ID, Status, CreatedBy string
	Created               time.Time
	H                     map[string]interface{} // the document (or a query row)
	L                     map[string]interface{} // the line, on a line report
	N                     int                    // line number, from 1
	src                   *catSource
	lk                    *catLookups
	p                     map[string]string
}

// catLookups fetches master records by id, in batches: a miss is remembered
// and the next load() reads every missed id of a doctype in one query.
type catLookups struct {
	schema string
	data   map[string]map[string]map[string]interface{}
	want   map[string]map[string]bool
	extra  map[string]interface{} // per-run caches a source builds (stock maps...)
}

func newCatLookups(schema string) *catLookups {
	return &catLookups{schema: schema, data: map[string]map[string]map[string]interface{}{}, want: map[string]map[string]bool{}, extra: map[string]interface{}{}}
}

func (l *catLookups) get(doctype, id string) map[string]interface{} {
	id = strings.TrimSpace(id)
	if id == "" || id == "<nil>" {
		return nil
	}
	if m, ok := l.data[doctype]; ok {
		if d, ok := m[id]; ok {
			return d
		}
	}
	if l.want[doctype] == nil {
		l.want[doctype] = map[string]bool{}
	}
	l.want[doctype][id] = true
	return nil
}

// load reads the missed ids; false when nothing was missed.
func (l *catLookups) load() bool {
	any := false
	for doctype, ids := range l.want {
		if len(ids) == 0 {
			continue
		}
		any = true
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		if l.data[doctype] == nil {
			l.data[doctype] = map[string]map[string]interface{}{}
		}
		for _, id := range list {
			l.data[doctype][id] = nil // a miss that stays a miss is not asked for again
		}
		// _created_at, _created_by and _status ride along for columns that need the record itself.
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, created_at, COALESCE(created_by, ''), COALESCE(status, '') FROM %s.documents WHERE doctype = $1 AND id = ANY($2)`, l.schema), doctype, pq.Array(list))
		if err == nil {
			for rows.Next() {
				var id, by, status string
				var raw []byte
				var created time.Time
				if rows.Scan(&id, &raw, &created, &by, &status) == nil {
					var m map[string]interface{}
					if json.Unmarshal(raw, &m) == nil && m != nil {
						m["_created_at"], m["_created_by"], m["_status"] = catStamp(created), by, status
						l.data[doctype][id] = m
					}
				}
			}
			rows.Close()
		}
		l.want[doctype] = nil
	}
	return any
}

// catLineList reads a lines field stored as a JSON array or as JSON text.
func catLineList(v interface{}) []map[string]interface{} {
	var arr []interface{}
	switch t := v.(type) {
	case []interface{}:
		arr = t
	case string:
		if strings.HasPrefix(strings.TrimSpace(t), "[") {
			_ = json.Unmarshal([]byte(t), &arr)
		}
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func catSQLField(f string) string { return strings.ReplaceAll(f, "'", "") }

// docRows reads a document source with the standard filters.
func (s *catSource) docRows(schema string, p map[string]string, noDates bool) ([]*catRow, error) {
	where := []string{"doctype = $1", "deleted_at IS NULL"}
	args := []interface{}{s.Doctype}
	add := func(cond string, v interface{}) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if s.Where != "" {
		where = append(where, "("+s.Where+")")
	}
	dateExpr := "to_char(created_at, 'YYYY-MM-DD')"
	if s.DateField != "" {
		dateExpr = fmt.Sprintf("LEFT(COALESCE(NULLIF(data->>'%s', ''), to_char(created_at, 'YYYY-MM-DD')), 10)", catSQLField(s.DateField))
	}
	if !noDates {
		if v := strings.TrimSpace(p["from"]); v != "" {
			add(dateExpr+" >= $%d", v)
		}
		if v := strings.TrimSpace(p["to"]); v != "" {
			add(dateExpr+" <= $%d", v)
		}
	}
	if s.StoreField != "-" && s.StoreOf == nil {
		if v := strings.TrimSpace(p["location"]); v != "" {
			add(fmt.Sprintf("data->>'%s' = ANY($%%d)", catSQLField(s.storeField())), pq.Array(strings.Split(v, ",")))
		}
	}
	if v := strings.TrimSpace(p["status"]); v != "" && len(s.Statuses) > 0 {
		add("status = $%d", v)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(status, ''), created_at, COALESCE(created_by, ''), data FROM %s.documents
		WHERE %s ORDER BY created_at DESC, id LIMIT %d`, schema, strings.Join(where, " AND "), registerRowCap), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var id, status, by string
		var created time.Time
		var raw []byte
		if rows.Scan(&id, &status, &created, &by, &raw) != nil {
			continue
		}
		var h map[string]interface{}
		_ = json.Unmarshal(raw, &h)
		if h == nil {
			h = map[string]interface{}{}
		}
		if s.Lines == "" {
			out = append(out, &catRow{ID: id, Status: status, Created: created, CreatedBy: by, H: h})
			continue
		}
		for i, l := range catLineList(h[s.Lines]) {
			out = append(out, &catRow{ID: id, Status: status, Created: created, CreatedBy: by, H: h, L: l, N: i + 1})
		}
	}
	return out, rows.Err()
}

func (s *catSource) storeField() string {
	if s.StoreField == "" {
		return "location"
	}
	return s.StoreField
}

// ---------------------------------------------------------------------------
// Running a report
// ---------------------------------------------------------------------------

func (w *catReport) colFn(h string, r *catRow) catFn {
	if f := w.Cols[h]; f != nil {
		return f
	}
	if r.src != nil {
		if f := r.src.Cols[h]; f != nil {
			return f
		}
	}
	return catCommon[h]
}

// catResolved reports whether a column has a value source on some row source
// of the report (for the coverage document).
func (w *catReport) catResolved(h string) bool {
	if w.Cols[h] != nil || catCommon[h] != nil {
		return true
	}
	for _, s := range w.sources() {
		if s.Cols[h] != nil {
			return true
		}
	}
	return false
}

func runCatalogReport(tenantID string, spec catalogSpec, w *catReport, p map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	lk := newCatLookups(schema)
	var rows []*catRow
	for _, src := range w.sources() {
		var rs []*catRow
		if src.Rows != nil {
			rs, err = src.Rows(tenantID, schema, p)
		} else {
			rs, err = src.docRows(schema, p, w.NoDates)
		}
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			r.src, r.lk, r.p = src, lk, p
		}
		rows = append(rows, rs...)
	}
	// The catalog columns plus the hidden ones a summary folds for its ratios.
	cols := append(append([]string{}, spec.Columns...), w.Hidden...)
	// Work the columns out until no lookup is missing (a store's legal
	// entity needs the store first), then once more for the values.
	for pass := 0; pass < 4; pass++ {
		for _, r := range rows {
			for _, h := range cols {
				if f := w.colFn(h, r); f != nil {
					f(r)
				}
			}
			if w.Keep != nil {
				w.Keep(r)
			}
			r.storeID()
		}
		if !lk.load() {
			break
		}
	}
	stores := map[string]bool{}
	for _, s := range strings.Split(p["location"], ",") {
		if s = strings.TrimSpace(s); s != "" {
			stores[s] = true
		}
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		if w.Keep != nil && !w.Keep(r) {
			continue
		}
		if len(stores) > 0 && r.src.StoreOf != nil && !stores[r.storeID()] {
			continue
		}
		row := make(map[string]interface{}, len(spec.Columns)+1)
		for _, h := range cols {
			var v interface{}
			if f := w.colFn(h, r); f != nil {
				v = catClean(f(r))
			}
			row[h] = v
		}
		row["_store"] = r.storeID()
		out = append(out, row)
	}
	if len(w.Group) > 0 {
		out = catFold(out, cols, w)
	}
	if w.PostAll != nil {
		w.PostAll(out)
	}
	// Keyed by column key for the engine; "_store" stays for store scope.
	final := make([]map[string]interface{}, len(out))
	for i, row := range out {
		m := make(map[string]interface{}, len(row))
		for _, h := range spec.Columns {
			m[catKey(h)] = row[h]
		}
		m["_store"] = row["_store"]
		final[i] = m
	}
	return final, nil
}

// catClean rounds money to paise and turns empty values into nil.
func catClean(v interface{}) interface{} {
	switch t := v.(type) {
	case nil:
		return nil
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return nil
		}
		if v := math.Round(t*100) / 100; v != 0 {
			return v
		}
		return 0.0 // never "-0"
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		if t == "" || t == "<nil>" {
			return nil
		}
		return t
	case bool:
		if t {
			return "Yes"
		}
		return "No"
	case map[string]interface{}, []interface{}:
		b, _ := json.Marshal(t)
		return string(b)
	}
	return v
}

func catFold(rows []map[string]interface{}, cols []string, w *catReport) []map[string]interface{} {
	groupSet := map[string]bool{}
	for _, g := range w.Group {
		groupSet[g] = true
	}
	type acc struct {
		row      map[string]interface{}
		distinct map[string]map[string]bool
	}
	byKey := map[string]*acc{}
	var order []string
	for _, r := range rows {
		parts := make([]string, len(w.Group))
		for i, g := range w.Group {
			parts[i] = fmt.Sprint(r[g])
		}
		key := strings.Join(parts, "\x1f")
		a := byKey[key]
		if a == nil {
			a = &acc{row: map[string]interface{}{"_store": r["_store"]}, distinct: map[string]map[string]bool{}}
			for _, g := range w.Group {
				a.row[g] = r[g]
			}
			byKey[key] = a
			order = append(order, key)
		}
		for _, h := range cols {
			if groupSet[h] {
				continue
			}
			mode := w.Agg[h]
			if mode == "" {
				if catNumericHeader(h) {
					mode = "sum"
				} else {
					mode = "first"
				}
			}
			v := r[h]
			switch mode {
			case "sum":
				a.row[h] = catNum(a.row[h]) + catNum(v)
			case "count":
				if v != nil {
					a.row[h] = catNum(a.row[h]) + 1
				}
			case "countd":
				if v != nil {
					if a.distinct[h] == nil {
						a.distinct[h] = map[string]bool{}
					}
					a.distinct[h][fmt.Sprint(v)] = true
					a.row[h] = float64(len(a.distinct[h]))
				}
			case "max", "min":
				if v == nil {
					continue
				}
				cur, has := a.row[h]
				if !has || cur == nil || (mode == "max" && catLess(cur, v)) || (mode == "min" && catLess(v, cur)) {
					a.row[h] = v
				}
			default: // first
				if a.row[h] == nil {
					a.row[h] = v
				}
			}
		}
		if a.row["_store"] != r["_store"] {
			a.row["_store"] = r["_store"] // rows of one group share a store when the group has one
		}
	}
	out := make([]map[string]interface{}, 0, len(order))
	for _, k := range order {
		row := byKey[k].row
		for h, v := range row {
			if f, ok := v.(float64); ok {
				row[h] = math.Round(f*100) / 100
			}
		}
		if w.Post != nil {
			w.Post(row)
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		for _, g := range w.Group {
			a, b := fmt.Sprint(out[i][g]), fmt.Sprint(out[j][g])
			if a != b {
				return a < b
			}
		}
		return false
	})
	return out
}

func catLess(a, b interface{}) bool {
	fa, oka := a.(float64)
	fb, okb := b.(float64)
	if oka && okb {
		return fa < fb
	}
	return fmt.Sprint(a) < fmt.Sprint(b)
}

// catNum reads a number from a value of any stored shape.
func catNum(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(t), ",", ""), 64)
		return f
	}
	return 0
}

// catPct is a / b as a percentage, or nil when b is zero.
func catPct(a, b float64) interface{} {
	if b == 0 {
		return nil
	}
	return math.Round(a/b*10000) / 100
}

func catDiv(a, b float64) interface{} {
	if b == 0 {
		return nil
	}
	return math.Round(a/b*100) / 100
}
