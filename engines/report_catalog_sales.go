package engines

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Catalog reports - Sales & POS (Stage 75.3.5, Wave A).
//
// A till bill is a POSCart once paid: bill_number is its GST invoice number,
// amount_paid what the customer paid, gst_breakdown its tax, and each line
// keeps the till's GST-inclusive price, the master (reference) price, its
// share of the bill's offers and its net. The allocation stored at payment
// splits every line by tender. An order-to-cash sale is a SalesInvoice;
// returns are ReturnRequests.

// posBillGST reads the bill's stored GST split.
func posBillGST(r *catRow, key string) float64 {
	if gb, ok := r.H["gst_breakdown"].(map[string]interface{}); ok {
		return catNum(gb[key])
	}
	return 0
}

// posAllocation is the bill's payment split for line n (1-based), matched by
// position and SKU.
func posAllocation(r *catRow) map[string]interface{} {
	lines := catLineList(r.H["allocation"])
	if r.N >= 1 && r.N <= len(lines) && catStr(lines[r.N-1]["sku"]) == r.sku() {
		return lines[r.N-1]
	}
	for _, l := range lines {
		if catStr(l["sku"]) == r.sku() {
			return l
		}
	}
	return nil
}

// posLineMoney is one till line in money: everything GST-inclusive on the
// bill is split back to taxable value at the item's rate, as the GST invoice
// prints it.
type posLineMoney struct {
	qty, mrp, gross, itemDisc, billDisc, points, taxable, gst, net, rate float64
}

func posLine(r *catRow) posLineMoney {
	m := posLineMoney{qty: r.ln("qty")}
	unit := r.ln("sale_price")
	m.rate = catNum(r.item()["gst_rate"])
	if v := r.ln("gst_rate"); v != 0 {
		m.rate = v
	}
	m.mrp = firstPositive(r.ln("reference_price"), catNum(r.item()["mrp"]), unit)
	m.net = r.ln("net_total")
	if m.net == 0 {
		m.net = unit*m.qty - r.ln("offer_share")
	}
	div := 1 + m.rate/100
	m.gross = m.mrp * m.qty / div
	if m.mrp > unit {
		m.itemDisc = (m.mrp - unit) * m.qty / div
	}
	m.billDisc = r.ln("offer_share") / div
	if a := posAllocation(r); a != nil {
		if t, ok := a["tenders"].(map[string]interface{}); ok {
			m.points = PaiseToRupees(int64(catNum(t["Loyalty Points"])))
		}
	}
	m.taxable = m.net / div
	m.gst = m.net - m.taxable
	return m
}

func posBillNo(r *catRow) interface{} { return firstNonEmpty(r.hs("bill_number"), r.ID) }

// posBillGross is the bill at master prices, GST included - so on the bill
// register Gross - Discount = Net, all as the customer saw them.
func posBillGross(r *catRow) float64 {
	total := 0.0
	for i, l := range catLineList(r.H["items"]) {
		lr := &catRow{H: r.H, L: l, N: i + 1, src: posLineShape, lk: r.lk}
		m := posLine(lr)
		total += m.mrp * m.qty
	}
	return total
}

func posBillDiscount(r *catRow) float64 {
	if d := posBillGross(r) - r.hn("amount_paid"); d > 0.004 {
		return d
	}
	return 0
}

func posSalesman(r *catRow) interface{} {
	return r.nameOf("Salesman", firstNonEmpty(r.ls("salesman_id"), r.hs("salesman_id")))
}

var posSaleCols = map[string]catFn{
	"Sales Invoice Date": func(r *catRow) interface{} { return r.date() },
	"Sales Invoice No":   posBillNo,
	"GST Bill No":        posBillNo,
	"POS Invoice No":     posBillNo,
	"Sales Channel":      catConst("Retail"),
	"PO Number":          catH("customer_reference"),
	"Tender Type": func(r *catRow) interface{} {
		kinds := make([]string, 0, 2)
		for k := range cartTenderTotals(r.H) {
			kinds = append(kinds, k)
		}
		if len(kinds) == 0 {
			return firstNonEmpty(r.hs("payment_tender"), r.hs("payment_mode"))
		}
		return strings.Join(catSorted(kinds), " + ")
	},
	"Salesperson Name": posSalesman,
	"Salesperson ID":   func(r *catRow) interface{} { return firstNonEmpty(r.ls("salesman_id"), r.hs("salesman_id")) },
	"Counter": func(r *catRow) interface{} {
		s := r.master("POSSession", r.hs("pos_session"))
		if s == nil {
			return nil
		}
		return r.nameOf("POSProfile", catStr(s["pos_profile"]))
	},
	"Place of Supply": func(r *catRow) interface{} { return r.store()["state"] },
	"Bill Net Amount": catHN("amount_paid"),
	"Net Amount":      catHN("amount_paid"),
	"Taxable Amount":  func(r *catRow) interface{} { return posBillGST(r, "taxable_amount") },
	"GST Amount":      func(r *catRow) interface{} { return posBillGST(r, "total_tax") },
	"CGST Amount":     func(r *catRow) interface{} { return posBillGST(r, "cgst") },
	"SGST Amount":     func(r *catRow) interface{} { return posBillGST(r, "sgst") },
	"IGST Amount":     func(r *catRow) interface{} { return posBillGST(r, "igst") },
	"Gross Amount":    func(r *catRow) interface{} { return firstPositive(posBillGross(r), r.hn("amount_paid")) },
	"Discount Amount": func(r *catRow) interface{} { return posBillDiscount(r) },
	"Bill Count":      catConst(1.0),
	"Sold Qty":        catSumLines("items", "qty"),
}

// Line columns (on the item lines of the same bills).
var posLineCols = map[string]catFn{
	"Sold Qty":               catLN("qty"),
	"MRP":                    func(r *catRow) interface{} { return posLine(r).mrp },
	"Gross Value":            func(r *catRow) interface{} { return posLine(r).gross },
	"Customer Redeem Points": func(r *catRow) interface{} { return posLine(r).points },
	"Item Discount":          func(r *catRow) interface{} { return posLine(r).itemDisc },
	"Bill Discount":          func(r *catRow) interface{} { return posLine(r).billDisc },
	"Discount Amount":        func(r *catRow) interface{} { m := posLine(r); return m.itemDisc + m.billDisc },
	"Item Taxable Amount":    func(r *catRow) interface{} { return posLine(r).taxable },
	"GST %":                  func(r *catRow) interface{} { return posLine(r).rate },
	"GST Amount":             func(r *catRow) interface{} { return posLine(r).gst },
	"Item Net Amount":        func(r *catRow) interface{} { return posLine(r).net },
	"Sales Amount":           func(r *catRow) interface{} { return posLine(r).net },
	"Net Sales Amount":       func(r *catRow) interface{} { return posLine(r).net },
	"Net Sales Qty":          catLN("qty"),
	"Bill Count":             func(r *catRow) interface{} { return posBillNo(r) },
	"Tender Amount":          func(r *catRow) interface{} { return posLine(r).net },
	"Purchase Price":         func(r *catRow) interface{} { return catItemCost(r) },
	"Landed Cost": func(r *catRow) interface{} {
		return catItemCost(r) * r.ln("qty")
	},
	"Margin Amount": func(r *catRow) interface{} {
		return posLine(r).taxable - catItemCost(r)*r.ln("qty")
	},
	"Margin %": func(r *catRow) interface{} {
		m := posLine(r)
		return catPct(m.taxable-catItemCost(r)*m.qty, m.taxable)
	},
	"Image URL": func(r *catRow) interface{} { return catItemImage(r) },
}

func catSorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func mergeCols(maps ...map[string]catFn) map[string]catFn {
	out := map[string]catFn{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

var catPOSBills = &catSource{
	Label: "POS Bill", Doctype: "POSCart", Where: "status = 'Paid'",
	Number: "bill_number", Customer: "customer_id", CustomerName: "customer_name", CustomerPhone: "customer_phone", Amount: "amount_paid",
	Cols: posSaleCols,
}

var catPOSLines = &catSource{
	Label: "POS Bill", Doctype: "POSCart", Where: "status = 'Paid'", Lines: "items",
	Number: "bill_number", Customer: "customer_id", CustomerName: "customer_name", CustomerPhone: "customer_phone",
	SKU: "sku", Qty: "qty", MRP: "reference_price",
	Cols: mergeCols(posSaleCols, posLineCols),
}

// An order-to-cash sale: the invoice carries its own GST split.
var catSalesInvoices = &catSource{
	Label: "Sales Invoice", Doctype: "SalesInvoice", Where: "status NOT IN ('Draft', 'Cancelled')",
	Statuses: []string{"Approved", "Posted", "Paid", "Overdue"},
	Number:   "invoice_number", CustomerName: "customer", Amount: "total_amount", Taxable: "taxable_amount", Tax: "total_tax",
	Cols: map[string]catFn{
		"Sales Invoice Date": func(r *catRow) interface{} { return r.date() },
		"Sales Invoice No":   func(r *catRow) interface{} { return r.number() },
		"GST Bill No":        func(r *catRow) interface{} { return r.number() },
		"Customer ID":        catH("customer_id"),
		"Sales Channel": func(r *catRow) interface{} {
			if o := r.master("SalesOrder", r.hs("sales_order_id")); o != nil {
				return firstNonEmpty(catStr(o["channel"]), "B2B")
			}
			return "B2B"
		},
		"PO Number":       catH("customer_reference"),
		"Bill Net Amount": catHN("total_amount"),
		"CGST Amount":     catHN("cgst"),
		"SGST Amount":     catHN("sgst"),
		"IGST Amount":     catHN("igst"),
		"Gross Amount":    catHN("total_amount"),
		"Discount Amount": catConst(0.0),
		"Bill Count":      catConst(1.0),
		"Place of Supply": func(r *catRow) interface{} {
			if o := r.master("SalesOrder", r.hs("sales_order_id")); o != nil {
				if s := catStr(o["shipping_state"]); s != "" {
					return s
				}
			}
			return r.store()["state"]
		},
		"Tax Type": func(r *catRow) interface{} {
			if r.hv("interstate") == true || r.hn("igst") > 0 {
				return "IGST"
			}
			return "CGST + SGST"
		},
	},
}

// catItemCost is the item's moving-average cost (item_cost), else its
// standard cost - loaded once per run.
func catItemCost(r *catRow) float64 {
	costs, ok := r.lk.extra["item_cost"].(map[string]float64)
	if !ok {
		costs = map[string]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT item_code, avg_unit_cost_paise FROM %s.item_cost`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var code string
				var paise int64
				if rows.Scan(&code, &paise) == nil {
					costs[code] = PaiseToRupees(paise)
				}
			}
			rows.Close()
		}
		r.lk.extra["item_cost"] = costs
	}
	if c := costs[r.sku()]; c > 0 {
		return c
	}
	return catNum(r.item()["standard_cost"])
}

// catItemImage is the item's main image (a media id the report table shows
// as a thumbnail), loaded once per run for the items on the report.
func catItemImage(r *catRow) interface{} {
	images, ok := r.lk.extra["images"].(map[string]string)
	if !ok {
		images = map[string]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT DISTINCT ON (data->>'item') data->>'item', id FROM %s.documents
			WHERE doctype = 'ProductMedia' AND status = 'Active' AND COALESCE(data->>'file_type', '') LIKE 'image/%%'
			ORDER BY data->>'item', (data->>'media_role' = 'Main Image') DESC, created_at`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var item, id string
				if rows.Scan(&item, &id) == nil {
					images[item] = id
				}
			}
			rows.Close()
		}
		r.lk.extra["images"] = images
	}
	if id := images[r.sku()]; id != "" {
		return id
	}
	return images[catStr(r.item()["family"])]
}

// --- returns ---------------------------------------------------------------

// returnLineValue is a returned line's value: the share the customer paid
// (from the sale's allocation) when known, else the original price.
func returnLineValue(r *catRow) float64 {
	if p := r.ln("net_paise"); p != 0 {
		return PaiseToRupees(int64(p))
	}
	return r.ln("original_unit_price") * r.ln("qty")
}

func returnLineMoney(r *catRow) (net, taxable, gst, rate float64) {
	net = returnLineValue(r)
	rate = catNum(r.item()["gst_rate"])
	taxable = net / (1 + rate/100)
	return net, taxable, net - taxable, rate
}

// returnOriginal is the bill (or order) the return is against.
func returnOriginal(r *catRow) map[string]interface{} {
	return r.master("POSCart", r.hs("original_order_id"))
}

func returnBillNo(r *catRow) interface{} {
	if c := returnOriginal(r); c != nil {
		return firstNonEmpty(catStr(c["bill_number"]), r.hs("original_order_id"))
	}
	return r.hs("original_order_id")
}

func returnCustomerID(r *catRow) string {
	if c := returnOriginal(r); c != nil {
		return catStr(c["customer_id"])
	}
	if o := r.master("SalesOrder", r.hs("original_order_id")); o != nil {
		return catStr(o["customer_id"])
	}
	return ""
}

var returnCols = map[string]catFn{
	"Sales Return Date": func(r *catRow) interface{} { return r.date() },
	"Sales Return No":   func(r *catRow) interface{} { return r.number() },
	"GST Bill No":       func(r *catRow) interface{} { return r.number() },
	"Original Bill No":  returnBillNo,
	"Customer ID":       func(r *catRow) interface{} { return returnCustomerID(r) },
	"Customer Name": func(r *catRow) interface{} {
		if id := returnCustomerID(r); id != "" {
			return r.nameOf("Customer", id)
		}
		if o := r.master("SalesOrder", r.hs("original_order_id")); o != nil {
			return o["customer_name"]
		}
		return nil
	},
	"Sales Channel": func(r *catRow) interface{} {
		if returnOriginal(r) != nil {
			return "Retail"
		}
		if o := r.master("SalesOrder", r.hs("original_order_id")); o != nil {
			return o["channel"]
		}
		return nil
	},
	"Credit Note No": func(r *catRow) interface{} {
		return firstNonEmpty(r.hs("credit_note_id"), r.hs("credit_note_number"))
	},
	"Reason": func(r *catRow) interface{} {
		return firstNonEmpty(r.ls("reason"), r.ls("disposition"), r.hs("reason"), r.hs("request_type"))
	},
}

func returnHeaderTotals(r *catRow) (qty, net, taxable, gst float64) {
	for i, l := range catLineList(r.H["items"]) {
		lr := &catRow{H: r.H, L: l, N: i + 1, src: returnLineShape, lk: r.lk}
		n, t, g, _ := returnLineMoney(lr)
		qty += catNum(l["qty"])
		net, taxable, gst = net+n, taxable+t, gst+g
	}
	return
}

var catReturns = &catSource{
	Label: "Sales Return", Doctype: "ReturnRequest", StoreField: "return_location", Where: "status NOT IN ('Rejected', 'Cancelled')",
	Number: "code",
	Cols: mergeCols(returnCols, map[string]catFn{
		"Return Qty":     func(r *catRow) interface{} { q, _, _, _ := returnHeaderTotals(r); return q },
		"Taxable Amount": func(r *catRow) interface{} { _, _, t, _ := returnHeaderTotals(r); return t },
		"GST Amount":     func(r *catRow) interface{} { _, _, _, g := returnHeaderTotals(r); return g },
		"Net Amount":     func(r *catRow) interface{} { _, n, _, _ := returnHeaderTotals(r); return n },
	}),
}

var catReturnLines = &catSource{
	Label: "Sales Return", Doctype: "ReturnRequest", StoreField: "return_location", Where: "status NOT IN ('Rejected', 'Cancelled')",
	Lines: "items", Number: "code", SKU: "sku", Qty: "qty",
	Cols: mergeCols(returnCols, map[string]catFn{
		"Return Qty": catLN("qty"),
		"MRP": func(r *catRow) interface{} {
			return firstPositive(catNum(r.item()["mrp"]), r.ln("original_unit_price"))
		},
		"Gross Value":         func(r *catRow) interface{} { _, t, _, _ := returnLineMoney(r); return t },
		"Item Taxable Amount": func(r *catRow) interface{} { _, t, _, _ := returnLineMoney(r); return t },
		"GST %":               func(r *catRow) interface{} { _, _, _, rt := returnLineMoney(r); return rt },
		"GST Amount":          func(r *catRow) interface{} { _, _, g, _ := returnLineMoney(r); return g },
		"Item Net Amount":     func(r *catRow) interface{} { n, _, _, _ := returnLineMoney(r); return n },
		"Customer Redeem Points": func(r *catRow) interface{} {
			return r.ln("points")
		},
		"Sales Return Amount": func(r *catRow) interface{} { n, _, _, _ := returnLineMoney(r); return n },
		"Net Sales Amount":    func(r *catRow) interface{} { n, _, _, _ := returnLineMoney(r); return -n },
		"Net Sales Qty":       func(r *catRow) interface{} { return -r.ln("qty") },
		"Document Date":       func(r *catRow) interface{} { return r.date() },
	}),
}

// --- tenders, edits, voids, advances, gift cards ---------------------------

// posTenderRows is one row per bill per tender.
func posTenderRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	bills, err := catPOSBills.docRows(schema, p, false)
	if err != nil {
		return nil, err
	}
	var out []*catRow
	for _, b := range bills {
		totals := cartTenderTotals(b.H)
		if len(totals) == 0 {
			totals = map[string]float64{firstNonEmpty(catStr(b.H["payment_tender"]), catStr(b.H["payment_mode"]), "Unrecorded"): catNum(b.H["amount_paid"])}
		}
		for _, kind := range catSortedKeys(totals) {
			r := *b
			r.L = map[string]interface{}{"tender": kind, "amount": totals[kind]}
			out = append(out, &r)
		}
	}
	return out, nil
}

func catSortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return catSorted(keys)
}

var catPOSTenders = &catSource{
	Label: "POS Bill", Doctype: "POSCart", Rows: posTenderRows,
	Number: "bill_number", Customer: "customer_id", CustomerName: "customer_name", CustomerPhone: "customer_phone",
	Cols: mergeCols(posSaleCols, map[string]catFn{
		"Tender Type":      catL("tender"),
		"Tender Amount":    catLN("amount"),
		"Net Sales Amount": catLN("amount"),
		"Reference No":     catH("payment_reference"),
		"Bill Count":       func(r *catRow) interface{} { return posBillNo(r) },
	}),
}

// Edits made to a bill after it was paid (DocumentEditHistory, Stage 73).
var catBillEdits = &catSource{
	Label: "Sales Bill", Doctype: "DocumentEditHistory", Lines: "changes", DateField: "edited_at", StoreOf: editedDocStore,
	Cols: map[string]catFn{
		"Sales Invoice Date": func(r *catRow) interface{} {
			if c := r.master(r.hs("document_type"), r.hs("document_id")); c != nil {
				return catDay(c["completed_at"])
			}
			return nil
		},
		"Sales Invoice No": func(r *catRow) interface{} { return firstNonEmpty(r.hs("document_number"), r.hs("document_id")) },
		"Audit ID":         func(r *catRow) interface{} { return fmt.Sprintf("%s-%d", firstNonEmpty(r.hs("code"), r.ID), r.N) },
		"Edit Action":      catConst("Edit"),
		"Field Changed":    func(r *catRow) interface{} { return firstNonEmpty(r.ls("label"), r.ls("field")) },
		"Old Value":        catL("before"),
		"New Value":        catL("after"),
		"Edit Reason":      catH("reason"),
		"Edited By":        catH("edited_by"),
		"Edited At":        func(r *catRow) interface{} { return r.hs("edited_at") },
	},
}

// editedDocStore is the store of the document an edit was made to.
func editedDocStore(r *catRow) string {
	if c := r.master(r.hs("document_type"), r.hs("document_id")); c != nil {
		return firstNonEmpty(catStr(c["location"]), catStr(c["location_code"]), catStr(c["return_location"]))
	}
	return ""
}

var catVoidBills = &catSource{
	Label: "POS Bill", Doctype: "POSCart", Where: "status IN ('Cancelled', 'Void', 'Voided')",
	Number: "bill_number", Customer: "customer_id", CustomerName: "customer_name",
	Cols: mergeCols(posSaleCols, map[string]catFn{
		"Bill Net Amount": func(r *catRow) interface{} {
			return firstPositive(r.hn("amount_paid"), posBillGST(r, "total_amount"))
		},
		"Reason": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("cancel_reason"), r.hs("void_reason"), r.hs("reason"))
		},
		"Approved By": func(r *catRow) interface{} { return firstNonEmpty(r.hs("cancel_approved_by"), r.hs("approved_by")) },
		"Approved At": func(r *catRow) interface{} { return firstNonEmpty(r.hs("cancel_approved_at"), r.hs("approved_at")) },
	}),
}

// advanceRows: one row per customer advance (Stage 85, a till tab).
func advanceRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	list, err := ListCustomerAdvances(tenantID, "", p["location"], false)
	if err != nil {
		return nil, err
	}
	var out []*catRow
	for _, a := range list {
		day := catDay(a.TakenAt)
		if (p["from"] != "" && day < p["from"]) || (p["to"] != "" && day > p["to"]) {
			continue
		}
		out = append(out, &catRow{ID: a.Number, Status: a.Status, CreatedBy: a.TakenBy, H: map[string]interface{}{
			"number": a.Number, "customer_id": a.CustomerID, "customer_name": a.CustomerName, "location": a.Location,
			"amount": a.Amount, "applied": a.Applied, "balance": a.Balance, "taken_at": a.TakenAt, "purpose": a.Purpose}})
	}
	return out, nil
}

var advanceCols = map[string]catFn{
	"Advance Date":            catHD("taken_at"),
	"Advance No":              catH("number"),
	"Advance Amount":          catHN("amount"),
	"Advance Adjusted Amount": catHN("applied"),
	"Advance Balance Amount":  catHN("balance"),
	"Advance Status":          func(r *catRow) interface{} { return r.Status },
}

var catAdvances = &catSource{Label: "Customer Advance", Rows: advanceRows, Number: "number", Customer: "customer_id", CustomerName: "customer_name", DateField: "taken_at", Cols: advanceCols}

// Every use of an advance or a gift card is a stored-value ledger event.
func storedValueEventRows(kind string) func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	return func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
		profileCond := "i.profile = '" + AdvanceProfileID + "'"
		if kind != "advance" {
			profileCond = "i.profile <> '" + AdvanceProfileID + "'"
		}
		q := fmt.Sprintf(`SELECT l.entry_id, l.event_type, l.amount_paise, l.balance_after_paise, COALESCE(l.source_doctype, ''), COALESCE(l.source_id, ''),
			COALESCE(l.location, ''), COALESCE(l.payment_mode, ''), COALESCE(l.user_name, ''), l.created_at, i.card_number, COALESCE(i.customer_id, ''), i.status
			FROM %[1]s.stored_value_ledger l JOIN %[1]s.stored_value_instruments i ON i.instrument_id = l.instrument_id
			WHERE %[2]s`, schema, profileCond)
		args := []interface{}{}
		if kind == "advance" {
			q += " AND l.event_type = '" + SVEventRedeem + "'" // used on a bill
		}
		if v := p["from"]; v != "" {
			args = append(args, v)
			q += fmt.Sprintf(" AND l.created_at >= $%d::date", len(args))
		}
		if v := p["to"]; v != "" {
			args = append(args, v)
			q += fmt.Sprintf(" AND l.created_at < ($%d::date + 1)", len(args))
		}
		if v := p["location"]; v != "" {
			args = append(args, v)
			q += fmt.Sprintf(" AND l.location = $%d", len(args))
		}
		rows, err := dbQuery(q+" ORDER BY l.created_at DESC LIMIT 20000", args...)
		if err != nil {
			return nil, nil // no stored-value tables on this client yet
		}
		defer rows.Close()
		var out []*catRow
		for rows.Next() {
			var id, event, srcType, srcID, loc, mode, user, card, cust, status string
			var amount, balance int64
			var at time.Time
			if rows.Scan(&id, &event, &amount, &balance, &srcType, &srcID, &loc, &mode, &user, &at, &card, &cust, &status) != nil {
				continue
			}
			out = append(out, &catRow{ID: id, Status: status, CreatedBy: user, Created: at, H: map[string]interface{}{
				"event": event, "amount": PaiseToRupees(amount), "balance": PaiseToRupees(balance), "source_doctype": srcType,
				"source_id": srcID, "location": loc, "payment_mode": mode, "card": card, "customer_id": cust}})
		}
		return out, rows.Err()
	}
}

func svBillNo(r *catRow) interface{} {
	if r.hs("source_doctype") == "POSCart" {
		if c := r.master("POSCart", r.hs("source_id")); c != nil {
			return firstNonEmpty(catStr(c["bill_number"]), r.hs("source_id"))
		}
	}
	return r.hs("source_id")
}

var catAdvanceUses = &catSource{
	Label: "Advance adjustment", Rows: storedValueEventRows("advance"), Customer: "customer_id",
	Cols: map[string]catFn{
		"Advance No":              catH("card"),
		"Sales Invoice Date":      func(r *catRow) interface{} { return catDay(r.Created) },
		"Sales Invoice No":        svBillNo,
		"Advance Adjusted Amount": func(r *catRow) interface{} { return -r.hn("amount") },
		"Advance Balance Amount":  catHN("balance"),
	},
}

var catGiftCards = &catSource{
	Label: "Gift card", Rows: storedValueEventRows("giftcard"), Customer: "customer_id",
	Cols: map[string]catFn{
		"Gift Card No":     func(r *catRow) interface{} { return MaskCardNumber(r.hs("card")) }, // last four only, as everywhere
		"Gift Card Amount": catHN("amount"),
		"Sales Invoice No": svBillNo,
		"Tender Type":      func(r *catRow) interface{} { return firstNonEmpty(r.hs("payment_mode"), r.hs("event")) },
		"Status":           func(r *catRow) interface{} { return r.hs("event") + " (" + r.Status + ")" },
	},
}

var catCreditNotes = &catSource{
	Label: "Credit Note", Doctype: "CreditNote", StoreOf: creditNoteStore, Number: "note_number", Customer: "customer_id", Amount: "amount",
	Statuses: []string{"Draft", "Approved", "Posted", "Applied", "Cancelled"},
	Cols: map[string]catFn{
		"Credit Note No":     func(r *catRow) interface{} { return r.number() },
		"Credit Note Amount": catHN("amount"),
		"Original Bill No": func(r *catRow) interface{} {
			if c := r.master("POSCart", r.hs("reference_cart")); c != nil {
				return firstNonEmpty(catStr(c["bill_number"]), r.hs("reference_cart"))
			}
			return r.hs("reference_cart")
		},
		"Sales Invoice No": func(r *catRow) interface{} {
			if i := r.master("SalesInvoice", r.hs("sales_invoice_id")); i != nil {
				return firstNonEmpty(catStr(i["invoice_number"]), r.hs("sales_invoice_id"))
			}
			return r.hs("sales_invoice_id")
		},
		"Store Code": func(r *catRow) interface{} { return creditNoteStore(r) },
		"Store Name": func(r *catRow) interface{} { return r.nameOf("Location", creditNoteStore(r)) },
		"Legal Entity": func(r *catRow) interface{} {
			if s := r.master("Location", creditNoteStore(r)); s != nil {
				return r.nameOf("LegalEntity", catStr(s["legal_entity"]))
			}
			return nil
		},
	},
}

func creditNoteStore(r *catRow) string {
	if c := r.master("POSCart", r.hs("reference_cart")); c != nil {
		return catStr(c["location"])
	}
	if i := r.master("SalesInvoice", r.hs("sales_invoice_id")); i != nil {
		return catStr(i["location"])
	}
	return ""
}

func posHasGSTIN(r *catRow) bool {
	return catStr(r.customer()["gstin"]) != ""
}

func invoiceExport(r *catRow) bool {
	if cur := r.hs("currency"); cur != "" && cur != "INR" {
		return true
	}
	if o := r.master("SalesOrder", r.hs("sales_order_id")); o != nil {
		c := strings.ToUpper(catStr(o["customer_country"]))
		return c != "" && c != "IN" && c != "INDIA"
	}
	return false
}

var _ = catWire("cat-sales-register", &catReport{Union: []*catSource{catPOSBills, catSalesInvoices}})
var _ = catWire("cat-sales-itemwise-detail", &catReport{Src: catPOSLines})
var _ = catWire("cat-sales-return-register", &catReport{Src: catReturns})
var _ = catWire("cat-sales-return-itemwise-detail", &catReport{Src: catReturnLines})
var _ = catWire("cat-net-sales-summary", &catReport{Union: []*catSource{catPOSLines, catReturnLines},
	Group: []string{"Document Date", "Store Code", "Store Name", "Sales Channel"},
	Agg:   map[string]string{"Bill Count": "countd"},
	Cols: map[string]catFn{
		"GST Amount": func(r *catRow) interface{} { // a return gives its GST back
			if r.src == catReturnLines {
				_, _, g, _ := returnLineMoney(r)
				return -g
			}
			return posLine(r).gst
		},
		"Sold Qty": func(r *catRow) interface{} {
			if r.src == catPOSLines {
				return r.ln("qty")
			}
			return 0.0
		},
		"Return Qty": func(r *catRow) interface{} {
			if r.src == catReturnLines {
				return r.ln("qty")
			}
			return 0.0
		},
		"Bill Count": func(r *catRow) interface{} {
			if r.src == catPOSLines {
				return posBillNo(r)
			}
			return nil
		},
	},
})
var _ = catWire("cat-sales-tender-summary", &catReport{Src: catPOSTenders,
	Group: []string{"Document Date", "Store Code", "Store Name", "Tender Type"}, Agg: map[string]string{"Bill Count": "countd"}})
var _ = catWire("cat-sales-tender-detail", &catReport{Src: catPOSTenders})
var _ = catWire("cat-salesman-sales-summary", &catReport{Src: catPOSLines,
	Cols:  map[string]catFn{"ABV": catPost, "UPT": catPost},
	Group: []string{"From Date", "To Date", "Store Code", "Store Name", "Salesperson ID", "Salesperson Name"},
	Agg:   map[string]string{"Bill Count": "countd"},
	Post: func(row map[string]interface{}) {
		bills := catNum(row["Bill Count"])
		row["ABV"], row["UPT"] = catDiv(catNum(row["Net Sales Amount"]), bills), catDiv(catNum(row["Sold Qty"]), bills)
	},
})
var _ = catWire("cat-sales-counter-summary", &catReport{Union: []*catSource{catPOSLines, catReturnLines},
	Group: []string{"From Date", "To Date", "Store Code", "Store Name", "Counter"},
	Agg:   map[string]string{"Bill Count": "countd"},
	Cols: map[string]catFn{
		"Counter": func(r *catRow) interface{} {
			if r.src == catReturnLines {
				return "Returns"
			}
			return posSaleCols["Counter"](r)
		},
		"Sold Qty": func(r *catRow) interface{} {
			if r.src == catPOSLines {
				return r.ln("qty")
			}
			return 0.0
		},
		"Bill Count": func(r *catRow) interface{} {
			if r.src == catPOSLines {
				return posBillNo(r)
			}
			return nil
		},
	},
})
var _ = catWire("cat-sales-discount-analysis", &catReport{Src: catPOSLines,
	Keep: func(r *catRow) bool { m := posLine(r); return m.itemDisc+m.billDisc+m.points > 0.004 }})
var _ = catWire("cat-sales-margin-analysis", &catReport{Src: catPOSLines})
var _ = catWire("cat-sales-bill-edit-audit-report", &catReport{Src: catBillEdits})
var _ = catWire("cat-cancelled-void-bill-register", &catReport{Src: catVoidBills})
var _ = catWire("cat-customer-advance-summary", &catReport{Src: catAdvances,
	Cols: map[string]catFn{"Sales Invoice No": catNotHeld}})
var _ = catWire("cat-customer-advance-itemwise-detail", &catReport{Src: catAdvances})
var _ = catWire("cat-customer-advance-utilization", &catReport{Src: catAdvanceUses})
var _ = catWire("cat-credit-note-register", &catReport{Src: catCreditNotes})
var _ = catWire("cat-gift-card-voucher-register", &catReport{Src: catGiftCards})
var _ = catWire("cat-b2b-sales-register", &catReport{Union: []*catSource{catPOSBills, catSalesInvoices},
	Keep: func(r *catRow) bool {
		if r.src == catPOSBills {
			return posHasGSTIN(r)
		}
		return !invoiceExport(r)
	},
	Cols: map[string]catFn{"Customer GSTIN": func(r *catRow) interface{} {
		if g := catStr(r.customer()["gstin"]); g != "" {
			return g
		}
		return r.hv("customer_gstin")
	}},
})
var _ = catWire("cat-export-international-sales-register", &catReport{Src: catSalesInvoices, Keep: invoiceExport})
var _ = catWire("cat-sales-design-image-register", &catReport{Src: catPOSLines,
	Group: []string{"Legal Entity", "Store Code", "Store Name", "Sales Invoice Date", "Sales Invoice No", "Design No"}})
var _ = catWire("cat-sales-variant-image-register", &catReport{Src: catPOSLines})

// The line fields alone, for helpers that walk a document's lines.
var posLineShape = &catSource{SKU: "sku", Qty: "qty", MRP: "reference_price"}
var returnLineShape = &catSource{SKU: "sku", Qty: "qty"}
