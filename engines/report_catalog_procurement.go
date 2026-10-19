package engines

import (
	"fmt"
	"strings"
	"time"
)

// Catalog reports - Procurement & GRN (Stage 75.3.5, Wave A).
//
// A PurchaseOrder keeps its lines in "items" (sku, qty, rate, mrp) and its
// GST in gst_breakdown; a GRN receives against one or more POs, line by line
// in "received_items" (ordered / received / accepted / rejected / damaged
// qty, the PO rate and the vendor's invoice rate, the reason); the vendor's
// invoice numbers and totals sit on the GRN. Returns, debit notes and vendor
// credit notes are their own documents. The two Receipt Note reports wait
// for Stage 74's receipt note.

// poReceipt is what the GRNs received against one PO line.
type poReceipt struct {
	received, accepted, rejected float64
	grns                         []string
	lastDate                     string
}

// poReceipts reads every GRN once per run: PO + SKU -> received.
func poReceipts(r *catRow) map[string]*poReceipt {
	if m, ok := r.lk.extra["poreceipts"].(map[string]*poReceipt); ok {
		return m
	}
	m := map[string]*poReceipt{}
	rows, err := dbQuery(fmt.Sprintf(`SELECT id, COALESCE(data->>'code', id), COALESCE(data->>'po_id', ''), COALESCE(data->>'received_items', data->>'items', '[]'),
		to_char(created_at, 'YYYY-MM-DD') FROM %s.documents WHERE doctype = 'GRN' AND deleted_at IS NULL AND status NOT IN ('Cancelled', 'Rejected', 'Draft')
		ORDER BY created_at`, r.lk.schema))
	if err == nil {
		for rows.Next() {
			var id, code, po, lines, day string
			if rows.Scan(&id, &code, &po, &lines, &day) != nil {
				continue
			}
			for _, l := range catLineList(lines) {
				key := firstNonEmpty(catStr(l["po_id"]), po) + "/" + catStr(l["sku"])
				pr := m[key]
				if pr == nil {
					pr = &poReceipt{}
					m[key] = pr
				}
				q := catNum(l["qty"])
				pr.received += q
				if _, has := l["accepted_qty"]; has {
					pr.accepted += catNum(l["accepted_qty"])
				} else {
					pr.accepted += q
				}
				pr.rejected += catNum(l["rejected_qty"]) + catNum(l["damaged_qty"])
				if len(pr.grns) == 0 || pr.grns[len(pr.grns)-1] != code {
					pr.grns = append(pr.grns, code)
				}
				pr.lastDate = day
			}
		}
		rows.Close()
	}
	r.lk.extra["poreceipts"] = m
	return m
}

func (r *catRow) poReceipt() *poReceipt {
	if pr := poReceipts(r)[r.ID+"/"+r.sku()]; pr != nil {
		return pr
	}
	return &poReceipt{}
}

// poHeaderReceipt adds up every line of the PO.
func poHeaderReceipt(r *catRow) (ordered, received float64) {
	m := poReceipts(r)
	for _, l := range catLineList(r.H["items"]) {
		ordered += catNum(l["qty"])
		if pr := m[r.ID+"/"+catStr(l["sku"])]; pr != nil {
			received += pr.accepted
		}
	}
	return
}

func poGSTRate(r *catRow) float64 {
	if v := r.ln("gst_rate"); v != 0 {
		return v
	}
	return catNum(r.item()["gst_rate"])
}

// poLineBasic is the line's value before GST (rates may include GST).
func poLineBasic(r *catRow) float64 {
	v := r.ln("rate") * r.ln("qty")
	if strings.EqualFold(r.hs("gst_mode"), "Inclusive") {
		return v / (1 + poGSTRate(r)/100)
	}
	return v
}

func poBreakdown(r *catRow, key string) float64 {
	if gb, ok := r.H["gst_breakdown"].(map[string]interface{}); ok {
		return catNum(gb[key])
	}
	return 0
}

var poCols = map[string]catFn{
	// newer POs carry vendor_id, older ones vendor
	"Vendor Code": func(r *catRow) interface{} { return firstNonEmpty(r.hs("vendor_id"), r.hs("vendor")) },
	"Vendor Name": func(r *catRow) interface{} {
		return r.nameOf("Vendor", firstNonEmpty(r.hs("vendor_id"), r.hs("vendor")))
	},
	"Vendor GSTIN": func(r *catRow) interface{} {
		return r.master("Vendor", firstNonEmpty(r.hs("vendor_id"), r.hs("vendor")))["gstin"]
	},
	"PO Date":   func(r *catRow) interface{} { return r.date() },
	"PO No":     func(r *catRow) interface{} { return r.number() },
	"PO Status": func(r *catRow) interface{} { return r.Status },
	"PO Qty": func(r *catRow) interface{} {
		if r.L != nil {
			return r.ln("qty")
		}
		o, _ := poHeaderReceipt(r)
		return o
	},
	"Received Qty": func(r *catRow) interface{} {
		if r.L != nil {
			return r.poReceipt().accepted
		}
		_, rec := poHeaderReceipt(r)
		return rec
	},
	"Pending Qty": func(r *catRow) interface{} {
		if r.L != nil {
			return mathMax0(r.ln("qty") - r.poReceipt().accepted)
		}
		o, rec := poHeaderReceipt(r)
		return mathMax0(o - rec)
	},
	"PO Rate": catLN("rate"),
	"PO Basic Value": func(r *catRow) interface{} {
		if r.L != nil {
			return poLineBasic(r)
		}
		return firstPositive(poBreakdown(r, "taxable_amount"), r.hn("total_amount"))
	},
	"PO GST Amount": func(r *catRow) interface{} {
		if r.L != nil {
			return poLineBasic(r) * poGSTRate(r) / 100
		}
		return poBreakdown(r, "total_tax")
	},
	"PO Gross Value": func(r *catRow) interface{} {
		if r.L != nil {
			return poLineBasic(r) * (1 + poGSTRate(r)/100)
		}
		return firstPositive(r.hn("grand_total"), poBreakdown(r, "total_amount"), r.hn("total_amount"))
	},
	"GST %": func(r *catRow) interface{} { return poGSTRate(r) },
	"GRN No": func(r *catRow) interface{} {
		return strings.Join(r.poReceipt().grns, ", ")
	},
	"GRN Date":          func(r *catRow) interface{} { return r.poReceipt().lastDate },
	"Receipt Note No":   func(r *catRow) interface{} { return strings.Join(r.poReceipt().grns, ", ") },
	"Receipt Note Date": func(r *catRow) interface{} { return r.poReceipt().lastDate },
	"Status": func(r *catRow) interface{} {
		if r.L != nil && r.ln("qty") > 0 && r.poReceipt().accepted >= r.ln("qty") {
			return "Received"
		}
		return r.Status
	},
}

// vendorInvoiceVariance adds the invoice-vs-receipt difference of the GRNs
// made against this PO.
func vendorInvoiceVariance(r *catRow) float64 {
	m, ok := r.lk.extra["povariance"].(map[string]float64)
	if !ok {
		m = map[string]float64{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT COALESCE(data->>'po_id', ''), COALESCE(SUM(NULLIF(data->>'invoice_amount_difference', '')::numeric), 0)
			FROM %s.documents WHERE doctype = 'GRN' AND deleted_at IS NULL AND status NOT IN ('Cancelled', 'Rejected')
			AND COALESCE(data->>'invoice_amount_difference', '') ~ '^-?[0-9.]+$' GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var po string
				var v float64
				if rows.Scan(&po, &v) == nil {
					m[po] = v
				}
			}
			rows.Close()
		}
		r.lk.extra["povariance"] = m
	}
	return m[r.ID]
}

func mathMax0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

var catPOs = &catSource{Label: "Purchase Order", Doctype: "PurchaseOrder", Number: "po_number", Vendor: "vendor_id",
	Statuses: []string{"Draft", "Pending Approval", "Approved", "Partially Received", "Received", "Closed", "Rejected", "Cancelled"},
	Cols:     poCols}

var catPOLines = &catSource{Label: "Purchase Order", Doctype: "PurchaseOrder", Lines: "items", Number: "po_number", Vendor: "vendor_id",
	SKU: "sku", Qty: "qty", Rate: "rate", MRP: "mrp",
	Statuses: []string{"Draft", "Pending Approval", "Approved", "Partially Received", "Received", "Closed", "Rejected", "Cancelled"},
	Cols:     poCols}

func poOpen(r *catRow) bool {
	switch r.Status {
	case "Approved", "Partially Received", "Pending Approval", "Open":
		return true
	}
	return false
}

// poExpired: an open PO past its quote's validity, or older than 90 days -
// the ERP holds no other PO expiry.
func poExpired(r *catRow) (bool, string) {
	if !poOpen(r) {
		return false, ""
	}
	today := time.Now().Format("2006-01-02")
	if v := catDay(r.hv("quote_valid_until")); v != "" && v < today {
		return true, "Past the awarded quote's validity (" + v + ")"
	}
	if r.since(r.Created) > 90*24*time.Hour {
		return true, fmt.Sprintf("Open for %d days", int(r.since(r.Created).Hours()/24))
	}
	return false, ""
}

// --- GRNs -------------------------------------------------------------------

func grnPONo(r *catRow) interface{} {
	ids := []string{}
	if r.L != nil {
		if id := r.ls("po_id"); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		if id := r.hs("po_id"); id != "" {
			ids = append(ids, id)
		}
		for _, id := range strings.Split(strings.Trim(r.hs("po_ids"), "[]\""), ",") {
			id = strings.Trim(strings.TrimSpace(id), "\"")
			if id != "" && (len(ids) == 0 || ids[0] != id) {
				ids = append(ids, id)
			}
		}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if po := r.master("PurchaseOrder", id); po != nil {
			out = append(out, firstNonEmpty(catStr(po["po_number"]), id))
		} else {
			out = append(out, id)
		}
	}
	return strings.Join(out, ", ")
}

func grnInterstate(r *catRow) bool {
	if po := r.master("PurchaseOrder", r.hs("po_id")); po != nil {
		return po["interstate"] == true
	}
	return false
}

func grnLineRate(r *catRow) float64 { return firstPositive(r.ln("invoice_rate"), r.ln("po_rate")) }

func grnLineTaxable(r *catRow) float64 { return grnLineRate(r) * r.ln("qty") }

func grnLineGSTRate(r *catRow) float64 {
	if v := r.ln("gst_rate"); v != 0 {
		return v
	}
	return catNum(r.item()["gst_rate"])
}

func grnTotals(r *catRow) (received, accepted, rejected float64) {
	for _, l := range catLineList(r.H["received_items"]) {
		q := catNum(l["qty"])
		received += q
		if _, has := l["accepted_qty"]; has {
			accepted += catNum(l["accepted_qty"])
		} else {
			accepted += q
		}
		rejected += catNum(l["rejected_qty"]) + catNum(l["damaged_qty"])
	}
	return
}

func grnHalfTax(r *catRow, igst bool) interface{} {
	tax := r.hn("invoice_gst")
	if grnInterstate(r) != igst {
		return 0.0
	}
	if igst {
		return tax
	}
	return tax / 2
}

var grnCols = map[string]catFn{
	"GRN Date":            func(r *catRow) interface{} { return r.date() },
	"GRN No":              func(r *catRow) interface{} { return r.number() },
	"GRN Status":          func(r *catRow) interface{} { return r.Status },
	"PO No":               grnPONo,
	"Vendor Invoice No":   catH("vendor_invoice_no"),
	"Vendor Invoice Date": catHD("vendor_invoice_date"),
	"Received Qty": func(r *catRow) interface{} {
		if r.L != nil {
			return r.ln("qty")
		}
		v, _, _ := grnTotals(r)
		return v
	},
	"Accepted Qty": func(r *catRow) interface{} {
		if r.L != nil {
			if _, has := r.L["accepted_qty"]; has {
				return r.ln("accepted_qty")
			}
			return r.ln("qty")
		}
		_, v, _ := grnTotals(r)
		return v
	},
	"Rejected Qty": func(r *catRow) interface{} {
		if r.L != nil {
			return r.ln("rejected_qty") + r.ln("damaged_qty")
		}
		_, _, v := grnTotals(r)
		return v
	},
	"PO Qty":       catLN("ordered_qty"),
	"Invoice Qty":  catLN("qty"),
	"PO Rate":      catLN("po_rate"),
	"Invoice Rate": catLN("invoice_rate"),
	"Rate Variance": func(r *catRow) interface{} {
		if r.ln("invoice_rate") == 0 {
			return nil
		}
		return r.ln("invoice_rate") - r.ln("po_rate")
	},
	"Amount Variance": func(r *catRow) interface{} {
		if r.L == nil {
			return r.hn("invoice_amount_difference")
		}
		if r.ln("invoice_rate") == 0 {
			return 0.0
		}
		return (r.ln("invoice_rate") - r.ln("po_rate")) * r.ln("qty")
	},
	"Purchase Price": func(r *catRow) interface{} { return grnLineRate(r) },
	"Silver + Making Cost / Gram": func(r *catRow) interface{} {
		if w := r.ln("actual_weight"); w > 0 {
			return grnLineRate(r) / w
		}
		return nil
	},
	"Taxable Amount": func(r *catRow) interface{} {
		if r.L != nil {
			return grnLineTaxable(r)
		}
		return firstPositive(r.hn("invoice_value"), r.hn("po_value"))
	},
	"GST %": func(r *catRow) interface{} { return grnLineGSTRate(r) },
	"GST Amount": func(r *catRow) interface{} {
		if r.L != nil {
			return grnLineTaxable(r) * grnLineGSTRate(r) / 100
		}
		return r.hn("invoice_gst")
	},
	"CGST Amount": func(r *catRow) interface{} { return grnHalfTax(r, false) },
	"SGST Amount": func(r *catRow) interface{} { return grnHalfTax(r, false) },
	"IGST Amount": func(r *catRow) interface{} { return grnHalfTax(r, true) },
	"Net Amount": func(r *catRow) interface{} {
		if r.L != nil {
			return grnLineTaxable(r) * (1 + grnLineGSTRate(r)/100)
		}
		return firstPositive(r.hn("invoice_total"), r.hn("vendor_invoice_amount"))
	},
	"Debit Note No": func(r *catRow) interface{} {
		if d := r.master("DebitNote", r.hs("debit_note_id")); d != nil {
			return firstNonEmpty(catStr(d["note_number"]), r.hs("debit_note_id"))
		}
		return r.hs("debit_note_id")
	},
	"Credit Note No (Vendor)": func(r *catRow) interface{} {
		if d := r.master("VendorCreditNote", r.hs("vendor_credit_note_id")); d != nil {
			return firstNonEmpty(catStr(d["note_number"]), r.hs("vendor_credit_note_id"))
		}
		return r.hs("vendor_credit_note_id")
	},
	"Reason": func(r *catRow) interface{} {
		return firstNonEmpty(r.ls("rejection_reason"), r.ls("damage_reason"), r.hs("cancel_reason"), r.hs("reason"))
	},
	"Reason Code": func(r *catRow) interface{} {
		switch {
		case r.ln("damaged_qty") > 0:
			return "DAMAGED"
		case r.ln("rejected_qty") > 0:
			return "REJECTED"
		}
		return nil
	},
	"User Name": func(r *catRow) interface{} { return r.CreatedBy },
	"Status": func(r *catRow) interface{} {
		if r.L != nil && r.ln("invoice_rate") != 0 && r.ln("invoice_rate") != r.ln("po_rate") {
			return "Rate differs"
		}
		return r.Status
	},
}

var catGRNs = &catSource{Label: "GRN", Doctype: "GRN", Number: "code", Vendor: "vendor",
	Statuses: []string{"Draft", "Pending Approval", "Received", "Approved", "Posted", "Cancelled"}, Cols: grnCols}
var catGRNLines = &catSource{Label: "GRN", Doctype: "GRN", Lines: "received_items", Number: "code", Vendor: "vendor",
	SKU: "sku", Qty: "qty", Rate: "invoice_rate", MRP: "mrp",
	Statuses: []string{"Draft", "Pending Approval", "Received", "Approved", "Posted", "Cancelled"}, Cols: grnCols}

// --- returns and notes --------------------------------------------------------

func purchaseReturnNote(r *catRow) map[string]interface{} {
	return r.master("DebitNote", r.hs("debit_note_id"))
}

func purchaseReturnTaxRatio(r *catRow) (taxable, gst float64) {
	if n := purchaseReturnNote(r); n != nil {
		return catNum(n["amount"]), catNum(n["gst_amount"])
	}
	taxable = r.hn("total_amount")
	return taxable, 0
}

var purchaseReturnCols = map[string]catFn{
	"Return From Outlet":               catStoreName,
	"Transaction Source Location":      catStoreName,
	"Transaction Destination Location": func(r *catRow) interface{} { return r.nameOf("Vendor", r.hs("vendor_id")) },
	"Qty": func(r *catRow) interface{} {
		if r.L != nil {
			return r.ln("qty")
		}
		return catSumLines("return_items", "qty")(r)
	},
	"Taxable Amount": func(r *catRow) interface{} {
		if r.L != nil {
			return firstPositive(r.ln("line_value"), r.ln("unit_cost")*r.ln("qty"))
		}
		t, _ := purchaseReturnTaxRatio(r)
		return t
	},
	"GST Amount": func(r *catRow) interface{} {
		if r.L != nil {
			v := firstPositive(r.ln("line_value"), r.ln("unit_cost")*r.ln("qty"))
			return v * catNum(r.item()["gst_rate"]) / 100
		}
		_, g := purchaseReturnTaxRatio(r)
		return g
	},
	"Net Amount": func(r *catRow) interface{} {
		if r.L != nil {
			v := firstPositive(r.ln("line_value"), r.ln("unit_cost")*r.ln("qty"))
			return v * (1 + catNum(r.item()["gst_rate"])/100)
		}
		if n := purchaseReturnNote(r); n != nil {
			return catNum(n["total_with_gst"])
		}
		return r.hn("total_amount")
	},
	"Purchase Price": catLN("unit_cost"),
	"GRN No": func(r *catRow) interface{} {
		if g := r.master("GRN", r.hs("grn_id")); g != nil {
			return firstNonEmpty(catStr(g["code"]), r.hs("grn_id"))
		}
		return r.hs("grn_id")
	},
	"Debit Note No": func(r *catRow) interface{} {
		return firstNonEmpty(catStr(purchaseReturnNote(r)["note_number"]), r.hs("debit_note_id"))
	},
	"Reason": func(r *catRow) interface{} { return firstNonEmpty(r.ls("stock_bucket"), r.hs("reason")) },
}

var catPurchaseReturns = &catSource{Label: "Purchase Return", Doctype: "PurchaseReturn", Number: "code", Vendor: "vendor_id", Cols: purchaseReturnCols}
var catPurchaseReturnLines = &catSource{Label: "Purchase Return", Doctype: "PurchaseReturn", Lines: "return_items", Number: "code", Vendor: "vendor_id",
	SKU: "sku", Qty: "qty", Rate: "unit_cost", Cols: purchaseReturnCols}

func vendorNoteCols(numberHeader string) map[string]catFn {
	return map[string]catFn{
		numberHeader: func(r *catRow) interface{} { return r.number() },
		"GRN No": func(r *catRow) interface{} {
			if g := r.master("GRN", r.hs("grn_id")); g != nil {
				return firstNonEmpty(catStr(g["code"]), r.hs("grn_id"))
			}
			return r.hs("grn_id")
		},
		"Vendor Invoice No": catH("vendor_invoice_no"),
		"Taxable Amount":    catHN("amount"),
		"GST Amount":        catHN("gst_amount"),
		"Net Amount": func(r *catRow) interface{} {
			return firstPositive(r.hn("total_with_gst"), r.hn("amount")+r.hn("gst_amount"))
		},
	}
}

func noteStore(r *catRow) string {
	if g := r.master("GRN", r.hs("grn_id")); g != nil {
		return catStr(g["location"])
	}
	if po := r.master("PurchaseOrder", r.hs("reference_po")); po != nil {
		return catStr(po["location"])
	}
	return ""
}

var catDebitNotes = &catSource{Label: "Debit Note", Doctype: "DebitNote", Number: "note_number", Vendor: "vendor_id", StoreOf: noteStore,
	Cols: vendorNoteCols("Debit Note No")}
var catVendorCreditNotes = &catSource{Label: "Vendor Credit Note", Doctype: "VendorCreditNote", Number: "note_number", Vendor: "vendor_id", StoreOf: noteStore,
	Cols: vendorNoteCols("Credit Note No (Vendor)")}

// --- requisitions, quotes, amendments -----------------------------------------

var catRequisitions = &catSource{Label: "Purchase Requisition", Doctype: "PurchaseRequisition", Number: "code", StoreField: "-", Qty: "quantity",
	Cols: map[string]catFn{
		"Purchase Requisition No": func(r *catRow) interface{} { return r.number() },
		"Item Code":               func(r *catRow) interface{} { return r.hs("sku") },
		"Item Name":               func(r *catRow) interface{} { return firstNonEmpty(r.nameOf("Item", r.hs("sku")), r.hs("description")) },
		"Category Name":           func(r *catRow) interface{} { return r.hs("department") },
		"Reason":                  catH("description"),
	}}

var catQuoteLines = &catSource{Label: "Vendor Quote", Doctype: "VendorQuote", Lines: "lines", Number: "code", Vendor: "vendor", StoreField: "-",
	SKU: "sku", Qty: "qty", Rate: "unit_price",
	Cols: map[string]catFn{
		"RFQ No": func(r *catRow) interface{} {
			if q := r.master("RFQ", r.hs("rfq_id")); q != nil {
				return firstNonEmpty(catStr(q["code"]), r.hs("rfq_id"))
			}
			return r.hs("rfq_id")
		},
		"Quotation No": func(r *catRow) interface{} { return r.number() },
		"Vendor Name": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("vendor_name"), r.nameOf("Vendor", r.hs("vendor")))
		},
		"Item Name": func(r *catRow) interface{} { return firstNonEmpty(catStr(r.item()["name"]), r.ls("description")) },
		"PO Qty":    catLN("qty"),
		"PO Rate":   catLN("unit_price"),
		"Landed Cost": func(r *catRow) interface{} {
			// the quote's freight shared by line value
			sub := r.hn("subtotal")
			if sub == 0 {
				return r.ln("amount")
			}
			return r.ln("amount") * (1 + r.hn("freight_amount")/sub)
		},
		"GST %":          catLN("tax_rate"),
		"PO Gross Value": func(r *catRow) interface{} { return r.ln("amount") + r.ln("tax") },
	}}

var catPOAmendments = &catSource{Label: "PO amendment", Rows: auditChangeRows([]string{"PurchaseOrder"}, nil), StoreField: "-",
	Cols: map[string]catFn{
		"PO No": func(r *catRow) interface{} {
			if po := r.master("PurchaseOrder", r.hs("doc_id")); po != nil {
				return firstNonEmpty(catStr(po["po_number"]), r.hs("doc_id"))
			}
			return r.hs("doc_id")
		},
		"PO Date": func(r *catRow) interface{} { return nil },
		"Amendment No": func(r *catRow) interface{} {
			if po := r.master("PurchaseOrder", r.hs("doc_id")); po != nil {
				return po["version"]
			}
			return nil
		},
		"Amendment Reason": func(r *catRow) interface{} { return r.master("PurchaseOrder", r.hs("doc_id"))["amendment_reason"] },
		"Field Changed":    catH("field"),
		"Old Value":        catH("old"),
		"New Value":        catH("new"),
		"Edited At":        func(r *catRow) interface{} { return catStamp(r.Created) },
		"Approved By":      catNotHeld,
	}}

// Only real amendments: the PO's own lines, money, vendor and dates - not
// the status and bookkeeping fields every save touches.
var poAmendmentFields = map[string]bool{"items": true, "grand_total": true, "total_amount": true, "vendor": true, "vendor_id": true,
	"location": true, "target_warehouse": true, "payment_terms": true, "freight_amount": true, "gst_mode": true, "place_of_supply": true, "currency": true}

var _ = catWire("cat-purchase-requisition-register", &catReport{Src: catRequisitions})
var _ = catWire("cat-rfq-quotation-comparison", &catReport{Src: catQuoteLines})
var _ = catWire("cat-purchase-order-register", &catReport{Src: catPOs})
var _ = catWire("cat-purchase-order-itemwise-detail", &catReport{Src: catPOLines})
var _ = catWire("cat-pending-open-po-report", &catReport{Src: catPOLines,
	Keep: func(r *catRow) bool { return poOpen(r) && r.ln("qty")-r.poReceipt().accepted > 0 }})
var _ = catWire("cat-pending-po-with-image", &catReport{Src: catPOLines,
	Keep:  func(r *catRow) bool { return poOpen(r) && r.ln("qty")-r.poReceipt().accepted > 0 },
	Group: []string{"PO Date", "PO No", "Vendor Name", "Design No"},
	Cols:  map[string]catFn{"Image URL": func(r *catRow) interface{} { return catItemImage(r) }}})
var _ = catWire("cat-pending-po-with-variant-image", &catReport{Src: catPOLines,
	Keep: func(r *catRow) bool { return poOpen(r) && r.ln("qty")-r.poReceipt().accepted > 0 },
	Cols: map[string]catFn{"Image URL": func(r *catRow) interface{} { return catItemImage(r) }}})
var _ = catWire("cat-po-approval-pending-report", &catReport{Src: catPOs,
	Keep: func(r *catRow) bool { return r.Status == "Pending Approval" || r.Status == "Draft" }})
var _ = catWire("cat-po-amendment-history", &catReport{Src: catPOAmendments,
	Keep: func(r *catRow) bool { return poAmendmentFields[r.hs("field")] }})
var _ = catWire("cat-cancelled-po-register", &catReport{Src: catPOs,
	Keep: func(r *catRow) bool { return r.Status == "Cancelled" || r.hv("short_closed") == true },
	Cols: map[string]catFn{
		"Cancelled Qty": func(r *catRow) interface{} {
			if r.hv("short_closed") == true {
				return catSumLines("short_closed_lines", "short_closed")(r)
			}
			o, rec := poHeaderReceipt(r)
			return mathMax0(o - rec)
		},
		"Reason": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("short_close_reason_code"), r.hs("short_close_note"), r.hs("cancel_reason"), r.hs("reason"))
		},
		"User Name": func(r *catRow) interface{} { return firstNonEmpty(r.hs("short_closed_by"), r.CreatedBy) },
		"Status": func(r *catRow) interface{} {
			if r.hv("short_closed") == true {
				return "Short closed"
			}
			return r.Status
		},
	}})
var _ = catWire("cat-expired-po-report", &catReport{Src: catPOs,
	Keep: func(r *catRow) bool { ok, _ := poExpired(r); return ok },
	Cols: map[string]catFn{"Remarks": func(r *catRow) interface{} { _, why := poExpired(r); return why }}})
var _ = catWire("cat-po-vs-grn-status", &catReport{Src: catPOLines})

// Per vendor, from the PO lines of the period: ordered, received (accepted),
// rejected at the GRN and still pending; Amount Variance is what the
// vendor's invoices differed from the goods received (GRN), once per GRN.
var _ = catWire("cat-vendor-performance-summary", &catReport{Src: catPOLines,
	Keep:  func(r *catRow) bool { return r.Status != "Draft" && r.Status != "Rejected" && r.Status != "Cancelled" },
	Group: []string{"From Date", "To Date", "Vendor Code", "Vendor Name"},
	Cols: map[string]catFn{
		"Rejected Qty": func(r *catRow) interface{} { return r.poReceipt().rejected },
		"Amount Variance": func(r *catRow) interface{} {
			if r.N != 1 {
				return 0.0 // the PO's GRNs are counted once, on its first line
			}
			return vendorInvoiceVariance(r)
		},
		"Status": catPost,
	},
	Post: func(row map[string]interface{}) {
		if po := catNum(row["PO Qty"]); po > 0 {
			row["Status"] = fmt.Sprintf("Fill rate %.0f%%, rejection %.0f%%", catNum(row["Received Qty"])/po*100, catNum(row["Rejected Qty"])/firstPositive(catNum(row["Received Qty"]), 1)*100)
		}
	}})
var _ = catWire("cat-purchase-register", &catReport{Src: catGRNs})
var _ = catWire("cat-purchase-itemwise-detail", &catReport{Src: catGRNLines})
var _ = catWire("cat-purchase-return-register", &catReport{Src: catPurchaseReturns})
var _ = catWire("cat-purchase-return-itemwise-detail", &catReport{Src: catPurchaseReturnLines})
var _ = catWire("cat-vendor-debit-note-register", &catReport{Src: catDebitNotes})
var _ = catWire("cat-vendor-credit-note-register", &catReport{Src: catVendorCreditNotes})
var _ = catWire("cat-grn-register", &catReport{Src: catGRNs})
var _ = catWire("cat-grn-itemwise-detail", &catReport{Src: catGRNLines})
var _ = catWire("cat-grn-po-invoice-variance", &catReport{Src: catGRNLines,
	Keep: func(r *catRow) bool {
		return (r.ln("invoice_rate") != 0 && r.ln("invoice_rate") != r.ln("po_rate")) || (r.ln("ordered_qty") != 0 && r.ln("ordered_qty") != r.ln("qty"))
	}})
var _ = catWire("cat-grn-cancelled-register", &catReport{Src: catGRNs, Keep: func(r *catRow) bool { return r.Status == "Cancelled" }})
var _ = catWire("cat-grn-barcode-generation-register", &catReport{Src: catBarcodes,
	Cols: map[string]catFn{
		"PO No":      func(r *catRow) interface{} { return barcodeCols["PO No"](r) },
		"Created By": func(r *catRow) interface{} { return firstNonEmpty(catStr(barcodeGRN(r)["created_by"]), r.CreatedBy) },
	}})
var _ = catWire("cat-grn-qc-rejection-exception", &catReport{Src: catGRNLines,
	Keep: func(r *catRow) bool { return r.ln("rejected_qty")+r.ln("damaged_qty") > 0 }})
var _ = catWire("cat-pending-grn-open-receipt", &catReport{Src: catPOLines,
	Keep: func(r *catRow) bool { return poOpen(r) && r.ln("qty")-r.poReceipt().accepted > 0 }})
var _ = catWire("cat-grn-vs-vendor-invoice-reconciliation", &catReport{Src: catGRNLines,
	Keep: func(r *catRow) bool { return r.hs("vendor_invoice_no") != "" }})
