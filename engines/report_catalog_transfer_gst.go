package engines

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Catalog reports - Stock Transfer and GST & Compliance (Stage 75.3.5,
// Wave A).
//
// A TransferOrder is the transfer out; its receipts are "Receipt" events in
// transfer_events (numbered 1, 2...), its per-SKU outcome receive_variance,
// and the dispatch is its first outgoing stock-ledger entry. The ERP has no
// e-way bills, so those columns stay blank and the E-Way Bill Register
// waits. GST lines come from the till bills (each line's net split at the
// item's rate) and the sales invoices (the invoice's own tax shared over its
// order lines by value, so the lines always add up to the invoice).

// --- transfers --------------------------------------------------------------

func transferEvents(r *catRow, kind string) []map[string]interface{} {
	var out []map[string]interface{}
	for _, e := range catLineList(r.H["transfer_events"]) {
		if kind == "" || catStr(e["kind"]) == kind {
			out = append(out, e)
		}
	}
	return out
}

// transferDispatch is when stock first left the source store for this
// transfer (stock ledger), once per run.
func transferDispatch(r *catRow) time.Time {
	m, ok := r.lk.extra["todispatch"].(map[string]time.Time)
	if !ok {
		m = map[string]time.Time{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'voucher_id', MIN(created_at) FROM %s.documents
			WHERE doctype = 'StockLedgerEntry' AND deleted_at IS NULL AND data->>'voucher_type' = 'TransferOrder' AND data->>'qty' LIKE '-%%'
			GROUP BY 1`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var id string
				var at time.Time
				if rows.Scan(&id, &at) == nil {
					m[id] = at
				}
			}
			rows.Close()
		}
		r.lk.extra["todispatch"] = m
	}
	return m[r.ID]
}

func transferQty(r *catRow, field string) float64 {
	total := 0.0
	for _, l := range catLineList(r.H[field]) {
		total += catNum(l["qty"])
	}
	return total
}

// transferOutstanding is dispatched less what arrived, was damaged or came back.
func transferOutstanding(r *catRow) float64 {
	sent := transferQty(r, "dispatched_items")
	if sent == 0 {
		return 0
	}
	settled := 0.0
	for _, v := range catLineList(r.H["receive_variance"]) {
		settled += catNum(v["received_qty"]) + catNum(v["damaged"]) + catNum(v["returned"])
	}
	if settled == 0 {
		settled = transferQty(r, "received_items")
	}
	return mathMax0(sent - settled)
}

func transferInNo(r *catRow, e map[string]interface{}) string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s/R%v", r.number(), e["no"])
}

func transferFirstReceipt(r *catRow) map[string]interface{} {
	if ev := transferEvents(r, "Receipt"); len(ev) > 0 {
		return ev[0]
	}
	return nil
}

var transferCols = map[string]catFn{
	"Transfer Out Date": func(r *catRow) interface{} { return r.date() },
	"Transfer Out No":   func(r *catRow) interface{} { return r.number() },
	"Document Date":     func(r *catRow) interface{} { return r.date() },
	"Transfer Status":   func(r *catRow) interface{} { return r.Status },
	"Qty": func(r *catRow) interface{} {
		if r.L != nil {
			return r.ln("qty")
		}
		return firstPositive(transferQty(r, "dispatched_items"), transferQty(r, "items"))
	},
	"Dispatch Date": func(r *catRow) interface{} { return catDay(transferDispatch(r)) },
	"In-Transit Days": func(r *catRow) interface{} {
		at := transferDispatch(r)
		if at.IsZero() || transferOutstanding(r) == 0 {
			return nil
		}
		return math.Floor(r.since(at).Hours() / 24)
	},
	"In-Transit Qty": func(r *catRow) interface{} { return transferOutstanding(r) },
	"Transfer In No": func(r *catRow) interface{} { return transferInNo(r, transferFirstReceipt(r)) },
	"Transfer In Date": func(r *catRow) interface{} {
		return catDay(transferFirstReceipt(r)["at"])
	},
	"Receipt Date": func(r *catRow) interface{} {
		ev := transferEvents(r, "Receipt")
		if len(ev) == 0 {
			return nil
		}
		return catDay(ev[len(ev)-1]["at"])
	},
	"Taxable Amount": catHN("total_amount"),
}

var catTransfers = &catSource{Label: "Transfer Out", Doctype: "TransferOrder", Number: "transfer_number", StoreField: "from_warehouse",
	FromStore: "from_warehouse", ToStore: "to_warehouse",
	Statuses: []string{"Draft", "Pending Approval", "Approved", "Dispatched", "In Transit", "Partially Received", "Received", "Closed", "Cancelled"},
	Cols:     transferCols}

var catTransferLines = &catSource{Label: "Transfer Out", Doctype: "TransferOrder", Lines: "items", Number: "transfer_number", StoreField: "from_warehouse",
	FromStore: "from_warehouse", ToStore: "to_warehouse", SKU: "sku", Qty: "qty",
	Statuses: []string{"Draft", "Pending Approval", "Approved", "Dispatched", "In Transit", "Partially Received", "Received", "Closed", "Cancelled"},
	Cols:     transferCols}

// transferReceiptRows: one row per receipt (register) or per receipt line
// (itemwise), the transfer in. The Store filter is the receiving store.
func transferReceiptRows(lines bool) func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	return func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
		q := catSource{Doctype: "TransferOrder", StoreField: "to_warehouse", Where: "data->>'transfer_events' LIKE '%Receipt%'"}
		docs, err := q.docRows(schema, map[string]string{"location": p["location"]}, true)
		if err != nil {
			return nil, err
		}
		var out []*catRow
		for _, d := range docs {
			for _, e := range catLineList(d.H["transfer_events"]) {
				if catStr(e["kind"]) != "Receipt" {
					continue
				}
				day := catDay(e["at"])
				if (p["from"] != "" && day < p["from"]) || (p["to"] != "" && day > p["to"]) {
					continue
				}
				evLines := catLineList(e["lines"])
				qty := 0.0
				for _, l := range evLines {
					qty += catNum(l["received"])
				}
				base := catRow{ID: d.ID, Status: d.Status, Created: d.Created, CreatedBy: firstNonEmpty(catStr(e["by"]), d.CreatedBy), H: d.H}
				if !lines {
					r := base
					r.L = map[string]interface{}{"event": e, "qty": qty, "at": e["at"], "no": e["no"]}
					out = append(out, &r)
					continue
				}
				for i, l := range evLines {
					r := base
					r.N = i + 1
					r.L = map[string]interface{}{"event": e, "sku": l["sku"], "qty": catNum(l["received"]), "damaged": l["damaged"],
						"reason": firstNonEmpty(catStr(l["reason_code"]), catStr(l["reason"])), "at": e["at"], "no": e["no"]}
					out = append(out, &r)
				}
			}
		}
		return out, nil
	}
}

var transferInCols = mergeCols(transferCols, map[string]catFn{
	"Transfer In Date": func(r *catRow) interface{} { return catDay(r.lv("at")) },
	"Transfer In No":   func(r *catRow) interface{} { return fmt.Sprintf("%s/R%v", r.number(), r.lv("no")) },
	"Receipt Date":     func(r *catRow) interface{} { return catDay(r.lv("at")) },
	"Qty":              catLN("qty"),
	"Reason":           catL("reason"),
	"Remarks": func(r *catRow) interface{} {
		if d := r.ln("damaged"); d > 0 {
			return fmt.Sprintf("%v damaged on arrival", d)
		}
		return nil
	},
})

var catTransferIns = &catSource{Label: "Transfer In", Rows: transferReceiptRows(false), Number: "transfer_number", StoreField: "to_warehouse",
	FromStore: "from_warehouse", ToStore: "to_warehouse", Cols: transferInCols}
var catTransferInLines = &catSource{Label: "Transfer In", Rows: transferReceiptRows(true), Number: "transfer_number", StoreField: "to_warehouse",
	FromStore: "from_warehouse", ToStore: "to_warehouse", SKU: "sku", Qty: "qty", Cols: transferInCols}

// Shortage / excess per SKU from the transfer's receive variance.
var catTransferVariance = &catSource{Label: "Transfer", Doctype: "TransferOrder", Lines: "receive_variance", Number: "transfer_number",
	StoreField: "to_warehouse", FromStore: "from_warehouse", ToStore: "to_warehouse", SKU: "sku", Qty: "dispatched_qty",
	Cols: mergeCols(transferCols, map[string]catFn{
		"Qty":            catLN("dispatched_qty"),
		"Received Qty":   catLN("received_qty"),
		"Difference Qty": func(r *catRow) interface{} { return r.ln("received_qty") - r.ln("dispatched_qty") },
		"Reason": func(r *catRow) interface{} {
			parts := []string{}
			for _, k := range []string{"shortfall", "damaged", "returned", "excess"} {
				if v := r.ln(k); v != 0 {
					parts = append(parts, fmt.Sprintf("%s %v", k, v))
				}
			}
			return strings.Join(parts, ", ")
		},
		"Remarks": catH("cancel_note"),
		"Status":  func(r *catRow) interface{} { return r.Status },
	})}

var catTransferAudit = &catSource{Label: "Transfer", Rows: auditChangeRows([]string{"TransferOrder"}, []string{"status"}), StoreOf: func(r *catRow) string {
	return catStr(r.master("TransferOrder", r.hs("doc_id"))["from_warehouse"])
},
	Cols: map[string]catFn{
		"Transfer Out No": func(r *catRow) interface{} {
			return firstNonEmpty(catStr(r.master("TransferOrder", r.hs("doc_id"))["transfer_number"]), r.hs("doc_id"))
		},
		"Document Date": func(r *catRow) interface{} { return catDay(r.Created) },
		"From Store": func(r *catRow) interface{} {
			return r.nameOf("Location", catStr(r.master("TransferOrder", r.hs("doc_id"))["from_warehouse"]))
		},
		"To Store": func(r *catRow) interface{} {
			return r.nameOf("Location", catStr(r.master("TransferOrder", r.hs("doc_id"))["to_warehouse"]))
		},
		"Edit Action": func(r *catRow) interface{} {
			if r.hs("old") == "" {
				return "Status set to " + r.hs("new")
			}
			return r.hs("old") + " → " + r.hs("new")
		},
		"Reason": func(r *catRow) interface{} {
			if r.hs("new") != "Cancelled" {
				return nil
			}
			to := r.master("TransferOrder", r.hs("doc_id"))
			return firstNonEmpty(catStr(to["cancel_reason_code"]), catStr(to["cancel_note"]))
		},
		"Approved By": func(r *catRow) interface{} {
			if r.hs("new") == "Approved" {
				return firstNonEmpty(catStr(r.master("TransferOrder", r.hs("doc_id"))["approved_by"]), r.CreatedBy)
			}
			return nil
		},
		"Approved At": func(r *catRow) interface{} {
			if r.hs("new") == "Approved" {
				return catStamp(r.Created)
			}
			return nil
		},
		"Status": catH("new"),
	}}

// --- GST lines ----------------------------------------------------------------

// gstLineRows: every outward GST line - till bill lines and sales invoice
// lines - with its document in H and the line in L ("_kind": pos | inv).
func gstLineRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	var out []*catRow
	bills, err := catPOSLines.docRows(schema, p, false)
	if err != nil {
		return nil, err
	}
	for _, b := range bills {
		b.L["_kind"] = "pos"
		out = append(out, b)
	}
	invoices, err := catSalesInvoices.docRows(schema, p, false)
	if err != nil {
		return nil, err
	}
	orderIDs := []string{}
	for _, inv := range invoices {
		if id := catStr(inv.H["sales_order_id"]); id != "" {
			orderIDs = append(orderIDs, id)
		}
	}
	linesByOrder := map[string][]map[string]interface{}{}
	if len(orderIDs) > 0 {
		rows, err := dbQuery(fmt.Sprintf(`SELECT data->>'order_id', data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL
			AND data->>'order_id' = ANY($1) AND COALESCE(data->>'line_status', '') NOT IN ('Cancelled')`, schema), pqStringArray(orderIDs))
		if err == nil {
			for rows.Next() {
				var order string
				var raw []byte
				if rows.Scan(&order, &raw) == nil {
					if l := catLineList("[" + string(raw) + "]"); len(l) == 1 {
						linesByOrder[order] = append(linesByOrder[order], l[0])
					}
				}
			}
			rows.Close()
		}
	}
	for _, inv := range invoices {
		lines := linesByOrder[catStr(inv.H["sales_order_id"])]
		total := 0.0
		for _, l := range lines {
			total += catNum(l["unit_price"]) * catNum(l["qty"])
		}
		if len(lines) == 0 || total == 0 { // no lines: the invoice as one line
			r := *inv
			r.L = map[string]interface{}{"_kind": "inv", "_share": 1.0}
			out = append(out, &r)
			continue
		}
		for i, l := range lines {
			r := *inv
			r.N = i + 1
			r.L = map[string]interface{}{"_kind": "inv", "sku": l["sku"], "qty": l["qty"], "_share": catNum(l["unit_price"]) * catNum(l["qty"]) / total}
			out = append(out, &r)
		}
	}
	return out, nil
}

// gstMoney is one GST line's taxable value, rate and tax split.
type gstMoney struct{ taxable, rate, cgst, sgst, igst float64 }

func (g gstMoney) tax() float64 { return g.cgst + g.sgst + g.igst }

func gstLine(r *catRow) gstMoney {
	if r.ls("_kind") == "inv" {
		s := r.ln("_share")
		g := gstMoney{taxable: r.hn("taxable_amount") * s, cgst: r.hn("cgst") * s, sgst: r.hn("sgst") * s, igst: r.hn("igst") * s}
		if g.taxable > 0 {
			g.rate = math.Round(g.tax() / g.taxable * 100)
		} else {
			g.rate = catNum(r.item()["gst_rate"])
		}
		return g
	}
	m := posLine(r)
	g := gstMoney{taxable: m.taxable, rate: m.rate}
	if posBillGST(r, "igst") > 0 {
		g.igst = m.gst
	} else {
		g.cgst, g.sgst = m.gst/2, m.gst/2
	}
	return g
}

func gstCustomerGSTIN(r *catRow) string {
	if r.ls("_kind") == "inv" {
		if o := r.master("SalesOrder", r.hs("sales_order_id")); o != nil {
			if g := catStr(o["customer_gstin"]); g != "" {
				return g
			}
			return catStr(r.master("Customer", catStr(o["customer_id"]))["gstin"])
		}
		return ""
	}
	return catStr(r.master("Customer", r.hs("customer_id"))["gstin"])
}

func gstPlaceOfSupply(r *catRow) string {
	if r.ls("_kind") == "inv" {
		if s := catStr(catSalesInvoices.Cols["Place of Supply"](r)); s != "" {
			return s
		}
	}
	return catStr(r.store()["state"])
}

var gstCols = map[string]catFn{
	"Document Date": func(r *catRow) interface{} { return r.date() },
	"Document Type": func(r *catRow) interface{} { return "Invoice" },
	"Document No": func(r *catRow) interface{} {
		if r.ls("_kind") == "inv" {
			return firstNonEmpty(r.hs("invoice_number"), r.ID)
		}
		return posBillNo(r)
	},
	"Customer GSTIN":  func(r *catRow) interface{} { return gstCustomerGSTIN(r) },
	"Place of Supply": func(r *catRow) interface{} { return gstPlaceOfSupply(r) },
	"Supply Type": func(r *catRow) interface{} {
		if gstCustomerGSTIN(r) != "" {
			return "B2B"
		}
		return "B2C"
	},
	"Tax Type": func(r *catRow) interface{} {
		if gstLine(r).igst > 0 {
			return "IGST"
		}
		return "CGST + SGST"
	},
	"GST %":          func(r *catRow) interface{} { return gstLine(r).rate },
	"Qty":            catLN("qty"),
	"Taxable Amount": func(r *catRow) interface{} { return gstLine(r).taxable },
	"CGST Amount":    func(r *catRow) interface{} { return gstLine(r).cgst },
	"SGST Amount":    func(r *catRow) interface{} { return gstLine(r).sgst },
	"IGST Amount":    func(r *catRow) interface{} { return gstLine(r).igst },
	"GST Amount":     func(r *catRow) interface{} { return gstLine(r).tax() },
	"Net Amount":     func(r *catRow) interface{} { g := gstLine(r); return g.taxable + g.tax() },
}

var catGSTLines = &catSource{Label: "GST line", Rows: gstLineRows, SKU: "sku", Qty: "qty", Customer: "customer_id", Cols: gstCols}

// --- e-invoices (irp_documents, Stage 75.2) -------------------------------------

func irpRows(where string) func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	return func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
		q := fmt.Sprintf(`SELECT d.id::text, d.doc_type, d.doc_number, to_char(d.doc_date, 'YYYY-MM-DD'), d.gstin, d.source_doctype, d.source_id, d.state,
			COALESCE(d.irn, ''), COALESCE(d.ack_no, ''), COALESCE(to_char(d.ack_date, 'YYYY-MM-DD HH24:MI'), ''), COALESCE(d.last_error, ''), d.attempts,
			d.updated_at, d.created_at, COALESCE((SELECT to_char(MAX(a.at), 'YYYY-MM-DD HH24:MI') FROM %[1]s.irp_attempts a WHERE a.document_id = d.id), '')
			FROM %[1]s.irp_documents d WHERE d.environment = $1 AND %[2]s`, schema, where)
		args := []interface{}{IRPEnvironment()}
		if v := p["from"]; v != "" {
			args = append(args, v)
			q += fmt.Sprintf(" AND d.doc_date >= $%d::date", len(args))
		}
		if v := p["to"]; v != "" {
			args = append(args, v)
			q += fmt.Sprintf(" AND d.doc_date <= $%d::date", len(args))
		}
		rows, err := dbQuery(q+" ORDER BY d.doc_date DESC, d.id DESC LIMIT 20000", args...)
		if err != nil {
			return nil, nil // no e-invoice tables on this client yet
		}
		defer rows.Close()
		var out []*catRow
		for rows.Next() {
			var id, docType, number, day, gstin, srcType, srcID, state, irn, ackNo, ackDate, lastErr, lastTry string
			var attempts int
			var updated, created time.Time
			if rows.Scan(&id, &docType, &number, &day, &gstin, &srcType, &srcID, &state, &irn, &ackNo, &ackDate, &lastErr, &attempts, &updated, &created, &lastTry) != nil {
				continue
			}
			out = append(out, &catRow{ID: id, Status: state, Created: created, H: map[string]interface{}{"doc_type": docType, "doc_number": number,
				"doc_date": day, "gstin": gstin, "source_doctype": srcType, "source_id": srcID, "irn": irn, "ack_no": ackNo, "ack_date": ackDate,
				"last_error": lastErr, "attempts": float64(attempts), "last_try": firstNonEmpty(lastTry, catStamp(updated))}})
		}
		return out, rows.Err()
	}
}

func irpSource(r *catRow) map[string]interface{} {
	return r.master(r.hs("source_doctype"), r.hs("source_id"))
}

func irpSourceStore(r *catRow) string {
	if d := irpSource(r); d != nil {
		if s := catStr(d["location"]); s != "" {
			return s
		}
		if c := r.master("POSCart", catStr(d["reference_cart"])); c != nil {
			return catStr(c["location"])
		}
	}
	return ""
}

var irpCols = map[string]catFn{
	"Document Date": catH("doc_date"),
	"Document Type": func(r *catRow) interface{} {
		return map[string]string{"INV": "Invoice", "CRN": "Credit Note", "DBN": "Debit Note"}[r.hs("doc_type")]
	},
	"Document No":   catH("doc_number"),
	"GSTIN":         catH("gstin"),
	"IRN":           catH("irn"),
	"Ack No":        catH("ack_no"),
	"Ack Date":      catH("ack_date"),
	"IRN Status":    func(r *catRow) interface{} { return r.Status },
	"IRN Error":     catH("last_error"),
	"Retry Count":   catHN("attempts"),
	"Last Retry At": catH("last_try"),
	"Status": func(r *catRow) interface{} {
		if d := irpSource(r); d != nil {
			return firstNonEmpty(catStr(d["status"]), r.Status)
		}
		return r.Status
	},
	"Customer GSTIN": func(r *catRow) interface{} {
		if d := irpSource(r); d != nil {
			if c := r.master("Customer", firstNonEmpty(catStr(d["customer_id"]), catStr(d["customer"]))); c != nil {
				return c["gstin"]
			}
		}
		return nil
	},
	"Place of Supply": func(r *catRow) interface{} { return r.master("Location", irpSourceStore(r))["state"] },
	"Taxable Amount": func(r *catRow) interface{} {
		d := irpSource(r)
		return firstPositive(catNum(d["taxable_amount"]), catNum(d["amount"]))
	},
	"CGST Amount": func(r *catRow) interface{} { return irpSource(r)["cgst"] },
	"SGST Amount": func(r *catRow) interface{} { return irpSource(r)["sgst"] },
	"IGST Amount": func(r *catRow) interface{} { return irpSource(r)["igst"] },
	"Net Amount": func(r *catRow) interface{} {
		d := irpSource(r)
		return firstPositive(catNum(d["total_amount"]), catNum(d["amount"]))
	},
	"User Name": func(r *catRow) interface{} { return irpSource(r)["created_by"] },
}

func irpSrc(where string) *catSource {
	return &catSource{Label: "E-invoice", Rows: irpRows(where), StoreOf: irpSourceStore, Cols: irpCols}
}

// --- the GST document series --------------------------------------------------

var catGSTDocs = &catSource{Label: "GST document", Rows: func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	var out []*catRow
	for _, s := range []*catSource{catPOSBills, catSalesInvoices, catCreditNotes} {
		rs, err := s.docRows(schema, p, false)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			r.L = map[string]interface{}{"_src": s.Label}
			out = append(out, r)
		}
	}
	return out, nil
},
	StoreOf: func(r *catRow) string {
		if r.ls("_src") == "Credit Note" {
			return creditNoteStore(r)
		}
		return r.hs("location")
	},
	Cols: map[string]catFn{
		"Document Type": func(r *catRow) interface{} {
			if r.ls("_src") == "Credit Note" {
				return "Credit Note"
			}
			return "Invoice (" + r.ls("_src") + ")"
		},
		"Document No": func(r *catRow) interface{} { return r.ID },
		"GST Bill No": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("bill_number"), r.hs("invoice_number"), r.hs("note_number"))
		},
		"Original Bill No": func(r *catRow) interface{} {
			if r.ls("_src") == "Credit Note" {
				return catCreditNotes.Cols["Original Bill No"](r)
			}
			return nil
		},
	}}

var _ = catWire("cat-transfer-out-register", &catReport{Src: catTransfers})
var _ = catWire("cat-transfer-out-itemwise-detail", &catReport{Src: catTransferLines})
var _ = catWire("cat-transfer-in-register", &catReport{Src: catTransferIns})
var _ = catWire("cat-transfer-in-itemwise-detail", &catReport{Src: catTransferInLines})
var _ = catWire("cat-transfer-in-transit-aging", &catReport{Src: catTransfers, Keep: func(r *catRow) bool { return transferOutstanding(r) > 0 }})
var _ = catWire("cat-transfer-barcode-traceability", &catReport{Src: catTransferLines})
var _ = catWire("cat-transfer-gst-e-way-control", &catReport{Src: catTransfers,
	Keep: func(r *catRow) bool {
		from, to := r.master("Location", r.hs("from_warehouse")), r.master("Location", r.hs("to_warehouse"))
		return catStr(from["legal_entity"]) != catStr(to["legal_entity"]) || catStr(from["state"]) != catStr(to["state"])
	},
	Cols: map[string]catFn{"Status": func(r *catRow) interface{} { return r.Status }}})
var _ = catWire("cat-transfer-shortage-excess-exception", &catReport{Src: catTransferVariance,
	Keep: func(r *catRow) bool {
		return r.ln("received_qty") != r.ln("dispatched_qty") || r.ln("damaged")+r.ln("returned") > 0
	}})
var _ = catWire("cat-transfer-approval-cancellation-audit", &catReport{Src: catTransferAudit})
var _ = catWire("cat-gstr-1-hsn-summary", &catReport{Src: catGSTLines, Group: []string{"From Date", "To Date", "GSTIN", "HSN Code", "GST %"}})
var _ = catWire("cat-gstr-1-b2b-b2c-summary", &catReport{Src: catGSTLines, Group: []string{"From Date", "To Date", "GSTIN", "Supply Type", "Place of Supply", "GST %"}})
var _ = catWire("cat-irn-portal-manual-upload-report", &catReport{Src: irpSrc("d.state IN ('MANUAL_REVIEW', 'MANUAL_IMPORT', 'BLOCKED_CONFIG', 'REJECTED')")})
var _ = catWire("cat-e-invoice-irn-status-register", &catReport{Src: irpSrc("d.state <> 'NOT_REQUIRED'")})
var _ = catWire("cat-e-invoice-error-retry-report", &catReport{Src: irpSrc("(d.last_error <> '' OR d.attempts > 1)")})
var _ = catWire("cat-gst-document-tax-detail", &catReport{Src: catGSTLines})
var _ = catWire("cat-hsn-tax-summary", &catReport{Src: catGSTLines, Group: []string{"From Date", "To Date", "HSN Code", "GST %"}})
var _ = catWire("cat-gst-bill-number-document-series-audit", &catReport{Src: catGSTDocs})
