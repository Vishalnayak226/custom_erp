package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Stage 49.5.5 (first slice): order -> shipment -> invoice -> payment. One
// report lists where the four disagree:
//
//   - Shipped not invoiced      shipped quantity on an order exceeds what its invoices bill
//   - Invoiced more than shipped invoices bill more than was shipped
//   - Paid more than invoiced   receipts allocated to an invoice exceed its total
//   - Invoice without order     an invoice that points at no order, shipment or bill
//
// An exception (SetReconciliationException) takes a mismatch off the list. It
// needs a note, may carry an attachment reference, and keeps the figures it
// was made against: when they change the dismissal lapses and the mismatch
// returns. Not in this slice: stock ownership, command-to-outbox checks and
// persisted money / UOM / tax versions.

const reconcileLimit = 5000

// OrderToCashRow is one mismatch.
type OrderToCashRow struct {
	Kind            string  `json:"kind"`
	Ref             string  `json:"ref"` // the order, or the invoice for invoice-level kinds
	Order           string  `json:"order"`
	Invoice         string  `json:"invoice"`
	Customer        string  `json:"customer"`
	ShippedQty      float64 `json:"shipped_qty"`
	InvoicedQty     float64 `json:"invoiced_qty"`
	InvoicedAmount  float64 `json:"invoiced_amount"`
	PaidAmount      float64 `json:"paid_amount"`
	Difference      float64 `json:"difference"`
	Owner           string  `json:"owner"`
	Hint            string  `json:"hint"`
	Exception       string  `json:"exception,omitempty"`
	ExceptionNote   string  `json:"exception_note,omitempty"`
	ExceptionBy     string  `json:"exception_by,omitempty"`
	ExceptionAt     string  `json:"exception_at,omitempty"`
	ExceptionAttach string  `json:"exception_attachment,omitempty"`
	fingerprint     string
}

// qtyOf sums the qty of a line list that is either a JSON array or a JSON
// string holding one.
func qtyOf(raw interface{}) float64 {
	var lines []map[string]interface{}
	switch v := raw.(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &lines)
	case []interface{}:
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, &lines)
	}
	total := 0.0
	for _, l := range lines {
		total += numOf(l["qty"])
	}
	return total
}

func numOf(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		var f float64
		fmt.Sscanf(strings.TrimSpace(n), "%f", &f)
		return f
	}
	return 0
}

func strOf(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

type cashDoc struct {
	id     string
	status string
	data   map[string]interface{}
}

func loadCashDocs(schema, doctype string, statuses ...string) ([]cashDoc, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, status, data::text FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND status = ANY($2) ORDER BY id LIMIT %d`, schema, reconcileLimit),
		doctype, "{"+strings.Join(statuses, ",")+"}")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []cashDoc
	for rows.Next() {
		var d cashDoc
		var raw string
		if err := rows.Scan(&d.id, &d.status, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &d.data)
		out = append(out, d)
	}
	return out, rows.Err()
}

// OrderToCashMismatches lists the mismatches. Dismissed ones whose figures
// have not changed are left out unless includeAccepted.
func OrderToCashMismatches(tenantID string, includeAccepted bool) ([]OrderToCashRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// order existence ignores status: any live SalesOrder counts
	orderSeen := map[string]bool{}
	orderCustomer := map[string]string{}
	ids, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'customer','') FROM %s.documents WHERE doctype = 'SalesOrder' AND deleted_at IS NULL LIMIT %d`, schema, reconcileLimit*2))
	if err != nil {
		return nil, err
	}
	for ids.Next() {
		var id, c string
		if err := ids.Scan(&id, &c); err == nil {
			orderSeen[id], orderCustomer[id] = true, c
		}
	}
	ids.Close()

	pkgs, err := loadCashDocs(schema, "ShippingPackage", "Shipped")
	if err != nil {
		return nil, err
	}
	invs, err := loadCashDocs(schema, "SalesInvoice", "Approved", "Paid")
	if err != nil {
		return nil, err
	}
	recs, err := loadCashDocs(schema, "CustomerReceipt", "Posted")
	if err != nil {
		return nil, err
	}

	pkgOrder := map[string]string{}
	shipped := map[string]float64{}
	for _, p := range pkgs {
		o := strOf(p.data["order_id"])
		pkgOrder[p.id] = o
		if o != "" {
			shipped[o] += qtyOf(p.data["items"])
		}
	}
	paidOn := map[string]float64{}
	for _, r := range recs {
		if list, ok := r.data["invoices"].([]interface{}); ok {
			for _, it := range list {
				if m, ok := it.(map[string]interface{}); ok {
					paidOn[strOf(m["ref"])] += numOf(m["amount"])
				}
			}
		}
	}

	invoicedQty := map[string]float64{}
	invoicedAmt := map[string]float64{}
	orderInvoice := map[string]string{}
	orderCust := map[string]string{}
	var rows []OrderToCashRow
	for _, inv := range invs {
		order := strOf(inv.data["sales_order_id"])
		if order == "" {
			order = pkgOrder[strOf(inv.data["shipping_package_id"])]
		}
		cust := strOf(inv.data["customer"])
		if cust == "" {
			cust = strOf(inv.data["customer_id"])
		}
		total := numOf(inv.data["total_amount"])
		if order != "" {
			invoicedQty[order] += qtyOf(inv.data["items"])
			invoicedAmt[order] += total
			if orderInvoice[order] == "" {
				orderInvoice[order] = inv.id
			} else {
				orderInvoice[order] += ", " + inv.id
			}
			orderCust[order] = cust
		}
		linked := order != "" || strOf(inv.data["oms_bill_id"]) != "" || strOf(inv.data["delivery_challan"]) != "" || strOf(inv.data["recurring_contract_id"]) != "" || strOf(inv.data["shipping_package_id"]) != ""
		if !linked || (order != "" && !orderSeen[order] && strOf(inv.data["oms_bill_id"]) == "") {
			rows = append(rows, OrderToCashRow{Kind: "Invoice without order", Ref: inv.id, Order: order, Invoice: inv.id, Customer: cust,
				InvoicedQty: qtyOf(inv.data["items"]), InvoicedAmount: total, PaidAmount: paidOn[inv.id], Difference: total,
				Owner: "Sales", Hint: "Link the invoice to its sales order, or accept it with a note if it is a one-off sale."})
		}
		if paid := paidOn[inv.id]; paid > total+0.005 {
			rows = append(rows, OrderToCashRow{Kind: "Paid more than invoiced", Ref: inv.id, Order: order, Invoice: inv.id, Customer: cust,
				InvoicedAmount: total, PaidAmount: paid, Difference: round2(paid - total),
				Owner: "Accounts", Hint: "Move the excess to another invoice or refund it."})
		}
	}
	for order, ship := range shipped {
		inv := invoicedQty[order]
		cust := orderCust[order]
		if cust == "" {
			cust = orderCustomer[order]
		}
		switch {
		case ship > inv+0.0005:
			rows = append(rows, OrderToCashRow{Kind: "Shipped not invoiced", Ref: order, Order: order, Invoice: orderInvoice[order], Customer: cust,
				ShippedQty: ship, InvoicedQty: inv, InvoicedAmount: invoicedAmt[order], Difference: round3(ship - inv),
				Owner: "Sales", Hint: "Raise an invoice for the shipped quantity."})
		case inv > ship+0.0005:
			rows = append(rows, OrderToCashRow{Kind: "Invoiced more than shipped", Ref: order, Order: order, Invoice: orderInvoice[order], Customer: cust,
				ShippedQty: ship, InvoicedQty: inv, InvoicedAmount: invoicedAmt[order], Difference: round3(inv - ship),
				Owner: "Sales", Hint: "Ship the balance, or credit the excess."})
		}
	}
	// an invoiced order with nothing shipped at all
	for order, inv := range invoicedQty {
		if _, ok := shipped[order]; ok || inv <= 0.0005 || !orderSeen[order] {
			continue
		}
		cust := orderCust[order]
		rows = append(rows, OrderToCashRow{Kind: "Invoiced more than shipped", Ref: order, Order: order, Invoice: orderInvoice[order], Customer: cust,
			InvoicedQty: inv, InvoicedAmount: invoicedAmt[order], Difference: round3(inv),
			Owner: "Sales", Hint: "Ship the balance, or credit the excess."})
	}
	for i := range rows {
		rows[i].fingerprint = fmt.Sprintf("%.3f|%.3f|%.2f|%.2f", rows[i].ShippedQty, rows[i].InvoicedQty, rows[i].InvoicedAmount, rows[i].PaidAmount)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Ref < rows[j].Ref
	})

	// exceptions: the latest row per key
	type exc struct{ status, fp, note, by, at, att string }
	latest := map[string]exc{}
	er, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT ON (mismatch_kind, ref_key) mismatch_kind, ref_key, status, fingerprint, note, changed_by, to_char(changed_at,'YYYY-MM-DD HH24:MI'), attachment
		FROM %s.reconciliation_exceptions ORDER BY mismatch_kind, ref_key, id DESC`, schema))
	if err != nil {
		return nil, err
	}
	for er.Next() {
		var k, r string
		var e exc
		if err := er.Scan(&k, &r, &e.status, &e.fp, &e.note, &e.by, &e.at, &e.att); err == nil {
			latest[k+"\x00"+r] = e
		}
	}
	er.Close()
	out := rows[:0]
	for _, r := range rows {
		if e, ok := latest[r.Kind+"\x00"+r.Ref]; ok && e.status == "Dismissed" && e.fp == r.fingerprint {
			if !includeAccepted {
				continue
			}
			r.Exception, r.ExceptionNote, r.ExceptionBy, r.ExceptionAt, r.ExceptionAttach = "Dismissed", e.note, e.by, e.at, e.att
		}
		out = append(out, r)
	}
	return out, nil
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

// SetReconciliationException dismisses (status "Dismissed") or reopens
// ("Open") a mismatch. Both need a note. A dismissal is only accepted for a
// mismatch that is on the list now, and keeps its current figures.
func SetReconciliationException(tenantID, kind, ref, status, note, attachment, actor string) error {
	note, attachment = strings.TrimSpace(note), strings.TrimSpace(attachment)
	if status != "Dismissed" && status != "Open" {
		return &ValidationError{Code: "GLOBAL-0002", Message: "status must be Dismissed or Open"}
	}
	if note == "" {
		return &ValidationError{Code: "GLOBAL-0002", Message: "a note is required to dismiss or reopen a mismatch"}
	}
	if len(note) > 2000 || len(attachment) > 500 {
		return &ValidationError{Code: "GLOBAL-0002", Message: "the note or attachment is too long"}
	}
	rows, err := OrderToCashMismatches(tenantID, true)
	if err != nil {
		return err
	}
	fp, found := "", false
	for _, r := range rows {
		if r.Kind == kind && r.Ref == ref {
			fp, found = r.fingerprint, true
			break
		}
	}
	if !found {
		return &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("no %q mismatch for %s on the list", kind, ref)}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.reconciliation_exceptions (mismatch_kind, ref_key, status, fingerprint, note, attachment, changed_by) VALUES ($1,$2,$3,$4,$5,$6,$7)`, schema),
		kind, ref, status, fp, note, attachment, actor); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "RECONCILIATION_"+strings.ToUpper(status), "SUCCESS", fmt.Sprintf("%s %s: %s", kind, ref, note))
	return nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "order-to-cash", Label: "Order to cash mismatches", Category: "Finance", StoreKey: "-",
		Columns: []ReportColumn{
			{Key: "kind", Label: "Mismatch"},
			{Key: "order", Label: "Order"},
			{Key: "invoice", Label: "Invoice"},
			{Key: "customer", Label: "Customer"},
			{Key: "shipped_qty", Label: "Shipped qty"},
			{Key: "invoiced_qty", Label: "Invoiced qty"},
			{Key: "invoiced_amount", Label: "Invoiced", Sensitive: true},
			{Key: "paid_amount", Label: "Paid", Sensitive: true},
			{Key: "difference", Label: "Difference"},
			{Key: "owner", Label: "Owner"},
			{Key: "hint", Label: "What it suggests"},
			{Key: "exception", Label: "Exception"},
			{Key: "exception_note", Label: "Note"},
		},
		Params: []ReportParam{
			{Key: "all", Label: "Include dismissed", Type: "select", Options: "No,Yes"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := OrderToCashMismatches(tenantID, params["all"] == "Yes")
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
