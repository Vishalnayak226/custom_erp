package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Ask 170.35: the GSTR-1 JSON the GST offline tool / portal accepts. Files
// only: nothing here calls the GST portal.
//
// Invoices and credit notes come from the month's posted documents through
// BuildCanonicalTaxDocument, the same basis the e-invoice is filed on, so the
// return and the IRN agree. Till bills and till returns come from the carts and
// return requests the ledger took that month (gstr1_till_sales.go). Exempt and
// nil-rated lines go to the nil table, not the rate sections. A document
// changed after its month was marked filed is reported again in the amendment
// sections (gstr1_amendments.go). Sections: b2b, b2cl, b2cs, cdnr, cdnur, exp,
// nil, hsn, doc_issue, b2ba, b2cla, b2csa, cdnra, cdnura, expa.

const (
	gstr1ErrCode = "TAXGST-1700" // GSTIN or month not valid
)

// gstr1B2CLFrom is when the B2CL limit fell from Rs 2.5 lakh to Rs 1 lakh
// (Notification 12/2024-Central Tax, invoices from 1 August 2024). Ask 215.18.
var gstr1B2CLFrom = time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC)

// gstr1B2CLLimit is the value above which a B2C inter-state invoice dated day
// is reported invoice by invoice (table 5, B2CL). day is dd-mm-yyyy or
// yyyy-mm-dd; a day that does not parse takes the current limit.
func gstr1B2CLLimit(day string) float64 {
	day = strings.TrimSpace(day)
	if len(day) > 10 {
		day = day[:10]
	}
	for _, layout := range []string{"02-01-2006", "2006-01-02"} {
		if t, err := time.Parse(layout, day); err == nil && t.Before(gstr1B2CLFrom) {
			return 250000
		}
	}
	return 100000
}

var (
	gstinRe        = regexp.MustCompile(`^[0-9]{2}[A-Z0-9]{13}$`)
	gstr1MonthOnly = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
)

// GSTR1Result is what the screen and the download use: the file itself, what
// was left out and how it ties to the ledger.
type GSTR1Result struct {
	File           map[string]interface{} `json:"file"`
	Warnings       []string               `json:"warnings"`
	Reconciliation GSTR1Reconciliation    `json:"reconciliation"`
	Counts         map[string]int         `json:"counts"`
}

// GSTR1Reconciliation compares the file's totals with GetGSTReturnSummary for
// the same month. Values are rupees; Matches allows a rupee of rounding.
// Amendments change earlier months, so they are not in these totals.
type GSTR1Reconciliation struct {
	FileTaxable   float64 `json:"file_taxable"`
	FileCGST      float64 `json:"file_cgst"`
	FileSGST      float64 `json:"file_sgst"`
	FileIGST      float64 `json:"file_igst"`
	LedgerTaxable float64 `json:"ledger_taxable"`
	LedgerCGST    float64 `json:"ledger_cgst"`
	LedgerSGST    float64 `json:"ledger_sgst"`
	LedgerIGST    float64 `json:"ledger_igst"`
	Matches       bool    `json:"matches"`
}

type gstr1Item struct {
	rate                float64
	txval, i, c, s, val int64 // paise
	qty                 float64
	hsn, desc, uqc      string
	nilKind             string // "expt" or "nil": reported in the nil table
}

type gstr1Doc struct {
	key        string // doctype:id, what a filed row is kept under
	gstin      string // the seller's GSTIN
	party      string // the customer document
	note       bool   // a credit note
	till       bool   // a till bill or till return
	cancelled  bool
	num, date  string
	val        int64
	pos        string
	items      []gstr1Item
	ctin       string
	export     string // "", "WPAY", "WOPAY"
	interstate bool
	sbNum      string
	sbDate     string
	sbPort     string
	origB2CL   bool // a note against a B2CL invoice
}

// section is the table the document is listed in.
func (d gstr1Doc) section() string {
	switch {
	case !d.note && d.export != "":
		return "exp"
	case !d.note && d.ctin != "":
		return "b2b"
	case !d.note && d.interstate && PaiseToRupees(d.val) > gstr1B2CLLimit(d.date):
		return "b2cl"
	case !d.note:
		return "b2cs"
	case d.ctin != "":
		return "cdnr"
	case d.export != "" || d.origB2CL:
		return "cdnur"
	}
	// A note against a small B2C invoice nets off the B2CS table.
	return "b2cs"
}

// taxed is the lines reported in the rate sections; the rest are nil / exempt.
func (d gstr1Doc) taxed() []gstr1Item {
	var out []gstr1Item
	for _, it := range d.items {
		if it.nilKind == "" {
			out = append(out, it)
		}
	}
	return out
}

// entry is the document as one invoice / note row of its section.
func (d gstr1Doc) entry() map[string]interface{} {
	itms := gstr1Itms(d.taxed(), d.interstate)
	if d.note {
		nt := map[string]interface{}{"ntty": "C", "nt_num": d.num, "nt_dt": d.date, "val": PaiseToRupees(d.val), "pos": d.pos, "rchrg": "N", "itms": itms}
		if d.ctin != "" {
			nt["inv_typ"] = "R"
		} else {
			typ := "B2CL"
			if d.export != "" {
				typ = "EXP" + d.export
			}
			nt["typ"] = typ
		}
		return nt
	}
	inv := map[string]interface{}{"inum": d.num, "idt": d.date, "val": PaiseToRupees(d.val), "itms": itms}
	switch d.section() {
	case "exp":
		if d.sbNum != "" {
			inv["sbnum"] = d.sbNum
		}
		if d.sbDate != "" {
			inv["sbdt"] = gstr1ISO(d.sbDate)
		}
		if d.sbPort != "" {
			inv["sbpcode"] = d.sbPort
		}
	case "b2b":
		inv["pos"], inv["rchrg"], inv["inv_typ"] = d.pos, "N", "R"
	}
	return inv
}

func gstr1Date(t time.Time) string { return t.Format("02-01-2006") }

func gstr1UQC(unit string) string {
	u := strings.ToUpper(strings.TrimSpace(unit))
	switch u {
	case "PCS", "NOS", "KGS", "MTR", "LTR", "BOX", "PAC", "SET", "PRS", "DOZ", "BAG", "BTL", "CTN", "GMS", "SQM", "TON", "UNT":
		return u
	case "":
		return "OTH"
	}
	return "OTH"
}

// gstr1ExportType maps the invoice's export_type onto the portal's code.
func gstr1ExportType(data map[string]interface{}) string {
	switch strings.ToLower(strings.TrimSpace(strField(data, "export_type"))) {
	case "export under lut":
		return "WOPAY"
	case "export with payment of tax", "export with payment of igst":
		return "WPAY"
	}
	return ""
}

func gstr1Month(month string) (first, last time.Time, err error) {
	if !gstr1MonthOnly.MatchString(strings.TrimSpace(month)) {
		return first, last, &ValidationError{Code: gstr1ErrCode, Message: "give the month as YYYY-MM"}
	}
	first, _ = time.Parse("2006-01", month)
	return first, first.AddDate(0, 1, -1), nil
}

// gstr1Ctx caches what every document of a build looks up again.
type gstr1Ctx struct {
	tenantID, schema string
	infos            map[string]gstr1ItemInfo
	stores           map[string]PrintStore
	buyers           map[string]PrintStore
}

type gstr1ItemInfo struct {
	tax  ItemTaxInfo
	err  error
	meta map[string]interface{}
}

func newGSTR1Ctx(tenantID string) (*gstr1Ctx, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return &gstr1Ctx{tenantID: tenantID, schema: schema, infos: map[string]gstr1ItemInfo{},
		stores: map[string]PrintStore{}, buyers: map[string]PrintStore{}}, nil
}

func (c *gstr1Ctx) item(sku string) gstr1ItemInfo {
	if v, ok := c.infos[sku]; ok {
		return v
	}
	var v gstr1ItemInfo
	v.tax, v.err = GetItemTaxInfo(c.tenantID, sku)
	v.meta = printItemFields(c.tenantID, sku, map[string]map[string]interface{}{})
	c.infos[sku] = v
	return v
}

// kind is "expt" or "nil" for an exempt or nil-rated item, "" otherwise.
func (c *gstr1Ctx) kind(sku string) string {
	if strings.TrimSpace(sku) == "" {
		return ""
	}
	if info := c.item(sku); info.err == nil {
		switch info.tax.Treatment {
		case TaxTreatmentExempt:
			return "expt"
		case TaxTreatmentNilRated:
			return "nil"
		}
	}
	return ""
}

func (c *gstr1Ctx) store(code string) PrintStore {
	if s, ok := c.stores[code]; ok {
		return s
	}
	s := LoadPrintStore(c.tenantID, code)
	c.stores[code] = s
	return s
}

func (c *gstr1Ctx) buyer(id string) PrintStore {
	if b, ok := c.buyers[id]; ok {
		return b
	}
	b := loadPrintCustomer(c.tenantID, id)
	c.buyers[id] = b
	return b
}

// gstr1MonthDocs is every GSTIN's invoices, credit notes, till bills and till
// returns for the month. A document that could not be read is a warning.
func gstr1MonthDocs(c *gstr1Ctx, first, last time.Time) ([]gstr1Doc, []string, error) {
	startS, endS := first.Format("2006-01-02"), last.Format("2006-01-02")
	type row struct {
		id, doctype, status string
		data                map[string]interface{}
	}
	var rows []row
	q, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, doctype, status, data FROM %s.documents
		WHERE deleted_at IS NULL AND (
		   (doctype = 'SalesInvoice' AND status IN ('Approved', 'Paid', 'Cancelled')
		    AND COALESCE(NULLIF(data->>'invoice_date', ''), to_char(created_at, 'YYYY-MM-DD')) BETWEEN $1 AND $2)
		OR (doctype = 'CreditNote' AND status = 'Posted'
		    AND COALESCE(NULLIF(data->>'note_date', ''), to_char(created_at, 'YYYY-MM-DD')) BETWEEN $1 AND $2))
		ORDER BY doctype, id`, c.schema), startS, endS)
	if err != nil {
		return nil, nil, err
	}
	for q.Next() {
		var r row
		var raw []byte
		if err := q.Scan(&r.id, &r.doctype, &r.status, &raw); err != nil {
			q.Close()
			return nil, nil, err
		}
		_ = json.Unmarshal(raw, &r.data)
		rows = append(rows, r)
	}
	q.Close()

	var docs []gstr1Doc
	warnings := []string{}
	for _, r := range rows {
		if r.status == "Cancelled" {
			// A cancelled number still counts in the document series.
			docs = append(docs, gstr1Doc{key: r.doctype + ":" + r.id, cancelled: true, gstin: strings.ToUpper(strField(r.data, "seller_gstin")),
				num: strings.ToUpper(firstNonEmpty(strField(r.data, "invoice_number"), r.id))})
			continue
		}
		d, err := gstr1CanonicalDoc(c, r.doctype, r.id, r.data)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s %s left out: %v", r.doctype, r.id, err))
			continue
		}
		docs = append(docs, d)
	}
	till, tw, err := gstr1TillDocs(c, first, last)
	if err != nil {
		return nil, nil, err
	}
	return append(docs, till...), append(warnings, tw...), nil
}

// gstr1CanonicalDoc is one posted invoice or credit note, from its canonical
// tax document.
func gstr1CanonicalDoc(c *gstr1Ctx, doctype, id string, data map[string]interface{}) (gstr1Doc, error) {
	doc, err := BuildCanonicalTaxDocument(c.tenantID, doctype, id)
	if err != nil {
		return gstr1Doc{}, err
	}
	d := gstr1Doc{key: doctype + ":" + id, gstin: strings.ToUpper(doc.Meta.SellerGSTIN), note: doctype == "CreditNote",
		num: doc.DocDtls.No, date: gstr1Date(doc.Meta.Date), val: doc.Paise.Invoice}
	src := data
	var origInv map[string]interface{}
	if d.note {
		if inv, _, e := fetchDocData(c.tenantID, "SalesInvoice", strField(data, "sales_invoice_id")); e == nil && inv != nil {
			origInv = inv
			src = inv
		}
	}
	d.party = firstNonEmpty(strField(data, "customer_id"), strField(src, "customer"))
	if len(d.gstin) < 2 {
		return d, nil // no GSTIN: in no file
	}
	d.ctin = strings.ToUpper(firstNonEmpty(strField(src, "customer_gstin"), doc.Meta.BuyerGSTIN))
	if !gstinRe.MatchString(d.ctin) {
		d.ctin = ""
	}
	d.export = gstr1ExportType(src)
	d.sbNum, d.sbDate, d.sbPort = strField(src, "shipping_bill_no"), strField(src, "shipping_bill_date"), strField(src, "port_code")
	sellerState := d.gstin[:2]
	d.pos = firstNonEmpty(strField(src, "place_of_supply"), doc.BuyerDtls.Pos, sellerState)
	if len(d.pos) > 2 {
		d.pos = d.pos[:2]
	}
	if d.export != "" {
		d.pos = "96" // other countries
		d.interstate = true
	} else {
		d.interstate = d.pos != sellerState
	}
	for i, it := range doc.ItemList {
		lp := doc.Lines[i]
		g := gstr1Item{rate: it.GstRt, qty: it.Qty, hsn: it.HsnCd, desc: it.PrdDesc, uqc: gstr1UQC(it.Unit)}
		tax := lp.CGST + lp.SGST + lp.IGST
		g.txval = lp.Assessable
		if d.export == "WOPAY" {
			// Nothing was charged: the billed value is the taxable value.
			g.txval, tax = lp.Total, 0
		}
		if d.export == "" {
			if k := c.kind(lp.SKU); k != "" {
				// Exempt / nil-rated: no tax in the price, the whole value is reported.
				g.nilKind, g.rate, g.txval, tax = k, 0, lp.Total, 0
			}
		}
		gstr1SplitTax(&g, tax, d.interstate)
		d.items = append(d.items, g)
	}
	if d.note && origInv != nil {
		d.origB2CL = d.ctin == "" && d.export == "" && d.interstate && gstr1InvoiceValue(origInv) > gstr1B2CLLimit(firstNonEmpty(strField(origInv, "invoice_date"), d.date))
	}
	return d, nil
}

// gstr1SplitTax puts a line's tax on IGST or halves it into CGST / SGST.
func gstr1SplitTax(g *gstr1Item, tax int64, interstate bool) {
	g.i, g.c, g.s = 0, 0, 0
	if interstate {
		g.i = tax
	} else {
		g.c = tax / 2
		g.s = tax - g.c
	}
	g.val = g.txval + tax
}

// BuildGSTR1 builds the month's GSTR-1 for one GSTIN.
func BuildGSTR1(tenantID, gstin, month string) (*GSTR1Result, error) {
	gstin = strings.ToUpper(strings.TrimSpace(gstin))
	month = strings.TrimSpace(month)
	if !gstinRe.MatchString(gstin) {
		return nil, &ValidationError{Code: gstr1ErrCode, Message: "the GSTIN must be 15 characters"}
	}
	first, last, err := gstr1Month(month)
	if err != nil {
		return nil, err
	}
	c, err := newGSTR1Ctx(tenantID)
	if err != nil {
		return nil, err
	}
	all, warnings, err := gstr1MonthDocs(c, first, last)
	if err != nil {
		return nil, err
	}
	res := &GSTR1Result{Warnings: warnings, Counts: map[string]int{}}
	var docs []gstr1Doc
	skipped := 0
	for _, d := range all {
		if d.gstin == gstin || (d.cancelled && d.gstin == "") {
			docs = append(docs, d)
		} else {
			skipped++
		}
	}
	if skipped > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d document(s) left out: issued from a store with a different GSTIN", skipped))
	}

	secs := newGSTR1Sections()
	var fileTaxable, fileC, fileS, fileI int64
	var live []gstr1Doc
	for _, d := range docs {
		if d.cancelled {
			continue
		}
		live = append(live, d)
		sign := int64(1)
		if d.note {
			sign = -1
		}
		for _, it := range d.items {
			fileTaxable += sign * it.txval
			fileC += sign * it.c
			fileS += sign * it.s
			fileI += sign * it.i
		}
		secs.add(d)
	}

	file := map[string]interface{}{"gstin": gstin, "fp": first.Format("012006")}
	secs.write(file)
	if hsn := gstr1HSN(live); len(hsn) > 0 {
		file["hsn"] = map[string]interface{}{"data": hsn}
	}
	if det := gstr1DocIssue(docs); len(det) > 0 {
		file["doc_issue"] = map[string]interface{}{"doc_det": det}
	}
	amend, aw, err := gstr1Amendments(c, gstin, first)
	if err != nil {
		return nil, err
	}
	res.Warnings = append(res.Warnings, aw...)
	if len(amend.fresh) > 0 {
		// A filed B2C sale that now has a buyer GSTIN: reported in this
		// month's B2B (b2csa takes it out of B2C). Not in the ledger check.
		for _, d := range amend.fresh {
			var taxed []gstr1Item // its nil part stays in the month it was filed
			for _, it := range d.items {
				if it.nilKind == "" {
					taxed = append(taxed, it)
				}
			}
			d.items = taxed
			secs.add(d)
		}
		secs.write(file)
	}
	amend.write(file)
	res.File = file
	for _, k := range []string{"b2b", "b2cl", "b2cs", "cdnr", "cdnur", "exp", "nil", "hsn", "doc_issue", "b2ba", "b2cla", "b2csa", "cdnra", "cdnura", "expa"} {
		if v, ok := file[k]; ok {
			b, _ := json.Marshal(v)
			var arr []interface{}
			if json.Unmarshal(b, &arr) == nil {
				res.Counts[k] = len(arr)
			} else {
				res.Counts[k] = 1
			}
		}
	}
	for _, d := range live {
		switch {
		case d.till && d.note:
			res.Counts["till_returns"]++
		case d.till:
			res.Counts["till_bills"]++
		case d.note:
			res.Counts["credit_notes"]++
		default:
			res.Counts["invoices"]++
		}
	}
	res.Counts["amendments"] = len(amend.changed)

	// The same month through the ledger.
	sum, err := GetGSTReturnSummary(tenantID, first.Format("2006-01-02"), last.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	rec := GSTR1Reconciliation{
		FileTaxable: PaiseToRupees(fileTaxable), FileCGST: PaiseToRupees(fileC), FileSGST: PaiseToRupees(fileS), FileIGST: PaiseToRupees(fileI),
		LedgerTaxable: round2(sum.TaxableValue + sum.NonTaxableValue), LedgerCGST: sum.OutputCGST, LedgerSGST: sum.OutputSGST, LedgerIGST: sum.OutputIGST,
	}
	rec.Matches = math.Abs(rec.FileTaxable-rec.LedgerTaxable) <= 1 && math.Abs(rec.FileCGST-rec.LedgerCGST) <= 1 &&
		math.Abs(rec.FileSGST-rec.LedgerSGST) <= 1 && math.Abs(rec.FileIGST-rec.LedgerIGST) <= 1
	res.Reconciliation = rec
	if !rec.Matches {
		res.Warnings = append(res.Warnings, "the file does not tie to the ledger for this month (unposted documents, returns of online orders and postings by hand are not in the file)")
	}
	return res, nil
}

// gstr1Sections is the month's own documents, table by table.
type gstr1Sections struct {
	b2b, b2cl, exp, cdnr map[string][]map[string]interface{}
	cdnur                []map[string]interface{}
	b2cs                 map[gstr1B2CSKey]*gstr1Item
	nilT                 map[string]*[2]int64 // supply type -> exempt, nil-rated (paise)
}

func newGSTR1Sections() *gstr1Sections {
	return &gstr1Sections{b2b: map[string][]map[string]interface{}{}, b2cl: map[string][]map[string]interface{}{},
		exp: map[string][]map[string]interface{}{}, cdnr: map[string][]map[string]interface{}{},
		b2cs: map[gstr1B2CSKey]*gstr1Item{}, nilT: map[string]*[2]int64{}}
}

func (s *gstr1Sections) add(d gstr1Doc) {
	sign := int64(1)
	if d.note {
		sign = -1
	}
	for _, it := range d.items {
		if it.nilKind == "" {
			continue
		}
		ty := "INTRA"
		if d.interstate {
			ty = "INTR"
		}
		if d.ctin != "" {
			ty += "B2B"
		} else {
			ty += "B2C"
		}
		n := s.nilT[ty]
		if n == nil {
			n = &[2]int64{}
			s.nilT[ty] = n
		}
		if it.nilKind == "expt" {
			n[0] += sign * it.txval
		} else {
			n[1] += sign * it.txval
		}
	}
	if len(d.taxed()) == 0 {
		return // wholly nil / exempt: only in the nil table
	}
	switch d.section() {
	case "exp":
		s.exp[d.export] = append(s.exp[d.export], d.entry())
	case "b2b":
		s.b2b[d.ctin] = append(s.b2b[d.ctin], d.entry())
	case "b2cl":
		s.b2cl[d.pos] = append(s.b2cl[d.pos], d.entry())
	case "cdnr":
		s.cdnr[d.ctin] = append(s.cdnr[d.ctin], d.entry())
	case "cdnur":
		s.cdnur = append(s.cdnur, d.entry())
	default:
		gstr1AddB2CS(s.b2cs, d, sign)
	}
}

// gstr1Grouped writes a section whose entries are grouped under a key
// (b2b by buyer GSTIN, b2cl by state, exp by export type).
func gstr1Grouped(file map[string]interface{}, sec, key, list string, m map[string][]map[string]interface{}) {
	if len(m) == 0 {
		return
	}
	var out []map[string]interface{}
	for _, k := range gstr1SortedKeys(m) {
		out = append(out, map[string]interface{}{key: k, list: m[k]})
	}
	file[sec] = out
}

func (s *gstr1Sections) write(file map[string]interface{}) {
	gstr1Grouped(file, "b2b", "ctin", "inv", s.b2b)
	gstr1Grouped(file, "b2cl", "pos", "inv", s.b2cl)
	if rows := gstr1B2CSRows(s.b2cs); len(rows) > 0 {
		file["b2cs"] = rows
	}
	gstr1Grouped(file, "cdnr", "ctin", "nt", s.cdnr)
	if len(s.cdnur) > 0 {
		file["cdnur"] = s.cdnur
	}
	gstr1Grouped(file, "exp", "exp_typ", "inv", s.exp)
	if len(s.nilT) > 0 {
		var inv []map[string]interface{}
		for _, ty := range []string{"INTRB2B", "INTRAB2B", "INTRB2C", "INTRAB2C"} {
			if n := s.nilT[ty]; n != nil {
				inv = append(inv, map[string]interface{}{"sply_ty": ty, "expt_amt": PaiseToRupees(n[0]), "nil_amt": PaiseToRupees(n[1]), "ngsup_amt": 0.0})
			}
		}
		file["nil"] = map[string]interface{}{"inv": inv}
	}
}

// gstr1B2CSRows is the B2CS table, one row per supply type, state and rate.
func gstr1B2CSRows(m map[gstr1B2CSKey]*gstr1Item) []map[string]interface{} {
	var keys []gstr1B2CSKey
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].pos != keys[b].pos {
			return keys[a].pos < keys[b].pos
		}
		if keys[a].ty != keys[b].ty {
			return keys[a].ty < keys[b].ty
		}
		return keys[a].rate < keys[b].rate
	})
	var out []map[string]interface{}
	for _, k := range keys {
		g := m[k]
		row := map[string]interface{}{"sply_ty": k.ty, "rt": k.rate, "typ": "OE", "pos": k.pos, "txval": PaiseToRupees(g.txval), "csamt": 0.0}
		if k.ty == "INTER" {
			row["iamt"] = PaiseToRupees(g.i)
		} else {
			row["camt"], row["samt"] = PaiseToRupees(g.c), PaiseToRupees(g.s)
		}
		out = append(out, row)
	}
	return out
}

func gstr1InvoiceValue(inv map[string]interface{}) float64 {
	return numFromInterface(inv["total_amount"])
}

// gstr1ISO turns a stored yyyy-mm-dd date into the portal's dd-mm-yyyy.
func gstr1ISO(s string) string {
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(s)); err == nil {
		return gstr1Date(t)
	}
	return s
}

type gstr1B2CSKey struct {
	ty, pos string
	rate    float64
}

func gstr1AddB2CS(m map[gstr1B2CSKey]*gstr1Item, d gstr1Doc, sign int64) {
	ty := "INTRA"
	if d.interstate {
		ty = "INTER"
	}
	for _, it := range d.taxed() {
		k := gstr1B2CSKey{ty, d.pos, it.rate}
		g := m[k]
		if g == nil {
			g = &gstr1Item{}
			m[k] = g
		}
		g.txval += sign * it.txval
		g.i += sign * it.i
		g.c += sign * it.c
		g.s += sign * it.s
	}
}

// gstr1Itms groups an invoice's lines by rate: the portal's itms / itm_det.
func gstr1Itms(items []gstr1Item, inter bool) []map[string]interface{} {
	by := map[float64]*gstr1Item{}
	var rates []float64
	for _, it := range items {
		g := by[it.rate]
		if g == nil {
			g = &gstr1Item{rate: it.rate}
			by[it.rate] = g
			rates = append(rates, it.rate)
		}
		g.txval += it.txval
		g.i += it.i
		g.c += it.c
		g.s += it.s
	}
	sort.Float64s(rates)
	out := []map[string]interface{}{}
	for n, r := range rates {
		g := by[r]
		det := map[string]interface{}{"rt": r, "txval": PaiseToRupees(g.txval), "csamt": 0.0}
		if inter {
			det["iamt"] = PaiseToRupees(g.i)
		} else {
			det["camt"], det["samt"] = PaiseToRupees(g.c), PaiseToRupees(g.s)
		}
		out = append(out, map[string]interface{}{"num": n + 1, "itm_det": det})
	}
	return out
}

// gstr1HSN is the HSN summary: every line, nil / exempt ones too, notes netted off.
func gstr1HSN(docs []gstr1Doc) []map[string]interface{} {
	type key struct {
		hsn, uqc string
		rate     float64
	}
	agg := map[key]*gstr1Item{}
	for _, d := range docs {
		sign := int64(1)
		if d.note {
			sign = -1
		}
		for _, it := range d.items {
			k := key{it.hsn, it.uqc, it.rate}
			g := agg[k]
			if g == nil {
				g = &gstr1Item{desc: it.desc}
				agg[k] = g
			}
			g.qty += float64(sign) * it.qty
			g.txval += sign * it.txval
			g.i += sign * it.i
			g.c += sign * it.c
			g.s += sign * it.s
			g.val += sign * it.val
		}
	}
	var keys []key
	for k := range agg {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].hsn != keys[b].hsn {
			return keys[a].hsn < keys[b].hsn
		}
		return keys[a].rate < keys[b].rate
	})
	out := []map[string]interface{}{}
	for i, k := range keys {
		g := agg[k]
		out = append(out, map[string]interface{}{"num": i + 1, "hsn_sc": k.hsn, "desc": g.desc, "uqc": k.uqc, "qty": g.qty, "rt": k.rate,
			"val": PaiseToRupees(g.val), "txval": PaiseToRupees(g.txval), "iamt": PaiseToRupees(g.i), "camt": PaiseToRupees(g.c), "samt": PaiseToRupees(g.s), "csamt": 0.0})
	}
	return out
}

// gstr1DocIssue is the documents-issued table: invoices and till bills, then
// credit notes, one row per number series (the number without its running
// digits), cancelled numbers counted in.
func gstr1DocIssue(docs []gstr1Doc) []map[string]interface{} {
	type series struct {
		nums   []string
		cancel int
	}
	rows := func(note bool) []map[string]interface{} {
		by := map[string]*series{}
		for _, d := range docs {
			if d.note != note || d.num == "" {
				continue
			}
			k := strings.TrimRight(d.num, "0123456789")
			s := by[k]
			if s == nil {
				s = &series{}
				by[k] = s
			}
			s.nums = append(s.nums, d.num)
			if d.cancelled {
				s.cancel++
			}
		}
		var out []map[string]interface{}
		for n, k := range gstr1SortedKeys(by) {
			s := by[k]
			sort.Strings(s.nums)
			out = append(out, map[string]interface{}{"num": n + 1, "from": s.nums[0], "to": s.nums[len(s.nums)-1], "totnum": len(s.nums), "cancel": s.cancel, "net_issue": len(s.nums) - s.cancel})
		}
		return out
	}
	var out []map[string]interface{}
	if inv := rows(false); len(inv) > 0 {
		out = append(out, map[string]interface{}{"doc_num": 1, "doc_typ": "Invoices for outward supply", "docs": inv})
	}
	if cn := rows(true); len(cn) > 0 {
		out = append(out, map[string]interface{}{"doc_num": 5, "doc_typ": "Credit Note", "docs": cn})
	}
	return out
}

func gstr1SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
