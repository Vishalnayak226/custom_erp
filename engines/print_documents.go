package engines

import (
	"custom_erp/db"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// Stage 63.1: one print-data shape for every transaction a user designs a
// print template for. The template (public/trade-docs.js) places any of these
// values anywhere on the page, so every transaction prints the same way:
//
//	brand        - always printed (setting print.brand_name, else the legal entity)
//	store        - the store the document is generated from (chosen at print time)
//	party        - the vendor, or the other store of a transfer
//	ship_to      - where the goods go, when that is not the store
//	fields       - every header field of the document, plus computed ones
//	lines        - every line, with its Item master fields under item.*
//	tax_summary  - GST split per rate (CGST / SGST / IGST)
//	totals       - qty, taxable, each tax, grand total, amount in words
//
// Values are server-computed from the same engines the documents post with
// (PreviewPurchaseOrder, the GRN's own prices), never re-derived in the page.

// Print transactions - the keys a PrintTemplate is designed for.
const (
	PrintTxnPurchaseOrder    = "PurchaseOrder"
	PrintTxnVendorPO         = "VendorPO"
	PrintTxnGRN              = "GRN"
	PrintTxnTransferOut      = "TransferOut"
	PrintTxnTransferIn       = "TransferIn"
	PrintTxnDebitNote        = "DebitNote"
	PrintTxnVendorCreditNote = "VendorCreditNote"
	// Stage 71 (the user's standard print formats pack): the vendor copy is
	// a print of the same GRN with every value taken out, the others print
	// documents that already existed without a print.
	PrintTxnGRNVendorCopy  = "GRNVendorCopy"
	PrintTxnPurchaseReturn = "PurchaseReturn"
	PrintTxnCreditNote     = "CreditNote"
	PrintTxnSalesReturn    = "SalesReturn"
	// Stage 85.D2 (77.15): a till sale to a buyer with a GSTIN is a tax
	// invoice - buyer, GSTIN, place of supply, HSN-wise tax - in its own
	// print format; a B2C sale keeps the receipt.
	PrintTxnPOSTaxInvoice = "POSTaxInvoice"
)

// PrintTransactions lists them with the doctype each prints from.
var PrintTransactions = []struct {
	Key, Label, Doctype string
}{
	{PrintTxnPurchaseOrder, "Purchase Order", "PurchaseOrder"},
	{PrintTxnVendorPO, "Vendor PO (copy for the vendor)", "PurchaseOrder"},
	{PrintTxnGRN, "Goods Receipt Note", "GRN"},
	{PrintTxnTransferOut, "Transfer Out (dispatch challan)", "TransferOrder"},
	{PrintTxnTransferIn, "Transfer In (receipt)", "TransferOrder"},
	{PrintTxnDebitNote, "Debit Note", "DebitNote"},
	{PrintTxnVendorCreditNote, "Credit Note (to vendor)", "VendorCreditNote"},
	{PrintTxnGRNVendorCopy, "Vendor Copy (goods receipt acknowledgement)", "GRN"},
	{PrintTxnPurchaseReturn, "Purchase Return (return to vendor)", "PurchaseReturn"},
	{PrintTxnCreditNote, "Credit Note (to customer)", "CreditNote"},
	{PrintTxnSalesReturn, "Sales Return", "ReturnRequest"},
	{PrintTxnPOSTaxInvoice, "Tax Invoice (GST bill from the till)", "POSCart"},
}

func printTxnDoctype(txn string) string {
	for _, t := range PrintTransactions {
		if t.Key == txn {
			return t.Doctype
		}
	}
	return ""
}

// PrintStore is a store block: the Location and its Legal Entity.
type PrintStore struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	LegalName string `json:"legal_name,omitempty"`
	Address   string `json:"address,omitempty"`
	City      string `json:"city,omitempty"`
	Pincode   string `json:"pincode,omitempty"`
	State     string `json:"state,omitempty"`
	StateCode string `json:"state_code,omitempty"`
	GSTIN     string `json:"gstin,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Email     string `json:"email,omitempty"`
}

// PrintTaxRow is one GST rate's share of the document - or, on a tax
// invoice, one HSN code's at one rate (HSN set).
type PrintTaxRow struct {
	HSN     string  `json:"hsn_code,omitempty"`
	GSTRate float64 `json:"gst_rate"`
	Taxable float64 `json:"taxable"`
	CGST    float64 `json:"cgst"`
	SGST    float64 `json:"sgst"`
	IGST    float64 `json:"igst"`
	Tax     float64 `json:"tax"`
}

// PrintDocument is the payload behind every designed print.
type PrintDocument struct {
	Transaction   string                   `json:"transaction"`
	Doctype       string                   `json:"doctype"`
	DocumentID    string                   `json:"document_id"`
	Title         string                   `json:"title"`
	Number        string                   `json:"number"`
	Date          string                   `json:"date,omitempty"`
	Status        string                   `json:"status"`
	Brand         map[string]string        `json:"brand"`
	Store         PrintStore               `json:"store"`
	Party         PrintStore               `json:"party"`
	PartyTitle    string                   `json:"party_title"`
	ShipTo        PrintStore               `json:"ship_to"`
	Interstate    bool                     `json:"interstate"`
	Fields        map[string]interface{}   `json:"fields"`
	Lines         []map[string]interface{} `json:"lines"`
	TaxSummary    []PrintTaxRow            `json:"tax_summary"`
	Totals        map[string]float64       `json:"totals"`
	AmountInWords string                   `json:"amount_in_words"`
	// Audit is the system audit line every print ends with (Stage 71):
	// created_by / created_at, approved_by / approved_at. Who printed it and
	// when is added by the page at print time.
	Audit map[string]string `json:"audit"`
	// For template resolution (product-wise / vendor-wise mapping).
	Vendor     string   `json:"vendor,omitempty"`
	Families   []string `json:"families,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

// LoadPrintStore resolves a Location into a store block, its state and GSTIN
// from the Location's Legal Entity.
func LoadPrintStore(tenantID, locationCode string) PrintStore {
	out := PrintStore{Code: locationCode, Name: locationCode}
	if strings.TrimSpace(locationCode) == "" {
		return out
	}
	loc, _, err := fetchDocData(tenantID, "Location", locationCode)
	if err != nil {
		return out
	}
	out.Code = firstNonEmpty(str(loc, "code"), locationCode)
	out.Name = firstNonEmpty(str(loc, "name"), out.Code)
	out.Address, out.City, out.Pincode = addressLines(loc), str(loc, "city"), str(loc, "pincode")
	out.Phone = str(loc, "contact_phone")
	if e, _, errE := fetchDocData(tenantID, "LegalEntity", str(loc, "legal_entity")); errE == nil {
		out.LegalName = str(e, "name")
		out.GSTIN = str(e, "gstin")
		code := firstNonEmpty(StateCodeFromGSTIN(out.GSTIN), StateCodeFromName(str(e, "state")))
		out.StateCode = code
		out.State = StateLabel(code)
		if out.Address == "" {
			out.Address = addressLines(e)
		}
	}
	if out.State == "Not set" {
		out.State = ""
	}
	return out
}

// loadPrintVendor is a vendor as a party block.
func loadPrintVendor(tenantID, vendorID string) PrintStore {
	out := PrintStore{Code: vendorID, Name: vendorID}
	if v, _, err := fetchDocData(tenantID, "Vendor", vendorID); err == nil {
		out.Code = firstNonEmpty(str(v, "code"), vendorID)
		out.Name = firstNonEmpty(str(v, "name"), vendorID)
		out.Address, out.City, out.Pincode = addressLines(v), str(v, "city"), str(v, "pincode")
		out.GSTIN = str(v, "gstin")
		out.StateCode = firstNonEmpty(StateCodeFromGSTIN(out.GSTIN), StateCodeFromName(str(v, "state")))
		out.State = StateLabel(out.StateCode)
		if out.State == "Not set" {
			out.State = ""
		}
		out.Email, out.Phone = str(v, "contact_email"), str(v, "contact_phone")
	}
	return out
}

// printBrandName is the brand every print carries: the print.brand_name
// setting, else the generating store's legal entity, else its name.
func printBrandName(tenantID string, store PrintStore) string {
	return firstNonEmpty(strings.TrimSpace(GetSettingString(tenantID, "print.brand_name")), store.LegalName, store.Name)
}

// printItemFields is every scalar field of an Item, for item.* on a line.
func printItemFields(tenantID, sku string, cache map[string]map[string]interface{}) map[string]interface{} {
	if f, ok := cache[sku]; ok {
		return f
	}
	out := map[string]interface{}{}
	if it, _, err := fetchDocData(tenantID, "Item", sku); err == nil {
		for k, v := range it {
			switch v.(type) {
			case string, float64, bool, int, int64:
				if s, isStr := v.(string); isStr && len(s) > 500 {
					continue
				}
				out[k] = v
			}
		}
	}
	cache[sku] = out
	return out
}

// itemThumbnailDataURL is the item's active Main Image thumbnail (else its
// first active image) as a data: URL, so the page can print it without an
// authenticated image request. "" when the item has none.
func itemThumbnailDataURL(tenantID, sku string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var mediaID string
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'ProductMedia' AND status = 'Active' AND data->>'item' = $1
		  AND COALESCE(data->>'file_type', '') LIKE 'image/%%'
		ORDER BY (data->>'media_role' = 'Main Image') DESC, created_at
		LIMIT 1`, schema), sku).Scan(&mediaID); err != nil {
		return ""
	}
	if path, err := GetMediaThumbnail(tenantID, mediaID); err == nil {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
		}
	}
	if path, fileType, err := GetMediaFile(tenantID, mediaID); err == nil {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 && len(b) <= 600*1024 {
			return "data:" + fileType + ";base64," + base64.StdEncoding.EncodeToString(b)
		}
	}
	return ""
}

// ItemImageBytes is the image the Excel export embeds: thumbnail first.
func ItemImageBytes(tenantID, sku string) ([]byte, string) {
	u := itemThumbnailDataURL(tenantID, sku)
	if u == "" {
		return nil, ""
	}
	comma := strings.Index(u, ",")
	mime := strings.TrimSuffix(strings.TrimPrefix(u[:comma], "data:"), ";base64")
	b, err := base64.StdEncoding.DecodeString(u[comma+1:])
	if err != nil {
		return nil, ""
	}
	return b, mime
}

// printLine builds one line: the tax split, the Item's fields, and its image.
type printLineIn struct {
	SKU, POID, BatchNo      string
	Qty, Rate, Taxable, GST float64
	GSTRate, MRP            float64
	Extra                   map[string]interface{}
}

func (d *PrintDocument) addLine(tenantID string, in printLineIn, cache map[string]map[string]interface{}, withImages bool) {
	item := printItemFields(tenantID, in.SKU, cache)
	cgst, sgst, igst := 0.0, 0.0, 0.0
	if d.Interstate {
		igst = round2(in.GST)
	} else {
		cgst = round2(in.GST / 2)
		sgst = round2(in.GST - cgst)
	}
	l := map[string]interface{}{
		"sr": len(d.Lines) + 1, "sku": in.SKU, "item_name": item["name"], "hsn_code": item["hsn_code"],
		"barcode": item["barcode"], "uom": item["uom"], "qty": in.Qty, "rate": round2(in.Rate),
		"mrp": in.MRP, "taxable": round2(in.Taxable), "gst_rate": in.GSTRate,
		"cgst": cgst, "sgst": sgst, "igst": igst, "tax": round2(in.GST), "amount": round2(in.Taxable + in.GST),
		"item": item,
	}
	// Stage 71: "Description / Design / Variant" - the name with the
	// variant's values (Silver / 7), and the design the item belongs to.
	variant := StickerVariantText(strField(item, "variant_option_values"))
	l["variant"], l["design"] = variant, strField(item, "parent_product_code")
	l["item_code"] = in.SKU
	if bc := strField(item, "barcode"); bc != "" && bc != in.SKU {
		l["item_code"] = in.SKU + " / " + bc
	}
	l["description"] = strField(item, "name")
	if variant != "" {
		l["description"] = strings.TrimSpace(strField(item, "name") + " · " + variant)
	}
	if in.MRP == 0 {
		l["mrp"] = numFromInterface(item["mrp"])
	}
	if in.POID != "" {
		l["po_id"] = in.POID
	}
	if in.BatchNo != "" {
		l["batch_no"] = in.BatchNo
	}
	for k, v := range in.Extra {
		l[k] = v
	}
	if withImages {
		l["image"] = itemThumbnailDataURL(tenantID, in.SKU)
	}
	if fam := strField(item, "family"); fam != "" && !containsString(d.Families, fam) {
		d.Families = append(d.Families, fam)
	}
	if cat := strField(item, "category"); cat != "" && !containsString(d.Categories, cat) {
		d.Categories = append(d.Categories, cat)
	}
	d.Lines = append(d.Lines, l)
}

// printQtyTotals are the per-line quantities some prints total (a GRN's PO /
// accepted / rejected, a transfer's shipped / received / short...), summed
// only when a line carries them.
var printQtyTotals = []string{"accepted_qty", "rejected_qty", "damaged_qty", "po_qty", "shipped_qty", "received_qty", "short_qty", "return_qty"}

// finish computes the tax summary, totals and amount in words from the lines.
func (d *PrintDocument) finish() {
	// A tax invoice sums by HSN code and rate (Rule 46(g)); every other
	// print by rate.
	byHSN := d.Transaction == PrintTxnPOSTaxInvoice
	byRate := map[string]*PrintTaxRow{}
	tot := map[string]float64{}
	for _, l := range d.Lines {
		rate := numFromInterface(l["gst_rate"])
		key, hsn := fmt.Sprintf("%08.3f", rate), ""
		if byHSN {
			hsn = strField(l, "hsn_code")
			key = hsn + "|" + key
		}
		r := byRate[key]
		if r == nil {
			r = &PrintTaxRow{GSTRate: rate, HSN: hsn}
			byRate[key] = r
		}
		r.Taxable += numFromInterface(l["taxable"])
		r.CGST += numFromInterface(l["cgst"])
		r.SGST += numFromInterface(l["sgst"])
		r.IGST += numFromInterface(l["igst"])
		r.Tax += numFromInterface(l["tax"])
		tot["qty"] += numFromInterface(l["qty"])
		for _, k := range []string{"taxable", "cgst", "sgst", "igst", "tax", "amount"} {
			tot[k] += numFromInterface(l[k])
		}
		for _, k := range printQtyTotals {
			if v, ok := l[k]; ok {
				tot[k] += numFromInterface(v)
			}
		}
	}
	rates := make([]string, 0, len(byRate))
	for k := range byRate {
		rates = append(rates, k)
	}
	sort.Strings(rates)
	d.TaxSummary = []PrintTaxRow{}
	for _, k := range rates {
		r := byRate[k]
		r.Taxable, r.CGST, r.SGST, r.IGST, r.Tax = round2(r.Taxable), round2(r.CGST), round2(r.SGST), round2(r.IGST), round2(r.Tax)
		d.TaxSummary = append(d.TaxSummary, *r)
	}
	for k, v := range tot {
		tot[k] = round2(v)
	}
	grand := tot["amount"]
	rounded := math.Round(grand)
	tot["round_off"] = round2(rounded - grand)
	tot["grand_total"] = rounded
	tot["line_count"] = float64(len(d.Lines))
	d.Totals = tot
	d.AmountInWords = AmountInWords(rounded)
}

// docHeaderFields is every scalar header field, for fields.* in a template.
func docHeaderFields(data map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range data {
		switch x := v.(type) {
		case string:
			if len(x) <= 500 && !strings.HasPrefix(strings.TrimSpace(x), "[") {
				out[k] = x
			}
		case float64, bool:
			out[k] = x
		}
	}
	return out
}

// BuildPrintDocument assembles the print data for one transaction. store
// overrides the generating store; withImages embeds each item's picture.
func BuildPrintDocument(tenantID, txn, id, store string, withImages bool) (PrintDocument, error) {
	doctype := printTxnDoctype(txn)
	if doctype == "" {
		return PrintDocument{}, &ValidationError{Code: "GLOBAL-0002", SubFor: "Transaction", Message: fmt.Sprintf("%q is not a printable transaction", txn)}
	}
	data, status, err := fetchDocData(tenantID, doctype, id)
	if err != nil {
		return PrintDocument{}, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("%s %s was not found", doctype, id)}
	}
	d := PrintDocument{Transaction: txn, Doctype: doctype, DocumentID: id, Status: firstNonEmpty(status, strField(data, "status")),
		Fields: docHeaderFields(data), Lines: []map[string]interface{}{}}
	for _, t := range PrintTransactions {
		if t.Key == txn {
			d.Title = t.Label
		}
	}
	d.Date = firstNonEmpty(strField(data, "order_date"), strField(data, "posting_date"), documentCreatedDate(tenantID, doctype, id))
	cache := map[string]map[string]interface{}{}
	defaultStore := ""

	switch txn {
	case PrintTxnPurchaseOrder, PrintTxnVendorPO:
		d.Title = "Purchase Order"
		d.Number = firstNonEmpty(strField(data, "po_number"), strField(data, "code"), id)
		defaultStore = firstNonEmpty(strField(data, "location"), strField(data, "target_warehouse"))
		d.Vendor = firstNonEmpty(strField(data, "vendor"), strField(data, "vendor_id"))
		d.Party, d.PartyTitle = loadPrintVendor(tenantID, d.Vendor), "Vendor"
		d.ShipTo = LoadPrintStore(tenantID, firstNonEmpty(strField(data, "target_warehouse"), strField(data, "location")))
		preview, err := PreviewPurchaseOrder(tenantID, data)
		if err != nil {
			return d, err
		}
		d.Interstate = preview.Breakdown.Interstate
		d.Fields["gst_mode"] = preview.GSTMode
		for _, l := range preview.Lines {
			d.addLine(tenantID, printLineIn{SKU: l.SKU, Qty: float64(l.Qty), Rate: l.Rate, Taxable: l.Taxable, GST: l.TaxAmount,
				GSTRate: l.GSTRate, MRP: l.MRP}, cache, withImages)
		}
	case PrintTxnGRN, PrintTxnGRNVendorCopy:
		d.Title = map[string]string{PrintTxnGRN: "Goods Receipt Note", PrintTxnGRNVendorCopy: "GRN Acknowledgement - Vendor Copy"}[txn]
		d.Number = firstNonEmpty(strField(data, "code"), id)
		defaultStore = strField(data, "location")
		pos := GRNPOIDs(data)
		d.Fields["po_numbers"] = strings.Join(pos, ", ")
		d.Vendor = strField(data, "vendor")
		if len(pos) > 0 {
			if po, _, errPO := fetchDocData(tenantID, "PurchaseOrder", pos[0]); errPO == nil {
				d.Vendor = firstNonEmpty(d.Vendor, str(po, "vendor"), str(po, "vendor_id"))
				d.Interstate, _ = po["interstate"].(bool)
			}
		}
		d.Party, d.PartyTitle = loadPrintVendor(tenantID, d.Vendor), "Vendor"
		ordered := poOrderedQty(tenantID, pos)
		for _, l := range grnLineMaps(data) {
			acc := grnLineAcceptedQty(l)
			rate := numFromInterface(l["invoice_rate"])
			if !lineNumberSet(l, "invoice_rate") {
				rate = numFromInterface(l["po_rate"])
			}
			gstRate := numFromInterface(l["gst_rate"])
			taxable := round2(rate * acc)
			sku, poID := strField(l, "sku"), strField(l, "po_id")
			rejected, damaged := numFromInterface(l["rejected_qty"]), numFromInterface(l["damaged_qty"])
			poQty, okPO := ordered[poID+"|"+sku]
			if !okPO {
				poQty = ordered["|"+sku]
			}
			// QC status is what the receipt split says: nothing kept back,
			// part kept back, or nothing accepted.
			qc := "Accepted"
			if rejected+damaged > 0 {
				qc = map[bool]string{true: "Rejected", false: "Partly accepted"}[acc <= 0]
			}
			reasons := []string{}
			for _, k := range []string{"rejection_reason", "damage_reason"} {
				if s := strings.TrimSpace(strField(l, k)); s != "" {
					reasons = append(reasons, s)
				}
			}
			d.addLine(tenantID, printLineIn{SKU: sku, POID: poID, BatchNo: strField(l, "batch_no"),
				Qty: numFromInterface(l["qty"]), Rate: rate, Taxable: taxable, GST: round2(taxable * gstRate / 100), GSTRate: gstRate,
				Extra: map[string]interface{}{
					"accepted_qty": acc, "rejected_qty": rejected, "damaged_qty": damaged, "po_qty": poQty,
					"qc_status": qc, "variance_reason": strings.Join(reasons, "; "),
					"po_rate": numFromInterface(l["po_rate"]), "invoice_rate": rate,
					"rate_difference": round2(rate - numFromInterface(l["po_rate"])),
				}}, cache, withImages)
		}
	case PrintTxnTransferOut, PrintTxnTransferIn:
		d.Number = firstNonEmpty(strField(data, "transfer_number"), strField(data, "code"), id)
		from, to := strField(data, "from_warehouse"), strField(data, "to_warehouse")
		if txn == PrintTxnTransferOut {
			d.Title, defaultStore = "Stock Transfer - Dispatch", from
			d.Party, d.PartyTitle = LoadPrintStore(tenantID, to), "Transfer to"
		} else {
			d.Title, defaultStore = "Stock Transfer - Receipt", to
			d.Party, d.PartyTitle = LoadPrintStore(tenantID, from), "Transfer from"
		}
		fromStore, toStore := LoadPrintStore(tenantID, from), LoadPrintStore(tenantID, to)
		// A transfer between two registrations is a taxable supply (IGST
		// across states); within one GSTIN it is a delivery challan, no tax.
		taxable := fromStore.GSTIN != "" && toStore.GSTIN != "" && fromStore.GSTIN != toStore.GSTIN
		d.Interstate = taxable && fromStore.StateCode != toStore.StateCode
		d.Fields["supply_type"] = map[bool]string{true: "Taxable stock transfer", false: "Delivery challan - stock transfer within one GSTIN, not a sale"}[taxable]
		d.Fields["value_basis"] = "Standard cost (MRP where no cost)"
		d.Fields["stock_status_after"] = map[string]string{PrintTxnTransferOut: "In transit", PrintTxnTransferIn: "Available at " + firstNonEmpty(toStore.Name, to)}[txn]
		// Stage 71: requested is the order's line, shipped what dispatch
		// recorded; a receipt prints against what was shipped. (The receipt
		// used to print its own received qty as "Sent", so a short line
		// looked complete.)
		requested, shipped := transferQtyBySKU(strField(data, "items")), transferQtyBySKU(strField(data, "items"))
		if s := strField(data, "dispatched_items"); s != "" {
			shipped = transferQtyBySKU(s)
		}
		raw := strField(data, "items")
		received := map[string]map[string]interface{}{}
		if txn == PrintTxnTransferIn && strField(data, "received_items") != "" {
			var rec []map[string]interface{}
			_ = json.Unmarshal([]byte(strField(data, "received_items")), &rec)
			for _, r := range rec {
				received[strField(r, "sku")] = r
			}
		}
		var lines []map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &lines)
		anyShort := false
		seen := map[string]bool{}
		for _, l := range lines {
			sku := strField(l, "sku")
			if seen[sku] { // quantities are per SKU; a repeated SKU prints once
				continue
			}
			seen[sku] = true
			qty := shipped[sku]
			extra := map[string]interface{}{"requested_qty": requested[sku], "shipped_qty": shipped[sku], "sent_qty": shipped[sku]}
			if txn == PrintTxnTransferIn {
				if r, ok := received[sku]; ok {
					qty = numFromInterface(r["qty"])
					if lineNumberSet(r, "received_qty") {
						qty = numFromInterface(r["received_qty"])
					}
					extra["reason"] = strField(r, "reason")
				} else if len(received) > 0 {
					qty = 0
				}
				short := math.Max(shipped[sku]-qty, 0)
				extra["received_qty"], extra["short_qty"] = qty, short
				anyShort = anyShort || short > 0
			}
			item := printItemFields(tenantID, sku, cache)
			rate := firstPositive(numFromInterface(item["standard_cost"]), numFromInterface(item["mrp"]))
			gstRate := numFromInterface(item["gst_rate"])
			val := round2(rate * qty)
			gst := 0.0
			if taxable {
				gst = round2(val * gstRate / 100)
			} else {
				gstRate = 0
			}
			d.addLine(tenantID, printLineIn{SKU: sku, Qty: qty, Rate: rate, Taxable: val, GST: gst, GSTRate: gstRate, Extra: extra}, cache, withImages)
		}
		if txn == PrintTxnTransferIn {
			d.Fields["receipt_status"] = map[bool]string{true: "Received with shortage", false: "Received in full"}[anyShort]
			if len(received) == 0 {
				d.Fields["receipt_status"] = "Not received yet"
			}
		}
	case PrintTxnDebitNote, PrintTxnVendorCreditNote:
		d.Title = map[string]string{PrintTxnDebitNote: "Debit Note", PrintTxnVendorCreditNote: "Credit Note"}[txn]
		d.Number = firstNonEmpty(strField(data, "note_number"), strField(data, "code"), id)
		d.Vendor = strField(data, "vendor_id")
		d.Party, d.PartyTitle = loadPrintVendor(tenantID, d.Vendor), "Vendor"
		grnID := firstNonEmpty(strField(data, "grn_id"), strField(data, "source_doc_id"))
		d.Fields["document_direction"] = "Vendor"
		d.Fields["place_of_supply"] = d.Party.State
		if grn, _, errG := fetchDocData(tenantID, "GRN", grnID); errG == nil && grnID != "" {
			defaultStore = strField(grn, "location")
			d.Fields["po_numbers"] = strings.Join(GRNPOIDs(grn), ", ")
			// The original invoice the note adjusts is the vendor's bill on
			// the GRN.
			d.Fields["original_invoice_no"] = firstNonEmpty(strField(data, "vendor_invoice_no"), strField(grn, "vendor_invoice_no"))
			d.Fields["original_invoice_date"] = strField(grn, "vendor_invoice_date")
			if po, _, errPO := fetchDocData(tenantID, "PurchaseOrder", strField(grn, "po_id")); errPO == nil {
				d.Interstate, _ = po["interstate"].(bool)
			}
		} else if po, _, errPO := fetchDocData(tenantID, "PurchaseOrder", strField(data, "reference_po")); errPO == nil {
			defaultStore = str(po, "location")
			d.Interstate, _ = po["interstate"].(bool)
		}
		var lines []GRNRateDifferenceLine
		_ = json.Unmarshal([]byte(strField(data, "note_lines")), &lines)
		for _, l := range lines {
			d.addLine(tenantID, printLineIn{SKU: l.SKU, POID: l.POID, Qty: l.Qty, Rate: round2(math.Abs(l.InvoiceRate - l.PORate)),
				Taxable: l.Taxable, GST: l.GST, GSTRate: l.GSTRate,
				Extra: map[string]interface{}{"po_rate": l.PORate, "invoice_rate": l.InvoiceRate,
					"original_taxable": round2(l.Qty * l.PORate), "reason": strField(data, "reason")}}, cache, withImages)
		}
		if len(lines) == 0 {
			amount, gst := numFromInterface(data["amount"]), numFromInterface(data["gst_amount"])
			gstRate := 0.0
			if amount > 0 {
				gstRate = round2(gst / amount * 100)
			}
			d.addLine(tenantID, printLineIn{Qty: 1, Rate: amount, Taxable: amount, GST: gst, GSTRate: gstRate,
				Extra: map[string]interface{}{"item_name": strField(data, "reason"), "description": strField(data, "reason")}}, cache, false)
		}
	case PrintTxnPurchaseReturn:
		d.Title = "Purchase Return - Return to Vendor"
		d.Number = firstNonEmpty(strField(data, "code"), id)
		d.Date = firstNonEmpty(strField(data, "return_date"), d.Date)
		defaultStore = strField(data, "location")
		d.Vendor = strField(data, "vendor_id")
		d.Party, d.PartyTitle = loadPrintVendor(tenantID, d.Vendor), "Vendor"
		received := map[string]float64{}
		grnRates := map[string]float64{}
		if grn, _, errG := fetchDocData(tenantID, "GRN", strField(data, "grn_id")); errG == nil {
			d.Fields["vendor_invoice_no"], d.Fields["vendor_invoice_date"] = strField(grn, "vendor_invoice_no"), strField(grn, "vendor_invoice_date")
			d.Fields["po_numbers"] = strings.Join(GRNPOIDs(grn), ", ")
			for _, l := range grnLineMaps(grn) {
				received[strField(l, "sku")] += numFromInterface(l["qty"])
				if lineNumberSet(l, "gst_rate") {
					grnRates[strField(l, "sku")] = numFromInterface(l["gst_rate"])
				}
			}
		}
		if po, _, errPO := fetchDocData(tenantID, "PurchaseOrder", strField(data, "po_id")); errPO == nil {
			d.Interstate, _ = po["interstate"].(bool)
		}
		var lines []PurchaseReturnLine
		_ = json.Unmarshal([]byte(strField(data, "return_items")), &lines)
		for _, l := range lines {
			gstRate, ok := grnRates[l.SKU]
			if !ok {
				gstRate = numFromInterface(printItemFields(tenantID, l.SKU, cache)["gst_rate"])
			}
			value := firstPositive(l.LineValue, round2(l.UnitCost*l.Qty))
			d.addLine(tenantID, printLineIn{SKU: l.SKU, BatchNo: l.BatchNo, Qty: l.Qty, Rate: l.UnitCost, Taxable: value,
				GST: round2(value * gstRate / 100), GSTRate: gstRate,
				Extra: map[string]interface{}{"return_qty": l.Qty, "received_qty": received[l.SKU], "stock_bucket": l.StockBucket,
					"reason": firstNonEmpty(strField(data, "reason"), l.StockBucket)}}, cache, withImages)
		}
	case PrintTxnCreditNote:
		d.Title = "Credit Note"
		d.Number = firstNonEmpty(strField(data, "note_number"), strField(data, "code"), id)
		d.Party, d.PartyTitle = loadPrintCustomer(tenantID, strField(data, "customer_id")), "Customer"
		d.Fields["document_direction"] = "Customer"
		if inv, _, errI := fetchDocData(tenantID, "SalesInvoice", strField(data, "sales_invoice_id")); errI == nil {
			d.Fields["original_invoice_no"] = firstNonEmpty(strField(inv, "invoice_number"), strField(data, "sales_invoice_id"))
			d.Fields["original_invoice_date"] = firstNonEmpty(strField(inv, "invoice_date"), strField(inv, "posting_date"))
			defaultStore = strField(inv, "location")
		}
		if cart, _, errC := fetchDocData(tenantID, "POSCart", strField(data, "reference_cart")); errC == nil && defaultStore == "" {
			defaultStore = firstNonEmpty(strField(cart, "location"), strField(cart, "store"))
		}
		// The note stores one amount and no tax split - it prints as one line.
		amount := numFromInterface(data["amount"])
		d.addLine(tenantID, printLineIn{Qty: 1, Rate: amount, Taxable: amount,
			Extra: map[string]interface{}{"item_name": strField(data, "reason"), "description": strField(data, "reason")}}, cache, false)
	case PrintTxnPOSTaxInvoice:
		d.Title = "Tax Invoice"
		if status != "Paid" {
			return d, &ValidationError{Code: "GLOBAL-0019", Message: fmt.Sprintf("sale %s is %s, not Paid - only a completed sale has a tax invoice", id, strings.ToLower(status))}
		}
		d.Number = firstNonEmpty(strField(data, "bill_number"), id)
		defaultStore = strField(data, "location")
		d.Party, d.PartyTitle = loadPrintCustomer(tenantID, strField(data, "customer_id")), "Buyer"
		if gb, ok := data["gst_breakdown"].(map[string]interface{}); ok {
			d.Interstate, _ = gb["interstate"].(bool)
		}
		d.Fields["supply_type"] = map[bool]string{true: "B2B", false: "B2C"}[d.Party.GSTIN != ""]
		d.Fields["buyer_gstin"] = d.Party.GSTIN
		// Place of supply is set below, from the store: goods handed over at
		// the counter are supplied where the store is (IGST Act s.10(1)).
		d.Fields["reverse_charge"] = "No"
		d.Fields["payment_mode"] = strField(data, "payment_mode")
		d.Fields["cart_number"] = id
		var lines []map[string]interface{}
		if raw, ok := data["items"].([]interface{}); ok {
			for _, r := range raw {
				if m, ok := r.(map[string]interface{}); ok {
					lines = append(lines, m)
				}
			}
		}
		for _, l := range lines {
			sku := strField(l, "sku")
			qty := numFromInterface(l["qty"])
			unit := numFromInterface(l["sale_price"])
			// The till's prices include GST: the line's net (after offers)
			// is split back into taxable value and tax at the item's rate.
			value := numFromInterface(l["net_total"])
			if value == 0 {
				value = round2(unit * qty)
			}
			gstRate := numFromInterface(printItemFields(tenantID, sku, cache)["gst_rate"])
			taxable := round2(value / (1 + gstRate/100))
			d.addLine(tenantID, printLineIn{SKU: sku, Qty: qty, Rate: unit, Taxable: taxable, GST: round2(value - taxable), GSTRate: gstRate,
				Extra: map[string]interface{}{"discount": round2(numFromInterface(l["offer_share"]))}}, cache, withImages)
		}
	case PrintTxnSalesReturn:
		d.Title = "Sales Return"
		d.Number = firstNonEmpty(strField(data, "code"), id)
		defaultStore = strField(data, "return_location")
		d.Fields["original_order"] = strField(data, "original_order_id")
		d.Fields["return_type"] = strField(data, "request_type")
		originalQty := map[string]float64{}
		if cart, _, errC := fetchDocData(tenantID, "POSCart", strField(data, "original_order_id")); errC == nil {
			d.Party = loadPrintCustomer(tenantID, firstNonEmpty(strField(cart, "customer_id"), strField(cart, "customer")))
			if d.Party.Name == "" {
				d.Party.Name, d.Party.Phone = strField(cart, "customer_name"), strField(cart, "customer_phone")
			}
			d.Fields["original_invoice_date"] = firstNonEmpty(strField(cart, "completed_at"), documentCreatedDate(tenantID, "POSCart", strField(data, "original_order_id")))
			for k, v := range transferQtyBySKU(strField(cart, "items")) {
				originalQty[k] = v
			}
		}
		d.PartyTitle = "Customer"
		if refund := returnRefund(tenantID, id); refund != nil {
			d.Fields["refund_method"], d.Fields["refund_no"] = strField(refund, "refund_method"), firstNonEmpty(strField(refund, "code"), "")
			d.Fields["refund_status"] = strField(refund, "status")
		}
		var lines []returnItemRecord
		_ = json.Unmarshal([]byte(strField(data, "items")), &lines)
		for _, l := range lines {
			// Shop prices include GST: the refund value is split back into
			// taxable and tax at the item's rate.
			item := printItemFields(tenantID, l.SKU, cache)
			gstRate := numFromInterface(item["gst_rate"])
			value := round2(l.OriginalUnitPrice * float64(l.Qty))
			taxable := round2(value / (1 + gstRate/100))
			d.addLine(tenantID, printLineIn{SKU: l.SKU, Qty: float64(l.Qty), Rate: l.OriginalUnitPrice, Taxable: taxable,
				GST: round2(value - taxable), GSTRate: gstRate,
				Extra: map[string]interface{}{"return_qty": float64(l.Qty), "original_qty": originalQty[l.SKU],
					"original_disc": round2(math.Max(numFromInterface(item["mrp"])-l.OriginalUnitPrice, 0)),
					"condition":     l.Disposition, "exchange_sku": l.ExchangeSKU}}, cache, withImages)
		}
	}

	d.Store = LoadPrintStore(tenantID, firstNonEmpty(strings.TrimSpace(store), defaultStore))
	if txn == PrintTxnPOSTaxInvoice {
		pos := strings.TrimSpace(d.Store.State)
		if pos != "" && d.Store.StateCode != "" {
			pos += " (" + d.Store.StateCode + ")"
		}
		d.Fields["place_of_supply"] = pos
	}
	d.Brand = map[string]string{"name": printBrandName(tenantID, d.Store)}
	d.finish()
	// Stage 71: the common header and audit fields every print carries.
	d.Fields["financial_year"] = financialYear(d.Date)
	if strField(d.Fields, "currency") == "" {
		d.Fields["currency"] = "INR"
	}
	d.Audit = printAudit(tenantID, doctype, id)
	if txn == PrintTxnGRNVendorCopy {
		d.stripValues()
	}
	return d, nil
}

// stripValues takes every price, value and tax out of a document, so a print
// meant for the vendor (the GRN's vendor copy) cannot show internal cost or
// valuation whatever template it is printed with.
func (d *PrintDocument) stripValues() {
	money := []string{"rate", "mrp", "taxable", "gst_rate", "cgst", "sgst", "igst", "tax", "amount", "po_rate", "invoice_rate", "rate_difference"}
	for _, l := range d.Lines {
		for _, k := range money {
			delete(l, k)
		}
		if item, ok := l["item"].(map[string]interface{}); ok {
			slim := map[string]interface{}{}
			for k, v := range item {
				switch k {
				case "standard_cost", "cost_price", "sale_price", "mrp":
				default:
					slim[k] = v
				}
			}
			l["item"] = slim
		}
	}
	for k := range d.Fields {
		if strings.Contains(k, "value") || strings.Contains(k, "amount") || strings.HasPrefix(k, "invoice_") {
			delete(d.Fields, k)
		}
	}
	for _, k := range []string{"taxable", "cgst", "sgst", "igst", "tax", "amount", "round_off", "grand_total"} {
		delete(d.Totals, k)
	}
	d.TaxSummary = []PrintTaxRow{}
	d.AmountInWords = ""
}

// financialYear is the Indian financial year (April-March) a date falls in,
// "2026-27"; "" for no date.
func financialYear(date string) string {
	t, err := time.Parse("2006-01-02", firstNonEmpty(dateOnly(date), ""))
	if err != nil {
		return ""
	}
	start := t.Year()
	if t.Month() < time.April {
		start--
	}
	return fmt.Sprintf("%d-%02d", start, (start+1)%100)
}

func dateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// printAudit is who created the document and when, and who approved it
// (the last Approved entry of the maker-checker log), as user names.
func printAudit(tenantID, doctype, id string) map[string]string {
	out := map[string]string{}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out
	}
	var by, at, docID string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(created_by, ''), COALESCE(to_char(created_at, 'YYYY-MM-DD HH24:MI'), '')
		FROM %s.documents WHERE doctype = $1 AND (id = $2 OR data->>'code' = $2) LIMIT 1`, schema), doctype, id).Scan(&docID, &by, &at) == nil {
		out["created_by"], out["created_at"] = printUserName(schema, by), at
	}
	if docID == "" {
		docID = id
	}
	var actor, when string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT actor_user_id, COALESCE(to_char(created_at, 'YYYY-MM-DD HH24:MI'), '') FROM %s.approval_log
		WHERE doctype = $1 AND document_id = $2 AND action = 'Approved' ORDER BY created_at DESC, id DESC LIMIT 1`, schema), doctype, docID).Scan(&actor, &when) == nil {
		out["approved_by"], out["approved_at"] = printUserName(schema, actor), when
	}
	return out
}

// printUserName turns a stored user reference (an id or a user name) into
// the user name; anything it cannot match prints as stored.
func printUserName(schema, ref string) string {
	var name string
	if ref != "" && db.DB.QueryRow(fmt.Sprintf(`SELECT username FROM %s.users WHERE id::text = $1 OR username = $1 LIMIT 1`, schema), ref).Scan(&name) == nil {
		return name
	}
	return ref
}

// poOrderedQty is each PO line's ordered quantity, keyed "po|sku", plus
// "|sku" summed across the POs (a GRN line without a PO id).
func poOrderedQty(tenantID string, poIDs []string) map[string]float64 {
	out := map[string]float64{}
	for _, poID := range poIDs {
		po, _, err := fetchDocData(tenantID, "PurchaseOrder", poID)
		if err != nil {
			continue
		}
		for sku, q := range transferQtyBySKU(strField(po, "items")) {
			out[poID+"|"+sku] += q
			out["|"+sku] += q
		}
	}
	return out
}

// transferQtyBySKU sums a JSON line list's qty per SKU ([{sku, qty}, ...] -
// the shape PO, transfer and cart lines share).
func transferQtyBySKU(raw string) map[string]float64 {
	out := map[string]float64{}
	var lines []map[string]interface{}
	if json.Unmarshal([]byte(raw), &lines) != nil {
		return out
	}
	for _, l := range lines {
		out[strField(l, "sku")] += numFromInterface(l["qty"])
	}
	return out
}

// loadPrintCustomer is a customer as a party block.
func loadPrintCustomer(tenantID, customerID string) PrintStore {
	out := PrintStore{Code: customerID, Name: customerID}
	if c, _, err := fetchDocData(tenantID, "Customer", customerID); err == nil {
		out.Name = firstNonEmpty(str(c, "name"), customerID)
		out.Phone, out.Email = str(c, "phone"), str(c, "email")
		// Stage 85.C1: the customer's address block and GSTIN, for a tax
		// invoice's buyer details. A walk-in's placeholder number never prints.
		out.Address = strings.TrimSpace(strings.Join(nonEmpty(str(c, "address"), str(c, "address_line2")), ", "))
		out.City, out.Pincode, out.State, out.StateCode = str(c, "city"), str(c, "pincode"), str(c, "state"), str(c, "state_code")
		out.GSTIN = str(c, "gstin")
		if str(c, "walk_in") == "Yes" {
			out.Phone = ""
		}
	}
	return out
}

func nonEmpty(vals ...string) []string {
	out := []string{}
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

// returnRefund is the RefundRequest raised for a return, nil when none.
func returnRefund(tenantID, returnID string) map[string]interface{} {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	var raw []byte
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'RefundRequest' AND deleted_at IS NULL
		AND data->>'return_request_id' = $1 ORDER BY created_at DESC LIMIT 1`, schema), returnID).Scan(&raw) != nil {
		return nil
	}
	var m map[string]interface{}
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func firstPositive(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

// documentCreatedDate is the day a document was written (YYYY-MM-DD) - the
// printed date when the document has no date field of its own.
func documentCreatedDate(tenantID, doctype, id string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var d string
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(to_char(created_at, 'YYYY-MM-DD'), '') FROM %s.documents WHERE doctype = $1 AND (id = $2 OR data->>'code' = $2) LIMIT 1`, schema),
		doctype, id).Scan(&d)
	return d
}
