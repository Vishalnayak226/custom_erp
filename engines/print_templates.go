package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Stage 63.2: PrintTemplate records - one designed layout per transaction,
// optionally mapped to a vendor and/or product families / categories - and
// the rule that picks one for a document.
//
// A mapping narrows: a template naming a vendor is only offered for that
// vendor's documents, one naming families / categories only when a line's
// Item belongs to one of them. Among those offered, the most specific wins
// (vendor 8, family 4, category 2, default 1). With no record at all, the
// page uses its built-in standard layout for the transaction.

// PrintTemplateChoice is one template offered for a document.
type PrintTemplateChoice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Score       int    `json:"score"`
	Recommended bool   `json:"recommended"`
	Layout      string `json:"layout"`
	Reason      string `json:"reason,omitempty"`
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func anyIn(want []string, have []string) bool {
	for _, w := range want {
		for _, h := range have {
			if strings.EqualFold(w, h) {
				return true
			}
		}
	}
	return false
}

// scorePrintTemplate is the mapping rule; ok=false when the template is
// mapped to a vendor / product the document does not have.
func scorePrintTemplate(t map[string]interface{}, doc PrintDocument) (score int, reason string, ok bool) {
	var why []string
	if v := strings.TrimSpace(strField(t, "vendor")); v != "" {
		if !strings.EqualFold(v, doc.Vendor) {
			return 0, "", false
		}
		score += 8
		why = append(why, "vendor "+v)
	}
	if fams := splitList(strField(t, "item_families")); len(fams) > 0 {
		if !anyIn(fams, doc.Families) {
			return 0, "", false
		}
		score += 4
		why = append(why, "product design/family")
	}
	if cats := splitList(strField(t, "item_categories")); len(cats) > 0 {
		if !anyIn(cats, doc.Categories) {
			return 0, "", false
		}
		score += 2
		why = append(why, "product category")
	}
	if d := strings.ToLower(strField(t, "is_default")); d == "yes" || d == "true" || d == "1" {
		score++
		why = append(why, "default")
	}
	return score, strings.Join(why, ", "), true
}

// PrintTemplatesFor lists the Active templates a document may print with,
// best match first (that one is Recommended).
func PrintTemplatesFor(tenantID string, doc PrintDocument) ([]PrintTemplateChoice, error) {
	out := []PrintTemplateChoice{}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'PrintTemplate' AND deleted_at IS NULL AND COALESCE(status, 'Active') <> 'Inactive'
		  AND data->>'transaction' = $1`, schema), doc.Transaction)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return out, err
		}
		var t map[string]interface{}
		if json.Unmarshal([]byte(raw), &t) != nil || strField(t, "status") == "Inactive" {
			continue
		}
		score, reason, ok := scorePrintTemplate(t, doc)
		if !ok {
			continue
		}
		out = append(out, PrintTemplateChoice{ID: id, Name: firstNonEmpty(strField(t, "name"), strField(t, "code"), id),
			Score: score, Layout: strField(t, "layout"), Reason: reason})
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if len(out) > 0 {
		out[0].Recommended = true
	}
	return out, nil
}

// PrintField is one value the designer can place.
type PrintField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Group string `json:"group"`
}

// PrintCatalog is the designer's field list for a transaction.
type PrintCatalog struct {
	Transactions []map[string]string `json:"transactions"`
	Header       []PrintField        `json:"header"`
	Line         []PrintField        `json:"line"`
}

var printStoreFieldLabels = [][2]string{
	{"name", "Name"}, {"legal_name", "Legal name"}, {"address", "Address"}, {"city", "City"}, {"pincode", "PIN code"},
	{"state", "State"}, {"state_code", "State code"}, {"gstin", "GSTIN"}, {"phone", "Phone"}, {"email", "Email"}, {"code", "Code"},
}

// GetPrintCatalog lists every placeable value for txn: document header
// fields (from the doctype's own field list, so a custom field shows up
// too), brand, store / party / ship-to blocks, totals, and line columns
// including every Item field.
func GetPrintCatalog(tenantID, txn string) (PrintCatalog, error) {
	out := PrintCatalog{}
	for _, t := range PrintTransactions {
		out.Transactions = append(out.Transactions, map[string]string{"key": t.Key, "label": t.Label, "doctype": t.Doctype})
	}
	doctype := printTxnDoctype(txn)
	if doctype == "" {
		return out, &ValidationError{Code: "GLOBAL-0002", SubFor: "Transaction", Message: fmt.Sprintf("%q is not a printable transaction", txn)}
	}
	add := func(list *[]PrintField, group, key, label string) {
		*list = append(*list, PrintField{Key: key, Label: label, Group: group})
	}
	add(&out.Header, "Document", "title", "Document title")
	add(&out.Header, "Document", "number", "Document number")
	add(&out.Header, "Document", "date", "Date")
	add(&out.Header, "Document", "status", "Status")
	add(&out.Header, "Document", "brand.name", "Brand name")
	if metas, err := GetDocTypeMeta(tenantID, doctype); err == nil {
		for _, m := range metas {
			if m.Fieldtype == "JSONTable" || m.Fieldname == "items" || m.Fieldname == "received_items" {
				continue
			}
			add(&out.Header, "Document", "fields."+m.Fieldname, m.Label)
		}
	}
	switch txn {
	case PrintTxnGRN, PrintTxnGRNVendorCopy, PrintTxnDebitNote, PrintTxnVendorCreditNote, PrintTxnPurchaseReturn:
		add(&out.Header, "Document", "fields.po_numbers", "PO numbers")
	case PrintTxnTransferOut, PrintTxnTransferIn:
		add(&out.Header, "Document", "fields.supply_type", "Supply type")
		add(&out.Header, "Document", "fields.value_basis", "Value basis")
		add(&out.Header, "Document", "fields.stock_status_after", "Stock status after posting")
		if txn == PrintTxnTransferIn {
			add(&out.Header, "Document", "fields.receipt_status", "Receipt status")
		}
	case PrintTxnPurchaseOrder, PrintTxnVendorPO:
		add(&out.Header, "Document", "fields.gst_mode", "GST treatment of prices")
	case PrintTxnPOSTaxInvoice:
		for _, f := range [][2]string{{"buyer_gstin", "Buyer GSTIN"}, {"place_of_supply", "Place of supply"}, {"supply_type", "Supply type (B2B / B2C)"},
			{"reverse_charge", "Reverse charge"}, {"payment_mode", "Payment mode"}, {"cart_number", "Till cart number"},
			{"bill_datetime", "Bill date and time"}, {"cashier", "Cashier"}} {
			add(&out.Header, "Document", "fields."+f[0], f[1])
		}
	case PrintTxnSalesReturn:
		for _, f := range [][2]string{{"original_order", "Original bill / order"}, {"original_invoice_date", "Original bill date"},
			{"return_type", "Return type"}, {"refund_method", "Refund method"}, {"refund_no", "Refund number"}, {"refund_status", "Refund status"}} {
			add(&out.Header, "Document", "fields."+f[0], f[1])
		}
	}
	switch txn {
	case PrintTxnDebitNote, PrintTxnVendorCreditNote, PrintTxnCreditNote:
		add(&out.Header, "Document", "fields.original_invoice_no", "Original invoice number")
		add(&out.Header, "Document", "fields.original_invoice_date", "Original invoice date")
		add(&out.Header, "Document", "fields.document_direction", "Document direction")
		if txn == PrintTxnCreditNote {
			// Stage 75.2.8a: the e-invoice details (blank until the IRP acknowledges).
			for _, f := range [][2]string{{"irn", "IRN"}, {"irn_ack_no", "IRN Ack No"}, {"irn_ack_date", "IRN Ack date"},
				{"irn_status", "IRN status (IRN PENDING)"}, {"irn_signed_qr", "IRN signed QR (for a QR code)"}} {
				add(&out.Header, "E-invoice", "fields."+f[0], f[1])
			}
		}
	case PrintTxnPurchaseReturn:
		add(&out.Header, "Document", "fields.vendor_invoice_no", "Vendor invoice number")
		add(&out.Header, "Document", "fields.vendor_invoice_date", "Vendor invoice date")
	}
	// Stage 71: the common header and audit fields of every print.
	add(&out.Header, "Document", "fields.financial_year", "Financial year")
	add(&out.Header, "Document", "fields.currency", "Currency")
	for _, f := range [][2]string{{"created_by", "Created by"}, {"created_at", "Created at"}, {"approved_by", "Approved by"},
		{"approved_at", "Approved at"}, {"printed_by", "Printed by"}, {"printed_at", "Printed at"}} {
		add(&out.Header, "Audit", "audit."+f[0], f[1])
	}
	for _, block := range [][2]string{{"store", "Generating store"}, {"party", "Vendor / other store"}, {"ship_to", "Ship to"}} {
		for _, f := range printStoreFieldLabels {
			add(&out.Header, block[1], block[0]+"."+f[0], block[1]+" - "+f[1])
		}
	}
	for _, t := range [][2]string{
		{"qty", "Total quantity"}, {"accepted_qty", "Total accepted"}, {"po_qty", "Total ordered (PO)"}, {"rejected_qty", "Total rejected"},
		{"damaged_qty", "Total damaged"}, {"shipped_qty", "Total shipped"}, {"received_qty", "Total received"}, {"short_qty", "Total short"},
		{"return_qty", "Total returned"}, {"taxable", "Taxable value"}, {"cgst", "CGST"},
		{"sgst", "SGST"}, {"igst", "IGST"}, {"tax", "Total tax"}, {"amount", "Total before rounding"},
		{"round_off", "Round off"}, {"grand_total", "Grand total"}, {"line_count", "Number of lines"}, {"discount", "Item discount"},
	} {
		add(&out.Header, "Totals", "totals."+t[0], t[1])
	}
	add(&out.Header, "Totals", "amount_in_words", "Amount in words")

	for _, f := range [][2]string{
		{"sr", "Sr."}, {"sku", "SKU / Combination ID"}, {"item_name", "Item name"}, {"image", "Item image"}, {"hsn_code", "HSN"},
		{"barcode", "Barcode"}, {"uom", "UOM"}, {"qty", "Quantity"}, {"rate", "Rate"}, {"mrp", "MRP"}, {"taxable", "Taxable value"},
		{"gst_rate", "GST %"}, {"cgst", "CGST"}, {"sgst", "SGST"}, {"igst", "IGST"}, {"tax", "Tax"}, {"amount", "Amount"},
		{"po_id", "PO"}, {"batch_no", "Batch"},
		{"description", "Description (name · variant)"}, {"variant", "Variant"}, {"design", "Design / parent product"}, {"brand", "Brand"},
		{"item_code", "Item / Barcode"},
	} {
		add(&out.Line, "Line", f[0], f[1])
	}
	// Stage 87: each variant attribute (Colour, Size...) as a column of its own.
	if m, err := GetDesignMatrix(tenantID); err == nil {
		for _, a := range m.Attributes {
			add(&out.Line, "Attributes", "attributes."+a.Code, a.Label)
		}
	}
	switch txn {
	case PrintTxnGRN, PrintTxnGRNVendorCopy:
		for _, f := range [][2]string{{"po_qty", "PO (ordered) qty"}, {"accepted_qty", "Accepted"}, {"rejected_qty", "Rejected"}, {"damaged_qty", "Damaged"},
			{"qc_status", "QC status"}, {"variance_reason", "Rejection / damage reason"}} {
			add(&out.Line, "Line", f[0], f[1])
		}
		if txn == PrintTxnGRN {
			for _, f := range [][2]string{{"po_rate", "PO rate"}, {"invoice_rate", "Invoice rate"}, {"rate_difference", "Rate difference"}} {
				add(&out.Line, "Line", f[0], f[1])
			}
		}
	case PrintTxnDebitNote, PrintTxnVendorCreditNote:
		add(&out.Line, "Line", "po_rate", "PO rate")
		add(&out.Line, "Line", "invoice_rate", "Invoice rate")
		add(&out.Line, "Line", "original_taxable", "Original taxable value")
		add(&out.Line, "Line", "reason", "Reason")
	case PrintTxnTransferIn, PrintTxnTransferOut:
		for _, f := range [][2]string{{"requested_qty", "Requested qty"}, {"shipped_qty", "Shipped qty"}} {
			add(&out.Line, "Line", f[0], f[1])
		}
		if txn == PrintTxnTransferIn {
			for _, f := range [][2]string{{"received_qty", "Received qty"}, {"short_qty", "Short qty"}, {"reason", "Shortage reason"}} {
				add(&out.Line, "Line", f[0], f[1])
			}
		}
	case PrintTxnPurchaseReturn:
		for _, f := range [][2]string{{"received_qty", "Received qty (GRN)"}, {"return_qty", "Return qty"}, {"stock_bucket", "Returned from (accepted / rejected / damaged)"}, {"reason", "Reason"}} {
			add(&out.Line, "Line", f[0], f[1])
		}
	case PrintTxnPOSTaxInvoice:
		add(&out.Line, "Line", "discount", "Discount (offer share)")
	case PrintTxnSalesReturn:
		for _, f := range [][2]string{{"original_qty", "Original qty"}, {"return_qty", "Return qty"}, {"original_disc", "Original discount / unit"},
			{"condition", "Condition"}, {"exchange_sku", "Exchanged for"}} {
			add(&out.Line, "Line", f[0], f[1])
		}
	}
	if metas, err := GetDocTypeMeta(tenantID, "Item"); err == nil {
		for _, m := range metas {
			if m.Fieldtype == "JSONTable" {
				continue
			}
			add(&out.Line, "Item master", "item."+m.Fieldname, "Item - "+m.Label)
		}
	}
	return out, nil
}
