package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"custom_erp/engines"
)

// Ask 237.1: the bulk PO templates are built from this organisation's own
// records each time they are downloaded - its stores, vendors, product types,
// categories, HSN codes and attributes with their values - so every column
// that takes one of them has a drop-down. Four templates, by ?kind= and
// ?grn=1:
//
//   - new: the Vendors and Purchase sheets; a row's attributes make (or
//     reuse, 237.3) its design and combination.
//   - new + grn: the same, with Vendor Invoice No, Invoice Date and Received
//     Qty required on every row; the rows of one vendor + invoice become one
//     goods receipt (237.2).
//   - reorder (+ grn): only Combination ID or Design No of what exists
//     (237.5).
//
// Ask 237.6: handleBulkPurchaseRunErrors gives a finished run's rows that did
// not load as the same sheets, the file's own cells plus a Reason column and
// the same drop-downs, ready to correct and upload again.

// bpTplLastRow is the last sheet row (0-based) a column's drop-down covers.
const bpTplLastRow = 5001

// bpList is one drop-down's values, kept in a column of the Lists sheet.
type bpList struct {
	title  string
	values []string
	strict bool // a value not in the list is refused, not warned about
	col    int
}

func (l *bpList) rng() string {
	c := bpColLetter(l.col)
	return fmt.Sprintf("'Lists'!$%s$2:$%s$%d", c, c, len(l.values)+1)
}

func bpColLetter(i int) string {
	name := ""
	for i++; i > 0; i = (i - 1) / 26 {
		name = string(rune('A'+(i-1)%26)) + name
	}
	return name
}

// bpLists is every drop-down of one organisation, by what a column names.
type bpLists struct {
	all    []*bpList
	byKey  map[string]*bpList // "store", "vendor", ... and "attr:" + bpAttrKey
	attrs  []engines.DesignAttribute
	states bool // the home country has a state list
}

func (ls *bpLists) add(key, title string, values []string, strict bool) {
	if len(values) == 0 {
		return
	}
	l := &bpList{title: title, values: values, strict: strict, col: len(ls.all)}
	ls.all = append(ls.all, l)
	ls.byKey[key] = l
}

// bpActive: a record that may still be chosen.
func bpActive(d map[string]interface{}) bool {
	switch strings.ToLower(strings.TrimSpace(swText(d["status"]))) {
	case "inactive", "disabled", "blocked", "archived", "cancelled":
		return false
	}
	return true
}

// bpNames lists a master's records by name. A name two records share is
// given as each one's code instead, since the upload refuses a name that
// names two records.
func bpNames(run *swRun, doctype string, name func(map[string]interface{}) string) []string {
	type rec struct{ name, code string }
	var recs []rec
	count := map[string]int{}
	for _, r := range run.index(doctype).recs {
		if !bpActive(r.Data) {
			continue
		}
		n := strings.TrimSpace(name(r.Data))
		if n == "" {
			continue
		}
		recs = append(recs, rec{n, strings.TrimSpace(swText(r.Data["code"]))})
		count[swNorm(n)]++
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range recs {
		v := r.name
		if count[swNorm(v)] > 1 && r.code != "" {
			v = r.code
		}
		if seen[swNorm(v)] {
			continue
		}
		seen[swNorm(v)] = true
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func bpField(fields ...string) func(map[string]interface{}) string {
	return func(d map[string]interface{}) string {
		for _, f := range fields {
			if s := strings.TrimSpace(swText(d[f])); s != "" {
				return s
			}
		}
		return ""
	}
}

func bpLoadLists(run *swRun) (*bpLists, error) {
	tenantID := run.c.tenantID
	ls := &bpLists{byKey: map[string]*bpList{}}
	ls.add("store", "Stores", bpNames(run, "Location", bpField("name", "label", "code")), true)
	ls.add("vendor", "Vendors", bpNames(run, "Vendor", bpField("name", "code")), false)
	ls.add("product type", "Product Types", bpNames(run, "ProductType", bpField("name", "code")), false)
	ls.add("category", "Categories", bpNames(run, "Category", bpField("name", "code")), false)
	ls.add("hsn code", "HSN Codes", bpNames(run, "HSNCode", func(d map[string]interface{}) string {
		h := bpHSNDigits(swText(d["hsn"]))
		if h == "" {
			return ""
		}
		if desc := strings.TrimSpace(swText(d["description"])); desc != "" {
			return h + " - " + desc
		}
		return h
	}), false)
	var states []string
	for _, r := range engines.CountryRegions(engines.TenantPhoneCountry(tenantID)) {
		states = append(states, r.Name)
	}
	ls.states = len(states) > 0
	ls.add("state", "States", states, true)
	m, err := engines.GetDesignMatrix(tenantID)
	if err != nil {
		return nil, err
	}
	ls.attrs = m.Attributes
	for _, a := range m.Attributes {
		if a.Label == "" || bpNotAttributes[swHeaderNorm(a.Label)] {
			continue
		}
		ls.add("attr:"+bpAttrKey(a.Label), a.Label, a.Options, false)
		if l := ls.byKey["attr:"+bpAttrKey(a.Label)]; l != nil && a.Code != "" {
			ls.byKey["attr:"+bpAttrKey(a.Code)] = l
		}
	}
	return ls, nil
}

// listsSheet is the Lists sheet: one column per drop-down.
func (ls *bpLists) listsSheet() engines.XLSXSheet {
	s := engines.XLSXSheet{Name: "Lists"}
	rows := 0
	for _, l := range ls.all {
		if len(l.values) > rows {
			rows = len(l.values)
		}
		s.ColWidths = append(s.ColWidths, 24)
	}
	head := make([]engines.XLSXCell, len(ls.all))
	for i, l := range ls.all {
		head[i] = swHeadC(l.title)
	}
	s.Rows = append(s.Rows, head)
	for r := 0; r < rows; r++ {
		row := make([]engines.XLSXCell, len(ls.all))
		for i, l := range ls.all {
			if r < len(l.values) {
				row[i] = swT(l.values[r])
			}
		}
		s.Rows = append(s.Rows, row)
	}
	s.FreezeRows = 1
	return s
}

// validations gives each column of a sheet whose header names a list its
// drop-down, from row from (0-based). reorder: the vendor must exist.
func (ls *bpLists) validations(sheet string, heads []string, from int, reorder bool) []engines.XLSXListValidation {
	var out []engines.XLSXListValidation
	for c, h := range heads {
		k := swHeaderNorm(h)
		var key string
		switch {
		case sheet == "vendors":
			if k == "state" {
				key = "state"
			}
		case sheet == "bills":
			if k == "vendor" {
				key = "vendor"
			}
		case k == bpColStore || k == bpColVendor || k == bpColType || k == bpColCategory || k == bpColHSN:
			key = k
		case !bpNotAttributes[k] && !reorder:
			key = "attr:" + bpAttrKey(k)
		}
		l := ls.byKey[key]
		if l == nil {
			continue
		}
		strict := l.strict || (reorder && key == "vendor")
		out = append(out, engines.XLSXListValidation{Col: c, FromRow: from, ToRow: bpTplLastRow, Range: l.rng(), AllowOther: !strict})
	}
	return out
}

type bpTplKind struct{ reorder, grn bool }

func (k bpTplKind) fileName() string {
	n := "bulk-po"
	if k.reorder {
		n = "reorder-po"
	}
	if k.grn {
		n += "-grn"
	}
	return n + "-template.xlsx"
}

func bpDateExample(order string) string {
	switch order {
	case engines.DateOrderMDY:
		return "10-20-2026"
	case engines.DateOrderYMD:
		return "2026-10-20"
	}
	return "20-10-2026"
}

// purchaseCols are the Purchase sheet's columns for a template kind.
func (ls *bpLists) purchaseCols(k bpTplKind, dateOrder string) []swCol {
	var cols []swCol
	if k.reorder {
		cols = []swCol{
			{Head: "PO Ref*", Hint: "Same ref + vendor = one PO"},
			{Head: "Vendor", Hint: "Blank = the design's vendor", Width: 24},
			{Head: "Store*", Hint: "Pick from the list", Width: 20},
			{Head: "Design No", Hint: "Or a one-combination design's no.", Width: 18},
			{Head: "Combination ID", Hint: "The SKU to reorder", Width: 18},
			{Head: "Qty*", Hint: "Whole number"},
			{Head: "Purchase Rate (₹)", Hint: "Blank = last rate", Width: 18},
			{Head: "MRP (₹)", Hint: "Blank = current MRP"},
		}
	} else {
		cols = []swCol{
			{Head: "PO Ref*", Hint: "Same ref + vendor = one PO"},
			{Head: "Vendor*", Hint: "Pick, or add on Vendors", Width: 24},
			{Head: "Store*", Hint: "Pick from the list", Width: 20},
			{Head: "Design No", Hint: "Optional; same no. = one design", Width: 18},
			{Head: "Product Type*", Hint: "Pick or type new", Width: 18},
			{Head: "Category*", Hint: "Pick or type new", Width: 18},
			{Head: "HSN Code*", Hint: "Pick or type new", Width: 26},
			{Head: "GST %", Hint: "Only for a new HSN"},
		}
		for _, a := range ls.attrs {
			if a.Label == "" || bpNotAttributes[swHeaderNorm(a.Label)] {
				continue
			}
			hint := "Type a value"
			if len(a.Options) > 0 {
				hint = "Pick or type new"
			}
			cols = append(cols, swCol{Head: a.Label, Hint: hint, Width: 16})
		}
		cols = append(cols,
			swCol{Head: "Qty*", Hint: "Whole number"},
			swCol{Head: "Purchase Rate (₹)*", Hint: "Per piece", Width: 18},
			swCol{Head: "MRP (₹)*", Hint: "Per piece"})
	}
	if k.grn {
		cols = append(cols,
			swCol{Head: "Vendor Invoice No*", Hint: "One goods receipt per invoice", Width: 20},
			swCol{Head: "Invoice Date*", Hint: "Like " + bpDateExample(dateOrder), Width: 16},
			swCol{Head: "Received Qty", Hint: "Blank = Qty", Width: 14})
	}
	if !k.reorder {
		cols = append(cols, swCol{Head: "Photo", Hint: "File names, comma between", Width: 22})
	}
	return cols
}

func (ls *bpLists) startSheet(k bpTplKind, dateOrder string) engines.XLSXSheet {
	title := map[bpTplKind]string{
		{false, false}: "Bulk PO upload", {false, true}: "Bulk PO + GRN upload",
		{true, false}: "Reorder PO upload", {true, true}: "Reorder PO + GRN upload",
	}[k]
	lines := []string{"Fill the Purchase sheet: one row per combination. Rows with the same Vendor and PO Ref make one PO."}
	if k.reorder {
		lines = append(lines,
			"Give the Combination ID, or the Design No of a design with one combination. Nothing new is made.",
			"Vendor, Purchase Rate and MRP left blank come from the design.")
	} else {
		lines = append(lines,
			"A new vendor goes on the Vendors sheet first.",
			"Pick from the drop-downs. A new product type, category, HSN code or attribute value is made for you.",
			"A design with the same item name, vendor and attributes is used again; otherwise one is made.",
			"A new attribute: add a column with its name as the header.")
	}
	if k.grn {
		lines = append(lines,
			"Every row needs the Vendor Invoice No and Invoice Date. Rows with the same vendor and invoice make one goods receipt at the PO's store.")
	}
	lines = append(lines,
		"Columns marked * are required. Dates like "+bpDateExample(dateOrder)+".",
		"Upload it on Purchase Orders › Upload from Excel: Preview, then Apply.",
		"Rows with a problem do not load; the rest do. Download Rows to fix, correct them and upload that file again.")
	s := engines.XLSXSheet{Name: "Start Here", ColWidths: []float64{4, 110}}
	s.Rows = append(s.Rows, []engines.XLSXCell{swBold(title)}, nil)
	for i, l := range lines {
		s.Rows = append(s.Rows, []engines.XLSXCell{swT(strconv.Itoa(i + 1)), swT(l)})
	}
	s.Rows = append(s.Rows, nil, []engines.XLSXCell{{}, swHint("Drop-downs list this organisation's records on " + engines.BusinessToday() + ".")})
	return s
}

// bpTemplateBook builds one template.
func bpTemplateBook(ls *bpLists, k bpTplKind, dateOrder string) ([]byte, error) {
	sheets := []engines.XLSXSheet{ls.startSheet(k, dateOrder)}
	if !k.reorder {
		vcols := []swCol{
			{Head: "Vendor Name*", Hint: "New vendors only", Width: 28},
			{Head: "State*", Hint: "Pick from the list", Width: 20},
			{Head: "City", Hint: ""},
			{Head: "GSTIN", Hint: "15 characters", Width: 18},
			{Head: "Phone", Hint: ""},
			{Head: "Email", Hint: "", Width: 24},
		}
		if !ls.states {
			vcols[1].Hint = "Type the state"
		}
		vs := swTableSheet(vcols, nil)
		vs.Name = "Vendors"
		vs.Validations = ls.validations("vendors", bpHeads(vcols), 2, false)
		sheets = append(sheets, vs)
	}
	pcols := ls.purchaseCols(k, dateOrder)
	ps := swTableSheet(pcols, nil)
	ps.Name = "Purchase"
	ps.Validations = ls.validations("purchase", bpHeads(pcols), 2, k.reorder)
	sheets = append(sheets, ps)
	if len(ls.all) > 0 {
		sheets = append(sheets, ls.listsSheet())
	}
	return engines.WriteXLSXBook(sheets)
}

func bpHeads(cols []swCol) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Head
	}
	return out
}

// handleBulkPurchaseTemplate serves this organisation's template:
// ?kind=new|reorder (default new) and ?grn=1 for the PO + GRN one.
func handleBulkPurchaseTemplate(w http.ResponseWriter, r *http.Request) {
	if !bpAllowed(w, r, "read") {
		return
	}
	q := r.URL.Query()
	k := bpTplKind{reorder: q.Get("kind") == "reorder", grn: q.Get("grn") == "1"}
	if kind := q.Get("kind"); kind != "" && kind != "new" && kind != "reorder" {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "kind is new or reorder")
		return
	}
	run, err := newSWRun(newSWCaller(r), false, false)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	ls, err := bpLoadLists(run)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	book, err := bpTemplateBook(ls, k, engines.ClientDateOrder(run.c.tenantID))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	swWriteXLSX(w, k.fileName(), book)
}

// bpNumCols are the columns a rows-to-fix file writes as numbers.
var bpNumCols = map[string]bool{bpColGST: true, bpColQty: true, bpColRate: true, bpColMRP: true, bpColRecv: true,
	"invoice total": true, "amount": true, "received amount": true}

// bpFixBook is the rows-to-fix workbook of a run: for each sheet with a row
// that did not load, the file's header plus Reason, and those rows as typed
// (PO Ref changed where the row must start a new PO). Dates are written as
// text in the client's date order, so they read back the same.
func bpFixBook(file []byte, fix []bpFixRow, ls *bpLists, dateOrder string) ([]byte, error) {
	sheets, err := engines.ReadXLSX(file)
	if err != nil {
		return nil, err
	}
	bySheet := map[string]map[int]bpFixRow{}
	for _, f := range fix {
		if bySheet[f.Sheet] == nil {
			bySheet[f.Sheet] = map[int]bpFixRow{}
		}
		bySheet[f.Sheet][f.Row] = f
	}
	words := bpHeaderWords()
	var out []engines.XLSXSheet
	for _, name := range []string{"vendors", "purchase", "bills"} {
		rows := bySheet[name]
		if len(rows) == 0 {
			continue
		}
		var src *engines.XLSXReadSheet
		for i := range sheets {
			if swNorm(sheets[i].Name) == name {
				src = &sheets[i]
			}
		}
		if src == nil {
			continue
		}
		header := -1
		for i := 0; i < len(src.Rows) && i < 6 && header < 0; i++ {
			for _, cell := range src.Rows[i] {
				for _, wd := range words[name] {
					if swHeaderNorm(cell) == swHeaderNorm(wd) {
						header = i
					}
				}
			}
		}
		if header < 0 {
			continue
		}
		heads := append([]string{}, src.Rows[header]...)
		for len(heads) > 0 && strings.TrimSpace(heads[len(heads)-1]) == "" {
			heads = heads[:len(heads)-1]
		}
		reasonAt, refAt := -1, -1
		norm := make([]string, len(heads))
		for c, h := range heads {
			norm[c] = swHeaderNorm(h)
			switch norm[c] {
			case "reason":
				reasonAt = c
			case bpColRef:
				refAt = c
			}
		}
		if reasonAt < 0 {
			reasonAt = len(heads)
			heads = append(heads, "Reason")
			norm = append(norm, "reason")
		}
		reorder := false
		for _, h := range norm {
			reorder = reorder || h == bpColCombo
		}
		for _, h := range norm {
			if h == bpColType {
				reorder = false
			}
		}
		sh := engines.XLSXSheet{Name: map[string]string{"vendors": "Vendors", "purchase": "Purchase", "bills": "Bills"}[name], FreezeRows: 1}
		head := make([]engines.XLSXCell, len(heads))
		for c, h := range heads {
			head[c] = swHeadC(h)
			w := float64(len(h) + 6)
			if w < 14 {
				w = 14
			}
			if c == reasonAt {
				w = 60
			}
			sh.ColWidths = append(sh.ColWidths, w)
		}
		sh.Rows = append(sh.Rows, head)
		for i := header + 1; i < len(src.Rows); i++ {
			f, ok := rows[src.RowNumbers[i]]
			if !ok {
				continue
			}
			cells := src.Rows[i]
			row := make([]engines.XLSXCell, len(heads))
			for c := range heads {
				v := ""
				if c < len(cells) {
					v = strings.TrimSpace(cells[c])
				}
				switch {
				case c == reasonAt:
					v = f.Reason
				case c == refAt && f.NewRef != "":
					v = f.NewRef
				case norm[c] == bpColInvDate && v != "":
					if iso, err := bpDate(v, dateOrder); err == nil {
						v = bpISOToOrder(iso, dateOrder)
					}
				}
				if bpNumCols[norm[c]] && v != "" {
					if n, err := swNumber(v); err == nil {
						row[c] = swNum(n)
						continue
					}
				}
				row[c] = swT(v)
			}
			sh.Rows = append(sh.Rows, row)
		}
		if ls != nil {
			sh.Validations = ls.validations(name, heads, 1, reorder)
		}
		out = append(out, sh)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the rows to fix are not in the uploaded file")
	}
	if ls != nil && len(ls.all) > 0 {
		out = append(out, ls.listsSheet())
	}
	return engines.WriteXLSXBook(out)
}

// bpISOToOrder writes a yyyy-mm-dd date in the client's date order.
func bpISOToOrder(iso, order string) string {
	if len(iso) < 10 {
		return iso
	}
	y, m, d := iso[0:4], iso[5:7], iso[8:10]
	switch order {
	case engines.DateOrderMDY:
		return m + "-" + d + "-" + y
	case engines.DateOrderYMD:
		return iso[:10]
	}
	return d + "-" + m + "-" + y
}

// handleBulkPurchaseRunErrors serves a finished run's rows that did not
// load, with why, as a workbook to correct and upload again (237.6).
func handleBulkPurchaseRunErrors(w http.ResponseWriter, r *http.Request) {
	if !bpAllowed(w, r, "read") {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	id := r.PathValue("id")
	run, err := engines.GetWorkbookRun(tenantID, engines.BulkPurchaseDoctype, id)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	var res bpResult
	if raw, err := json.Marshal(run["result"]); err == nil {
		_ = json.Unmarshal(raw, &res)
	}
	if res.Fix = bpRunFix(&res); len(res.Fix) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Every row of this upload loaded - there are no rows to fix.")
		return
	}
	name, file, err := engines.GetWorkbookRunFile(tenantID, engines.BulkPurchaseDoctype, id)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	sw, err := newSWRun(newSWCaller(r), false, false)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	ls, err := bpLoadLists(sw)
	if err != nil {
		ls = nil // the rows still come back, without drop-downs
	}
	book, err := bpFixBook(file, res.Fix, ls, engines.ClientDateOrder(tenantID))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".xlsx"), ".XLSX")
	if base == "" {
		base = "bulk-po"
	}
	swWriteXLSX(w, base+"-rows-to-fix.xlsx", book)
}
