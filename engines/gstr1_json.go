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

// Ask 170.35: the GSTR-1 JSON the GST offline tool / portal accepts, built
// from the month's posted sales invoices and credit notes. Files only: nothing
// here calls the GST portal.
//
// The lines come from BuildCanonicalTaxDocument, the same basis the e-invoice
// is filed on, so the return and the IRN agree. Sections: b2b, b2cl, b2cs,
// cdnr, cdnur, exp, hsn, doc_issue. Not in this build: the nil / exempt table,
// the amendment sections and till (POS) sales, which the ledger holds but no
// invoice document does - the reconciliation block says what is missing.

const (
	gstr1ErrCode = "TAXGST-1700" // GSTIN or month not valid

	// A B2C inter-state invoice above this value is reported invoice by invoice.
	gstr1B2CLLimit = 250000.0
)

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
}

type gstr1Doc struct {
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
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	res := &GSTR1Result{Warnings: []string{}, Counts: map[string]int{}}
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
		ORDER BY doctype, id`, schema), startS, endS)
	if err != nil {
		return nil, err
	}
	for q.Next() {
		var r row
		var raw []byte
		if err := q.Scan(&r.id, &r.doctype, &r.status, &raw); err != nil {
			q.Close()
			return nil, err
		}
		_ = json.Unmarshal(raw, &r.data)
		rows = append(rows, r)
	}
	q.Close()

	var invoices, notes []gstr1Doc
	var invNums, cnNums []string
	cancelled := map[string]int{}
	skipped := 0
	for _, r := range rows {
		if r.status == "Cancelled" {
			// A cancelled number still counts in the document series.
			if strField(r.data, "seller_gstin") == "" || strings.EqualFold(strField(r.data, "seller_gstin"), gstin) {
				n := strings.ToUpper(firstNonEmpty(strField(r.data, "invoice_number"), r.id))
				invNums = append(invNums, n)
				cancelled["inv"]++
			}
			continue
		}
		doc, err := BuildCanonicalTaxDocument(tenantID, r.doctype, r.id)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s %s left out: %v", r.doctype, r.id, err))
			continue
		}
		if doc.Meta.SellerGSTIN != gstin {
			skipped++
			continue
		}
		d := gstr1Doc{num: doc.DocDtls.No, date: gstr1Date(doc.Meta.Date), val: doc.Paise.Invoice}
		src := r.data
		var origInv map[string]interface{}
		if r.doctype == "CreditNote" {
			if inv, _, e := fetchDocData(tenantID, "SalesInvoice", strField(r.data, "sales_invoice_id")); e == nil && inv != nil {
				origInv = inv
				src = inv
			}
		}
		d.ctin = strings.ToUpper(firstNonEmpty(strField(src, "customer_gstin"), doc.Meta.BuyerGSTIN))
		if !gstinRe.MatchString(d.ctin) {
			d.ctin = ""
		}
		d.export = gstr1ExportType(src)
		d.sbNum, d.sbDate, d.sbPort = strField(src, "shipping_bill_no"), strField(src, "shipping_bill_date"), strField(src, "port_code")
		sellerState := gstin[:2]
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
			if d.interstate {
				g.i = tax
			} else {
				g.c = tax / 2
				g.s = tax - g.c
			}
			g.val = g.txval + tax
			d.items = append(d.items, g)
		}
		if r.doctype == "SalesInvoice" {
			invoices = append(invoices, d)
			invNums = append(invNums, d.num)
		} else {
			if origInv != nil {
				d.origB2CL = d.ctin == "" && d.export == "" && d.interstate && gstr1InvoiceValue(origInv) > gstr1B2CLLimit
			}
			notes = append(notes, d)
			cnNums = append(cnNums, d.num)
		}
	}
	if skipped > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d document(s) left out: issued from a store with a different GSTIN", skipped))
	}

	b2b := map[string][]map[string]interface{}{}
	b2cl := map[string][]map[string]interface{}{}
	b2cs := map[gstr1B2CSKey]*gstr1Item{}
	exp := map[string][]map[string]interface{}{}
	cdnr := map[string][]map[string]interface{}{}
	var cdnur []map[string]interface{}

	var fileTaxable, fileC, fileS, fileI int64
	addTotals := func(d gstr1Doc, sign int64) {
		for _, it := range d.items {
			fileTaxable += sign * it.txval
			fileC += sign * it.c
			fileS += sign * it.s
			fileI += sign * it.i
		}
	}
	for _, d := range invoices {
		addTotals(d, 1)
		switch {
		case d.export != "":
			inv := map[string]interface{}{"inum": d.num, "idt": d.date, "val": PaiseToRupees(d.val), "itms": gstr1Itms(d.items, true)}
			if d.sbNum != "" {
				inv["sbnum"] = d.sbNum
			}
			if d.sbDate != "" {
				inv["sbdt"] = gstr1ISO(d.sbDate)
			}
			if d.sbPort != "" {
				inv["sbpcode"] = d.sbPort
			}
			exp[d.export] = append(exp[d.export], inv)
		case d.ctin != "":
			b2b[d.ctin] = append(b2b[d.ctin], map[string]interface{}{"inum": d.num, "idt": d.date, "val": PaiseToRupees(d.val), "pos": d.pos, "rchrg": "N", "inv_typ": "R", "itms": gstr1Itms(d.items, d.interstate)})
		case d.interstate && PaiseToRupees(d.val) > gstr1B2CLLimit:
			b2cl[d.pos] = append(b2cl[d.pos], map[string]interface{}{"inum": d.num, "idt": d.date, "val": PaiseToRupees(d.val), "itms": gstr1Itms(d.items, true)})
		default:
			gstr1AddB2CS(b2cs, d, 1)
		}
	}
	for _, d := range notes {
		addTotals(d, -1)
		nt := map[string]interface{}{"ntty": "C", "nt_num": d.num, "nt_dt": d.date, "val": PaiseToRupees(d.val), "pos": d.pos, "rchrg": "N", "itms": gstr1Itms(d.items, d.interstate)}
		switch {
		case d.ctin != "":
			nt["inv_typ"] = "R"
			cdnr[d.ctin] = append(cdnr[d.ctin], nt)
		case d.export != "" || d.origB2CL:
			typ := "B2CL"
			if d.export != "" {
				typ = "EXP" + d.export
			}
			nt["typ"] = typ
			cdnur = append(cdnur, nt)
		default:
			// A note against a small B2C invoice nets off the B2CS table.
			gstr1AddB2CS(b2cs, d, -1)
		}
	}

	file := map[string]interface{}{"gstin": gstin, "fp": first.Format("012006")}
	if len(b2b) > 0 {
		var out []map[string]interface{}
		for _, ctin := range gstr1SortedKeys(b2b) {
			out = append(out, map[string]interface{}{"ctin": ctin, "inv": b2b[ctin]})
		}
		file["b2b"] = out
	}
	if len(b2cl) > 0 {
		var out []map[string]interface{}
		for _, pos := range gstr1SortedKeys(b2cl) {
			out = append(out, map[string]interface{}{"pos": pos, "inv": b2cl[pos]})
		}
		file["b2cl"] = out
	}
	if len(b2cs) > 0 {
		var keys []gstr1B2CSKey
		for k := range b2cs {
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
			g := b2cs[k]
			row := map[string]interface{}{"sply_ty": k.ty, "rt": k.rate, "typ": "OE", "pos": k.pos, "txval": PaiseToRupees(g.txval), "csamt": 0.0}
			if k.ty == "INTER" {
				row["iamt"] = PaiseToRupees(g.i)
			} else {
				row["camt"], row["samt"] = PaiseToRupees(g.c), PaiseToRupees(g.s)
			}
			out = append(out, row)
		}
		file["b2cs"] = out
	}
	if len(cdnr) > 0 {
		var out []map[string]interface{}
		for _, ctin := range gstr1SortedKeys(cdnr) {
			out = append(out, map[string]interface{}{"ctin": ctin, "nt": cdnr[ctin]})
		}
		file["cdnr"] = out
	}
	if len(cdnur) > 0 {
		file["cdnur"] = cdnur
	}
	if len(exp) > 0 {
		var out []map[string]interface{}
		for _, ty := range gstr1SortedKeys(exp) {
			out = append(out, map[string]interface{}{"exp_typ": ty, "inv": exp[ty]})
		}
		file["exp"] = out
	}
	if hsn := gstr1HSN(invoices, notes); len(hsn) > 0 {
		file["hsn"] = map[string]interface{}{"data": hsn}
	}
	if det := gstr1DocIssue(invNums, cnNums, cancelled["inv"]); len(det) > 0 {
		file["doc_issue"] = map[string]interface{}{"doc_det": det}
	}
	res.File = file
	for _, k := range []string{"b2b", "b2cl", "b2cs", "cdnr", "cdnur", "exp", "hsn", "doc_issue"} {
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
	res.Counts["invoices"], res.Counts["credit_notes"] = len(invoices), len(notes)

	// The same month through the ledger.
	sum, err := GetGSTReturnSummary(tenantID, startS, endS)
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
		res.Warnings = append(res.Warnings, "the file does not tie to the ledger for this month (till sales and unposted documents are not in the file)")
	}
	return res, nil
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
	for _, it := range d.items {
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

func gstr1HSN(invoices, notes []gstr1Doc) []map[string]interface{} {
	type key struct {
		hsn, uqc string
		rate     float64
	}
	agg := map[key]*gstr1Item{}
	add := func(docs []gstr1Doc, sign int64) {
		for _, d := range docs {
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
	}
	add(invoices, 1)
	add(notes, -1)
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

func gstr1DocIssue(invNums, cnNums []string, cancelledInv int) []map[string]interface{} {
	series := func(nums []string, cancel int) map[string]interface{} {
		sort.Strings(nums)
		return map[string]interface{}{"num": 1, "from": nums[0], "to": nums[len(nums)-1], "totnum": len(nums), "cancel": cancel, "net_issue": len(nums) - cancel}
	}
	var out []map[string]interface{}
	if len(invNums) > 0 {
		out = append(out, map[string]interface{}{"doc_num": 1, "doc_typ": "Invoices for outward supply", "docs": []map[string]interface{}{series(invNums, cancelledInv)}})
	}
	if len(cnNums) > 0 {
		out = append(out, map[string]interface{}{"doc_num": 5, "doc_typ": "Credit Note", "docs": []map[string]interface{}{series(cnNums, 0)}})
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
