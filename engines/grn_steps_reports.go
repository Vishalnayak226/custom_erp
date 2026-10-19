package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 94.9: the goods receipt reports. Every figure comes from grn_line /
// grn_line_change (94.1) or the GRN document - "all should be saved in a table
// to extract in a report".
func init() {
	dates := []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}}
	vendor := ReportParam{Key: "vendor", Label: "Vendor (blank = all)", Type: "text"}
	RegisterReport(ReportDefinition{
		ID: "grn-line-register", Label: "GRN Register - every line, every step", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "grn", Label: "GRN"}, {Key: "child", Label: "Child"}, {Key: "location", Label: "Store"}, {Key: "vendor", Label: "Vendor"},
			{Key: "vendor_invoice_no", Label: "Invoice No"}, {Key: "po_id", Label: "PO"}, {Key: "sku", Label: "SKU"},
			{Key: "po_qty", Label: "PO Qty"}, {Key: "received_qty", Label: "Received"}, {Key: "invoice_qty", Label: "Invoice Qty"},
			{Key: "grn_qty", Label: "GRN Qty"}, {Key: "rejected_qty", Label: "Rejected"}, {Key: "damaged_qty", Label: "Damaged"},
			{Key: "po_rate", Label: "PO Price", Sensitive: true}, {Key: "invoice_price", Label: "Invoice Price", Sensitive: true},
			{Key: "grn_price", Label: "GRN Price", Sensitive: true}, {Key: "gst_rate", Label: "GST %"},
			{Key: "old_mrp", Label: "Old MRP"}, {Key: "new_mrp", Label: "MRP"}, {Key: "batch_no", Label: "Batch"},
			{Key: "step", Label: "Step"}, {Key: "received_at", Label: "Received"}, {Key: "posted_at", Label: "Posted"},
		},
		Params:   append(append([]ReportParam{}, dates...), vendor),
		StoreKey: "location",
		Run:      grnLineRegisterReport,
	})
	RegisterReport(ReportDefinition{
		ID: "grn-invoice-differences", Label: "Invoice vs GRN Differences", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "month", Label: "Month"}, {Key: "vendor", Label: "Vendor"}, {Key: "receipts", Label: "Receipts"},
			{Key: "invoice_total", Label: "Invoiced", Sensitive: true}, {Key: "our_total", Label: "Received and verified", Sensitive: true},
			{Key: "difference", Label: "Difference", Sensitive: true}, {Key: "debit_notes", Label: "Debit notes", Sensitive: true},
			{Key: "credit_notes", Label: "Credit notes", Sensitive: true}, {Key: "note_numbers", Label: "Notes"},
		},
		Params:   append(append([]ReportParam{}, dates...), vendor),
		StoreKey: "-",
		Run:      grnInvoiceDifferencesReport,
	})
	RegisterReport(ReportDefinition{
		ID: "grn-price-changes", Label: "Purchase Price Changes", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "posted_at", Label: "Received"}, {Key: "sku", Label: "SKU"}, {Key: "vendor", Label: "Vendor"}, {Key: "grn", Label: "GRN"},
			{Key: "prev_price", Label: "Previous Price", Sensitive: true}, {Key: "prev_vendor", Label: "Previous Vendor"},
			{Key: "grn_price", Label: "New Price", Sensitive: true}, {Key: "change_pct", Label: "Change %"},
			{Key: "old_mrp", Label: "Old MRP"}, {Key: "new_mrp", Label: "New MRP"},
		},
		Params:   append(append([]ReportParam{}, dates...), vendor),
		StoreKey: "-",
		Run:      grnPriceChangesReport,
	})
	RegisterReport(ReportDefinition{
		ID: "grn-waiting", Label: "Goods Receipts Waiting", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "grn", Label: "GRN"}, {Key: "vendor", Label: "Vendor"}, {Key: "vendor_invoice_no", Label: "Invoice No"},
			{Key: "waiting_for", Label: "Waiting for"}, {Key: "age_days", Label: "Age (days)"},
			{Key: "invoice_total", Label: "Invoice Total", Sensitive: true}, {Key: "excess_pending", Label: "Excess awaiting approval"},
		},
		StoreKey: "-",
		Run:      grnWaitingReport,
	})
	RegisterReport(ReportDefinition{
		ID: "grn-corrections", Label: "GRN Corrections Log", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "changed_at", Label: "When"}, {Key: "grn", Label: "GRN"}, {Key: "line_no", Label: "Line"}, {Key: "sku", Label: "SKU"},
			{Key: "step", Label: "Step"}, {Key: "field", Label: "Field"}, {Key: "old_value", Label: "Was"}, {Key: "new_value", Label: "Now"},
			{Key: "reason", Label: "Why"}, {Key: "changed_by", Label: "By"},
		},
		Params:   dates,
		StoreKey: "-",
		Run:      grnCorrectionsReport,
	})
	RegisterReport(ReportDefinition{
		ID: "grn-po-variance", Label: "PO vs GRN Price Variance", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "posted_at", Label: "Received"}, {Key: "vendor", Label: "Vendor"}, {Key: "po_id", Label: "PO"}, {Key: "grn", Label: "GRN"},
			{Key: "sku", Label: "SKU"}, {Key: "qty", Label: "Qty"}, {Key: "po_rate", Label: "PO Price", Sensitive: true},
			{Key: "grn_price", Label: "GRN Price", Sensitive: true}, {Key: "variance", Label: "Variance / unit", Sensitive: true},
			{Key: "variance_pct", Label: "Variance %"}, {Key: "variance_value", Label: "Variance value", Sensitive: true},
		},
		Params:   append(append([]ReportParam{}, dates...), vendor),
		StoreKey: "-",
		Run:      grnPOVarianceReport,
	})
	RegisterReport(ReportDefinition{
		ID: "grn-payments", Label: "GRN Payments", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "grn", Label: "GRN"}, {Key: "vendor", Label: "Vendor"}, {Key: "vendor_invoice_no", Label: "Invoice No"},
			{Key: "invoice_total", Label: "Invoiced", Sensitive: true}, {Key: "our_total", Label: "We owe", Sensitive: true},
			{Key: "paid", Label: "Paid", Sensitive: true}, {Key: "due", Label: "Due", Sensitive: true},
			{Key: "payment_status", Label: "Status"}, {Key: "last_paid", Label: "Last paid"}, {Key: "last_mode", Label: "Paid by"},
		},
		Params:   []ReportParam{vendor, {Key: "due_only", Label: "Due only", Type: "select", Options: ",Yes"}},
		StoreKey: "-",
		Run:      grnPaymentsReport,
	})
	RegisterReport(ReportDefinition{
		ID: "grn-in-receiving", Label: "Goods In Receiving", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "location", Label: "Store"}, {Key: "sku", Label: "SKU"}, {Key: "qty", Label: "In receiving"},
			{Key: "receipts", Label: "Receipts"}, {Key: "oldest", Label: "Received since"},
		},
		Params: []ReportParam{{Key: "store", Label: "Store code (blank = all)", Type: "text"}},
		Run:    grnInReceivingReport,
	})
	for id, q := range map[string]reportQuestion{
		"grn-in-receiving":        {"What has arrived but is not in stock yet?", "Goods on receipts still being verified, priced or posted - not sellable until their child GRN posts."},
		"grn-line-register":       {"What did each receipt line go through?", "Every GRN line with the PO, invoice and verified figures, MRP and who did each step."},
		"grn-invoice-differences": {"Where do vendor invoices differ from what we received?", "Invoiced against received-and-verified, by vendor and month, with the notes raised."},
		"grn-price-changes":       {"Which purchase prices changed?", "Items received at a price different from the last purchase, with the old and new MRP."},
		"grn-waiting":             {"Which goods receipts are waiting, and for what?", "Receipts not yet verified, priced or posted, oldest first."},
		"grn-corrections":         {"What was changed on a goods receipt?", "Every change at every step: was, now, who, when and why."},
		"grn-po-variance":         {"Are we paying what the PO agreed?", "Received price against the PO price per line."},
		"grn-payments":            {"What do we still owe on goods received?", "Each posted receipt with what we owe, what is paid and what is due."},
	} {
		reportQuestions[id] = q
	}
}

// grnDateFilter is "AND <col> between from and to" for the optional dates.
func grnDateFilter(col string, params map[string]string, args *[]interface{}) string {
	var out string
	if f := strings.TrimSpace(params["from"]); f != "" {
		*args = append(*args, f)
		out += fmt.Sprintf(" AND %s >= $%d::date", col, len(*args))
	}
	if t := strings.TrimSpace(params["to"]); t != "" {
		*args = append(*args, t)
		out += fmt.Sprintf(" AND %s < $%d::date + 1", col, len(*args))
	}
	return out
}

func grnVendorFilter(col string, params map[string]string, args *[]interface{}) string {
	if v := strings.TrimSpace(params["vendor"]); v != "" {
		*args = append(*args, v)
		return fmt.Sprintf(" AND %s = $%d", col, len(*args))
	}
	return ""
}

func nullFloat(v sql.NullFloat64) interface{} {
	if v.Valid {
		return round2(v.Float64)
	}
	return nil
}

func nullTimeText(v sql.NullTime) interface{} {
	if v.Valid {
		return v.Time.Format("2006-01-02 15:04")
	}
	return nil
}

func grnLineRegisterReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	if !grnLineTablesReady(db.DB, schema) {
		return out, nil
	}
	var args []interface{}
	where := grnDateFilter("COALESCE(posted_at, received_at, updated_at)", params, &args) + grnVendorFilter("vendor", params, &args)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT grn_code, COALESCE(child_key, ''), COALESCE(location, ''), COALESCE(vendor, ''),
		COALESCE(vendor_invoice_no, ''), COALESCE(po_id, ''), sku, po_qty, received_qty, invoice_qty, grn_qty, rejected_qty, damaged_qty,
		po_rate, invoice_price, grn_price, gst_rate, old_mrp, new_mrp, COALESCE(batch_no, ''), COALESCE(step, ''), received_at, posted_at
		FROM %s.grn_line WHERE TRUE %s ORDER BY COALESCE(posted_at, received_at, updated_at) DESC, grn_id, line_no LIMIT 5000`, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var grn, child, loc, vend, inv, po, sku, batch, step string
		var poQty, rec, iq, gq, rej, dmg, poRate, ip, gp, gst, oldMRP, newMRP sql.NullFloat64
		var recAt, postAt sql.NullTime
		if err := rows.Scan(&grn, &child, &loc, &vend, &inv, &po, &sku, &poQty, &rec, &iq, &gq, &rej, &dmg, &poRate, &ip, &gp, &gst, &oldMRP, &newMRP, &batch, &step, &recAt, &postAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"grn": grn, "child": child, "location": loc, "vendor": vend, "vendor_invoice_no": inv, "po_id": po, "sku": sku,
			"po_qty": nullFloat(poQty), "received_qty": nullFloat(rec), "invoice_qty": nullFloat(iq), "grn_qty": nullFloat(gq),
			"rejected_qty": nullFloat(rej), "damaged_qty": nullFloat(dmg), "po_rate": nullFloat(poRate), "invoice_price": nullFloat(ip),
			"grn_price": nullFloat(gp), "gst_rate": nullFloat(gst), "old_mrp": nullFloat(oldMRP), "new_mrp": nullFloat(newMRP),
			"batch_no": batch, "step": step, "received_at": nullTimeText(recAt), "posted_at": nullTimeText(postAt),
		})
	}
	return out, rows.Err()
}

// stepGRNDocs reads the step GRNs a report needs, newest first.
func stepGRNDocs(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var args []interface{}
	where := grnDateFilter("created_at", params, &args) + grnVendorFilter("data->>'vendor'", params, &args)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, COALESCE(status, ''), created_at FROM %s.documents
		WHERE doctype = 'GRN' AND deleted_at IS NULL AND data->>'flow' = 'steps' AND status <> 'Cancelled' %s
		ORDER BY created_at DESC LIMIT 5000`, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, dataStr, status string
		var at time.Time
		if err := rows.Scan(&id, &dataStr, &status, &at); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(dataStr), &data) != nil {
			continue
		}
		data["_id"], data["_status"], data["_created"] = id, status, at
		grnStampTotals(data, grnLineMaps(data))
		out = append(out, data)
	}
	return out, rows.Err()
}

func grnInvoiceDifferencesReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	docs, err := stepGRNDocs(tenantID, params)
	if err != nil {
		return nil, err
	}
	type agg struct {
		month, vendor            string
		n                        int
		inv, ours, diff, dn, vcn float64
		notes                    []string
	}
	groups := map[string]*agg{}
	for _, d := range docs {
		if d["_status"] != "Approved" {
			continue // only posted receipts have a settled difference
		}
		month := d["_created"].(time.Time).Format("2006-01")
		key := month + "|" + strField(d, "vendor")
		a := groups[key]
		if a == nil {
			a = &agg{month: month, vendor: strField(d, "vendor")}
			groups[key] = a
		}
		a.n++
		a.inv += numFromInterface(d["vendor_invoice_amount"])
		a.ours += numFromInterface(d["our_total"])
		diff := numFromInterface(d["note_difference"])
		a.diff += diff
		if id := strField(d, "debit_note_id"); id != "" {
			a.dn += math.Max(diff, 0)
			a.notes = append(a.notes, id)
		}
		if id := strField(d, "vendor_credit_note_id"); id != "" {
			a.vcn += math.Max(-diff, 0)
			a.notes = append(a.notes, id)
		}
	}
	out := []map[string]interface{}{}
	for _, a := range groups {
		out = append(out, map[string]interface{}{"month": a.month, "vendor": a.vendor, "receipts": a.n,
			"invoice_total": round2(a.inv), "our_total": round2(a.ours), "difference": round2(a.diff),
			"debit_notes": round2(a.dn), "credit_notes": round2(a.vcn), "note_numbers": strings.Join(a.notes, ", ")})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i]["month"] != out[j]["month"] {
			return out[i]["month"].(string) > out[j]["month"].(string)
		}
		return out[i]["vendor"].(string) < out[j]["vendor"].(string)
	})
	return out, nil
}

func grnPriceChangesReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	if !grnLineTablesReady(db.DB, schema) {
		return out, nil
	}
	var args []interface{}
	where := grnDateFilter("posted_at", params, &args) + grnVendorFilter("vendor", params, &args)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT posted_at, sku, COALESCE(vendor, ''), grn_code, prev_price, COALESCE(prev_vendor, ''), grn_price, old_mrp, new_mrp
		FROM %s.grn_line WHERE posted_at IS NOT NULL AND prev_price IS NOT NULL AND grn_price IS NOT NULL
		  AND abs(grn_price - prev_price) >= 0.005 %s ORDER BY posted_at DESC LIMIT 5000`, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var at sql.NullTime
		var sku, vend, grn, prevVend string
		var prev, price, oldMRP, newMRP sql.NullFloat64
		if err := rows.Scan(&at, &sku, &vend, &grn, &prev, &prevVend, &price, &oldMRP, &newMRP); err != nil {
			return nil, err
		}
		pct := interface{}(nil)
		if prev.Float64 > 0 {
			pct = round2((price.Float64 - prev.Float64) / prev.Float64 * 100)
		}
		out = append(out, map[string]interface{}{"posted_at": nullTimeText(at), "sku": sku, "vendor": vend, "grn": grn,
			"prev_price": nullFloat(prev), "prev_vendor": prevVend, "grn_price": nullFloat(price), "change_pct": pct,
			"old_mrp": nullFloat(oldMRP), "new_mrp": nullFloat(newMRP)})
	}
	return out, rows.Err()
}

func grnWaitingReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	rows, err := ListGRNInbox(tenantID)
	if err != nil {
		return nil, err
	}
	label := map[string]string{"verify": "Verify", "price": "Price check / MRP and post", "pay": "Payment"}
	out := []map[string]interface{}{}
	for _, r := range rows {
		if r.Tab == "done" {
			continue
		}
		pending := ""
		if r.ExcessPending {
			pending = "Yes"
		}
		out = append(out, map[string]interface{}{"grn": r.Code, "vendor": r.Vendor, "vendor_invoice_no": r.InvoiceNo,
			"waiting_for": label[r.Tab], "age_days": round2(r.AgeHours / 24), "invoice_total": r.InvoiceTotal, "excess_pending": pending})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["age_days"].(float64) > out[j]["age_days"].(float64) })
	return out, nil
}

func grnCorrectionsReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	if !grnLineTablesReady(db.DB, schema) {
		return out, nil
	}
	var args []interface{}
	where := grnDateFilter("c.changed_at", params, &args)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT c.changed_at, COALESCE(d.data->>'code', c.grn_id), c.line_no, COALESCE(c.sku, ''), c.step, c.field,
		COALESCE(c.old_value, ''), COALESCE(c.new_value, ''), COALESCE(c.reason, ''), COALESCE(c.changed_by, '')
		FROM %s.grn_line_change c LEFT JOIN %s.documents d ON d.doctype = 'GRN' AND d.id = c.grn_id
		WHERE TRUE %s ORDER BY c.changed_at DESC, c.id DESC LIMIT 5000`, schema, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var at time.Time
		var grn, sku, step, field, oldV, newV, reason, by string
		var line sql.NullInt64
		if err := rows.Scan(&at, &grn, &line, &sku, &step, &field, &oldV, &newV, &reason, &by); err != nil {
			return nil, err
		}
		var ln interface{}
		if line.Valid {
			ln = line.Int64
		}
		out = append(out, map[string]interface{}{"changed_at": at.Format("2006-01-02 15:04"), "grn": grn, "line_no": ln, "sku": sku,
			"step": step, "field": strings.ReplaceAll(field, "_", " "), "old_value": oldV, "new_value": newV, "reason": reason, "changed_by": by})
	}
	return out, rows.Err()
}

func grnPOVarianceReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	if !grnLineTablesReady(db.DB, schema) {
		return out, nil
	}
	var args []interface{}
	where := grnDateFilter("posted_at", params, &args) + grnVendorFilter("vendor", params, &args)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT posted_at, COALESCE(vendor, ''), COALESCE(po_id, ''), grn_code, sku,
		COALESCE(grn_qty, 0) - COALESCE(rejected_qty, 0) - COALESCE(damaged_qty, 0), po_rate, grn_price
		FROM %s.grn_line WHERE posted_at IS NOT NULL AND po_rate IS NOT NULL AND grn_price IS NOT NULL %s
		ORDER BY posted_at DESC LIMIT 5000`, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var at sql.NullTime
		var vend, po, grn, sku string
		var qty, poRate, price float64
		if err := rows.Scan(&at, &vend, &po, &grn, &sku, &qty, &poRate, &price); err != nil {
			return nil, err
		}
		v := round2(price - poRate)
		pct := interface{}(nil)
		if poRate > 0 {
			pct = round2(v / poRate * 100)
		}
		out = append(out, map[string]interface{}{"posted_at": nullTimeText(at), "vendor": vend, "po_id": po, "grn": grn, "sku": sku,
			"qty": round2(qty), "po_rate": round2(poRate), "grn_price": round2(price), "variance": v, "variance_pct": pct, "variance_value": round2(v * qty)})
	}
	return out, rows.Err()
}

func grnPaymentsReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	docs, err := stepGRNDocs(tenantID, params)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for _, d := range docs {
		if strField(d, "grn_step") != GRNStepPosted {
			continue
		}
		due := grnNetPayable(tenantID, d)
		if params["due_only"] == "Yes" && due <= 0.004 {
			continue
		}
		pays := grnPayments(d)
		lastPaid, lastMode := "", ""
		if len(pays) > 0 {
			lastPaid, lastMode = pays[len(pays)-1].Date, pays[len(pays)-1].Mode
		}
		out = append(out, map[string]interface{}{"grn": firstNonEmpty(strField(d, "code"), d["_id"].(string)), "vendor": strField(d, "vendor"),
			"vendor_invoice_no": strField(d, "vendor_invoice_no"), "invoice_total": numFromInterface(d["vendor_invoice_amount"]),
			"our_total": numFromInterface(d["our_total"]), "paid": numFromInterface(d["paid_amount"]), "due": due,
			"payment_status": firstNonEmpty(strField(d, "payment_status"), "Due"), "last_paid": lastPaid, "last_mode": lastMode})
	}
	return out, nil
}

// grnInReceivingReport is stock that has arrived but is not sellable yet: the
// lines of step receipts whose child GRN has not posted.
func grnInReceivingReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	if !grnLineTablesReady(db.DB, schema) {
		return out, nil
	}
	var args []interface{}
	where := ""
	if st := strings.TrimSpace(params["store"]); st != "" {
		args = append(args, st)
		where = " AND l.location = $1"
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(l.location, ''), l.sku, SUM(COALESCE(l.grn_qty, l.received_qty, 0))::float8,
		string_agg(DISTINCT l.grn_code, ', '), MIN(l.received_at)
		FROM %s.grn_line l JOIN %s.documents d ON d.doctype = 'GRN' AND d.id = l.grn_id AND d.status <> 'Cancelled' AND d.deleted_at IS NULL
		WHERE l.flow = 'steps' AND l.posted_at IS NULL %s
		GROUP BY 1, 2 ORDER BY 1, 2`, schema, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var loc, sku, grns string
		var qty float64
		var since sql.NullTime
		if err := rows.Scan(&loc, &sku, &qty, &grns, &since); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"location": loc, "sku": sku, "qty": round2(qty), "receipts": grns, "oldest": nullTimeText(since)})
	}
	return out, rows.Err()
}
