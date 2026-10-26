package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 130.16 (74.3.2, BRD 4.10, DEC-19): a PO's commercial terms and a
// GRN's logistics. The PO carries its payment term (the PaymentTerm master,
// with net days), delivery terms, who pays the freight, the vendor contact
// and the quote's validity; with procurement.po_terms_required on (off by
// default - old clients unchanged) they are required when it goes for
// approval, never on a draft. The approved terms are kept on the PO
// (approved_terms), and a later change to an approved PO's terms needs the
// amendment reason and is written, before and after, to its amendment_log.
// The GRN carries the vendor's challan and the transport; reports read both.

const SettingPOTermsRequired = "procurement.po_terms_required"

// poTermsRequired are the terms DEC-19 asks for at approval.
var poTermsRequired = []struct{ key, label string }{
	{"payment_term", "payment term"}, {"delivery_terms", "delivery terms"},
	{"expected_delivery_date", "expected delivery date"}, {"freight_by", "who pays the freight"},
}

// poTermFields are what approved_terms keeps and amendment_log compares.
var poTermFields = []string{"payment_term", "payment_terms", "delivery_terms", "freight_by", "freight_amount", "vendor_contact",
	"quote_valid_until", "expected_delivery_date", "location", "target_warehouse"}

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingPOTermsRequired, Module: "Procurement",
		Label: "PO commercial terms are required for approval", Type: SettingTypeBool, Default: "false",
		Description: "On: a purchase order cannot go for approval (or be approved) without its payment term, delivery terms, expected delivery date and who pays the freight, and not after its quote's validity date. Drafts never need them.",
	})
	RegisterApprovalSubmitCheck("PurchaseOrder", func(tenantID, docID string, data map[string]interface{}) error {
		return validatePOTermsForApproval(tenantID, data)
	})
	RegisterApprovalDecisionHook("PurchaseOrder", false, func(tenantID, docID, decision, actorUserID string) {
		if decision != "Approved" {
			return
		}
		data, _, err := fetchDocData(tenantID, "PurchaseOrder", docID)
		if err != nil || data == nil {
			return
		}
		schema, err := db.GetTenantSchema(tenantID)
		if err != nil {
			return
		}
		if err := mergeDocData(schema, "PurchaseOrder", docID, map[string]interface{}{"approved_terms": poTermsSnapshot(data)}); err != nil {
			LogSystemError(tenantID, actorUserID, "WARN", "POApprovedTerms", fmt.Sprintf("PO %s: approved terms not kept: %v", docID, err), "")
		}
	})
	RegisterReport(ReportDefinition{ID: "po-delivery-and-terms", Label: "PO expected delivery and terms", Category: "Procurement",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "po", Label: "PO"}, {Key: "vendor", Label: "Vendor"}, {Key: "location", Label: "Store"}, {Key: "status", Label: "Status"},
			{Key: "expected_delivery_date", Label: "Expected delivery"}, {Key: "days_late", Label: "Days late"}, {Key: "payment_term", Label: "Payment term"},
			{Key: "delivery_terms", Label: "Delivery terms"}, {Key: "freight_by", Label: "Freight paid by"}, {Key: "quote_valid_until", Label: "Quote valid until"},
			{Key: "amendments", Label: "Amendments"}, {Key: "value", Label: "Value"}},
		Run: poDeliveryAndTerms})
	RegisterReport(ReportDefinition{ID: "grn-discrepancy-by-vendor", Label: "Goods receipt discrepancies by vendor", Category: "Procurement",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "vendor", Label: "Vendor"}, {Key: "receipts", Label: "Receipts"}, {Key: "received", Label: "Received"},
			{Key: "accepted", Label: "Accepted"}, {Key: "rejected", Label: "Rejected"}, {Key: "damaged", Label: "Damaged"},
			{Key: "short_vs_invoice", Label: "Short of the invoice"}, {Key: "invoice_difference", Label: "Invoice difference"}, {Key: "rejected_pct", Label: "Rejected %"}},
		Run: grnDiscrepancyByVendor, StoreKey: "-"})
	RegisterReport(ReportDefinition{ID: "vendor-document-transport-register", Label: "Vendor document and transport register", Category: "Procurement",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "grn", Label: "Goods receipt"}, {Key: "received_at", Label: "Received"}, {Key: "vendor", Label: "Vendor"}, {Key: "location", Label: "Store"},
			{Key: "vendor_invoice_no", Label: "Invoice no"}, {Key: "vendor_invoice_date", Label: "Invoice date"}, {Key: "vendor_challan_no", Label: "Challan no"},
			{Key: "vendor_challan_date", Label: "Challan date"}, {Key: "lr_awb_no", Label: "LR / AWB"}, {Key: "vehicle_no", Label: "Vehicle"},
			{Key: "transporter", Label: "Transporter"}, {Key: "package_count", Label: "Packages"}},
		Run: vendorDocumentTransportRegister})
	RegisterReport(ReportDefinition{ID: "grni", Label: "Goods received not invoiced (GRNI)", Category: "Procurement",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "grn", Label: "Goods receipt"}, {Key: "date", Label: "Date"}, {Key: "vendor", Label: "Vendor"}, {Key: "location", Label: "Store"},
			{Key: "accepted_value", Label: "Accepted value"}, {Key: "invoiced_value", Label: "Invoiced"}, {Key: "open_value", Label: "Not invoiced"},
			{Key: "price_difference", Label: "Price difference"}, {Key: "invoices", Label: "Vendor invoices"}, {Key: "ap_hold", Label: "AP hold"}},
		Run: grniReport})
}

// validatePOTermsForApproval is DEC-19's check, when the client asks for it.
func validatePOTermsForApproval(tenantID string, data map[string]interface{}) error {
	if !GetSettingBool(tenantID, SettingPOTermsRequired) {
		return nil
	}
	var missing []string
	for _, f := range poTermsRequired {
		if strings.TrimSpace(strField(data, f.key)) == "" {
			missing = append(missing, f.label)
		}
	}
	if len(missing) > 0 {
		return &ValidationError{Code: "PURCHA-1316", SubFor: "Commercial terms", Message: "before approval, enter the PO's " + strings.Join(missing, ", ")}
	}
	if v := strings.TrimSpace(strField(data, "quote_valid_until")); v != "" {
		if until, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil && until.Before(todayDate()) {
			return &ValidationError{Code: "PURCHA-1316", SubFor: "Quote valid until", Message: fmt.Sprintf("the vendor's quote was valid until %s - get it renewed (or change the date) before approval", v)}
		}
	}
	return nil
}

// poTermsSnapshot is the terms as approved.
func poTermsSnapshot(data map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{"approved_at": time.Now().UTC().Format(time.RFC3339)}
	for _, k := range poTermFields {
		if v, ok := data[k]; ok {
			out[k] = v
		}
	}
	return out
}

// poTermChanges is each term that differs, before and after.
func poTermChanges(prior, payload map[string]interface{}) map[string][]string {
	out := map[string][]string{}
	for _, k := range poTermFields {
		if _, sent := payload[k]; !sent {
			continue
		}
		before, after := termText(prior[k]), termText(payload[k])
		if before != after {
			out[k] = []string{before, after}
		}
	}
	return out
}

// termText is a term as compared: nothing, "" and 0 are all "not set".
func termText(v interface{}) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "0" {
		return ""
	}
	return s
}

// preparePOTerms runs in the PO's save check: the terms are required on the
// save that sends it for approval or approves it; approving keeps them; an
// approved PO's changed terms need the amendment reason and are logged.
func preparePOTerms(tenantID, priorStatus string, priorData, payload map[string]interface{}) error {
	merged := map[string]interface{}{}
	for k, v := range priorData {
		merged[k] = v
	}
	for k, v := range payload {
		merged[k] = v
	}
	// The payment term's name prints; the code is what is chosen.
	if code := strings.TrimSpace(strField(payload, "payment_term")); code != "" && code != strField(priorData, "payment_term") {
		if term, _, err := fetchDocData(tenantID, "PaymentTerm", code); err == nil && term != nil {
			payload["payment_terms"] = firstNonEmpty(strField(term, "name"), code)
			merged["payment_terms"] = payload["payment_terms"]
		}
	}
	status := strField(merged, "status")
	entering := (status == "Pending Approval" || status == "Approved") && priorStatus != "Pending Approval" && priorStatus != "Approved"
	if entering {
		if err := validatePOTermsForApproval(tenantID, merged); err != nil {
			return err
		}
	}
	if status == "Approved" && priorStatus != "Approved" {
		payload["approved_terms"] = poTermsSnapshot(merged)
	}
	if priorStatus != "Approved" || priorData == nil {
		return nil
	}
	changes := poTermChanges(priorData, payload)
	if len(changes) == 0 {
		return nil
	}
	reason := strings.TrimSpace(strField(payload, "amendment_reason"))
	if reason == "" {
		return &ValidationError{Code: "PURCHA-0085", Message: "amendment reason is required to change the terms of an Approved purchase order"}
	}
	var log []map[string]interface{}
	if raw := strField(priorData, "amendment_log"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &log)
	}
	log = append(log, map[string]interface{}{"at": time.Now().UTC().Format(time.RFC3339), "reason": reason, "changes": changes})
	encoded, _ := json.Marshal(log)
	payload["amendment_log"] = string(encoded)
	return nil
}

// POPaymentDueDate is a bill's due date by its PO's payment term: the
// invoice date plus the term's net days ("" when either is unknown).
func POPaymentDueDate(tenantID, poID, invoiceDate string) string {
	if poID == "" || invoiceDate == "" {
		return ""
	}
	po, _, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil || po == nil || strField(po, "payment_term") == "" {
		return ""
	}
	term, _, err := fetchDocData(tenantID, "PaymentTerm", strField(po, "payment_term"))
	if err != nil || term == nil {
		return ""
	}
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(invoiceDate), time.Local)
	if err != nil {
		return ""
	}
	return day.AddDate(0, 0, int(numFromInterface(term["net_days"]))).Format("2006-01-02")
}

// --- reports ------------------------------------------------------------------

type reportDoc struct {
	id     string
	at     time.Time
	data   map[string]interface{}
	status string
}

func docsBetween(tenantID, doctype string, p map[string]string) ([]reportDoc, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, created_at, data, status FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND ($2 = '' OR created_at >= public.erp_day_start($2::date)) AND ($3 = '' OR created_at < public.erp_day_start($3::date + 1)) ORDER BY created_at DESC LIMIT 5000`, schema),
		doctype, strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reportDoc
	for rows.Next() {
		var d reportDoc
		var raw string
		if err := rows.Scan(&d.id, &d.at, &raw, &d.status); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &d.data)
		out = append(out, d)
	}
	return out, rows.Err()
}

func poDeliveryAndTerms(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	docs, err := docsBetween(tenantID, "PurchaseOrder", p)
	if err != nil {
		return nil, err
	}
	today := todayDate()
	var out []map[string]interface{}
	for _, d := range docs {
		if d.status == "Cancelled" {
			continue
		}
		late := 0
		if v := strField(d.data, "expected_delivery_date"); v != "" && d.status != "Received" && d.status != "Closed" {
			if due, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil && today.After(due) {
				late = int(today.Sub(due).Hours() / 24)
			}
		}
		var log []interface{}
		_ = json.Unmarshal([]byte(strField(d.data, "amendment_log")), &log)
		out = append(out, map[string]interface{}{"po": firstNonEmpty(strField(d.data, "po_number"), strField(d.data, "code"), d.id),
			"vendor": firstNonEmpty(strField(d.data, "vendor"), strField(d.data, "vendor_id")), "location": strField(d.data, "location"), "status": d.status,
			"expected_delivery_date": strField(d.data, "expected_delivery_date"), "days_late": late,
			"payment_term": firstNonEmpty(strField(d.data, "payment_term"), strField(d.data, "payment_terms")), "delivery_terms": strField(d.data, "delivery_terms"),
			"freight_by": strField(d.data, "freight_by"), "quote_valid_until": strField(d.data, "quote_valid_until"), "amendments": len(log),
			"value": round2(firstPositive(numFromInterface(d.data["grand_total"]), numFromInterface(d.data["total_amount"])))})
	}
	return out, nil
}

func grnDiscrepancyByVendor(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	docs, err := docsBetween(tenantID, "GRN", p)
	if err != nil {
		return nil, err
	}
	type agg struct {
		receipts                                           int
		received, accepted, rejected, damaged, short, diff float64
	}
	by := map[string]*agg{}
	for _, d := range docs {
		if d.status == "Cancelled" {
			continue
		}
		v := strField(d.data, "vendor")
		a := by[v]
		if a == nil {
			a = &agg{}
			by[v] = a
		}
		a.receipts++
		for _, l := range grnLineMaps(d.data) {
			rec := firstPositive(numFromInterface(l["received_qty"]), numFromInterface(l["qty"]))
			a.received += rec
			a.accepted += numFromInterface(l["accepted_qty"])
			a.rejected += numFromInterface(l["rejected_qty"])
			a.damaged += numFromInterface(l["damaged_qty"])
			if iq := numFromInterface(l["invoice_qty"]); iq > rec {
				a.short += iq - rec
			}
		}
		a.diff += firstNonZero(numFromInterface(d.data["note_difference"]), numFromInterface(d.data["invoice_amount_difference"]))
	}
	var out []map[string]interface{}
	for v, a := range by {
		pct := 0.0
		if a.received > 0 {
			pct = round2(a.rejected * 100 / a.received)
		}
		out = append(out, map[string]interface{}{"vendor": v, "receipts": a.receipts, "received": a.received, "accepted": a.accepted, "rejected": a.rejected,
			"damaged": a.damaged, "short_vs_invoice": a.short, "invoice_difference": round2(a.diff), "rejected_pct": pct})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["rejected_pct"].(float64) > out[j]["rejected_pct"].(float64) })
	return out, nil
}

func firstNonZero(vals ...float64) float64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

func vendorDocumentTransportRegister(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	docs, err := docsBetween(tenantID, "GRN", p)
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	for _, d := range docs {
		row := map[string]interface{}{"grn": firstNonEmpty(strField(d.data, "code"), d.id), "received_at": firstNonEmpty(strField(d.data, "received_at"), d.at.Format(time.RFC3339)),
			"vendor": strField(d.data, "vendor"), "location": strField(d.data, "location"), "package_count": numFromInterface(d.data["package_count"])}
		for _, k := range []string{"vendor_invoice_no", "vendor_invoice_date", "vendor_challan_no", "vendor_challan_date", "lr_awb_no", "vehicle_no", "transporter"} {
			row[k] = strField(d.data, k)
		}
		out = append(out, row)
	}
	return out, nil
}

// grniReport is R-TX-15: each receipt's accepted value (its credit to 2100)
// against what its vendor invoices cleared (their debit to 2100).
func grniReport(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	docs, err := docsBetween(tenantID, "GRN", p)
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	for _, d := range docs {
		if d.status == "Cancelled" {
			continue
		}
		var accepted, invoiced int64
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(credit - debit), 0) FROM %s.gl_postings WHERE account_code = $1 AND document_type = 'GRN' AND document_id = $2`, schema),
			AccountAPSuspense, d.id).Scan(&accepted)
		if accepted == 0 {
			continue
		}
		var numbers []string
		hold := false
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'invoice_number', id), status FROM %s.documents WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL AND data->>'grn_id' = $1`, schema), d.id)
		if err != nil {
			return nil, err
		}
		var ids []string
		for rows.Next() {
			var id, no, status string
			if rows.Scan(&id, &no, &status) == nil {
				ids, numbers = append(ids, id), append(numbers, no)
				hold = hold || status == "MismatchHold"
			}
		}
		rows.Close()
		if len(ids) > 0 {
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(debit - credit), 0) FROM %s.gl_postings WHERE account_code = $1 AND document_type = 'VendorInvoice' AND document_id = ANY($2)`, schema),
				AccountAPSuspense, pqStringArray(ids)).Scan(&invoiced)
		}
		out = append(out, map[string]interface{}{"grn": firstNonEmpty(strField(d.data, "code"), d.id), "date": d.at.Format("2006-01-02"), "vendor": strField(d.data, "vendor"),
			"location": strField(d.data, "location"), "accepted_value": PaiseToRupees(accepted), "invoiced_value": PaiseToRupees(invoiced),
			"open_value": PaiseToRupees(accepted - invoiced), "price_difference": round2(firstNonZero(numFromInterface(d.data["note_difference"]), numFromInterface(d.data["invoice_amount_difference"]))),
			"invoices": strings.Join(numbers, ", "), "ap_hold": hold})
	}
	return out, nil
}
