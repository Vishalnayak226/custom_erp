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

// PrintTaxRow is one GST rate's share of the document.
type PrintTaxRow struct {
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
	out.Address, out.City, out.Pincode = str(loc, "address"), str(loc, "city"), str(loc, "pincode")
	out.Phone = str(loc, "contact_phone")
	if e, _, errE := fetchDocData(tenantID, "LegalEntity", str(loc, "legal_entity")); errE == nil {
		out.LegalName = str(e, "name")
		out.GSTIN = str(e, "gstin")
		code := firstNonEmpty(StateCodeFromGSTIN(out.GSTIN), StateCodeFromName(str(e, "state")))
		out.StateCode = code
		out.State = StateLabel(code)
		if out.Address == "" {
			out.Address = str(e, "address")
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
		out.Address, out.City, out.Pincode = str(v, "address"), str(v, "city"), str(v, "pincode")
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
	SKU, POID, BatchNo         string
	Qty, Rate, Taxable, GST    float64
	GSTRate, MRP               float64
	Extra                      map[string]interface{}
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

// finish computes the tax summary, totals and amount in words from the lines.
func (d *PrintDocument) finish() {
	byRate := map[float64]*PrintTaxRow{}
	tot := map[string]float64{}
	for _, l := range d.Lines {
		rate := numFromInterface(l["gst_rate"])
		r := byRate[rate]
		if r == nil {
			r = &PrintTaxRow{GSTRate: rate}
			byRate[rate] = r
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
		if v, ok := l["accepted_qty"]; ok {
			tot["accepted_qty"] += numFromInterface(v)
		}
	}
	rates := make([]float64, 0, len(byRate))
	for k := range byRate {
		rates = append(rates, k)
	}
	sort.Float64s(rates)
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
	case PrintTxnGRN:
		d.Title = "Goods Receipt Note"
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
		for _, l := range grnLineMaps(data) {
			acc := grnLineAcceptedQty(l)
			rate := numFromInterface(l["invoice_rate"])
			if !lineNumberSet(l, "invoice_rate") {
				rate = numFromInterface(l["po_rate"])
			}
			gstRate := numFromInterface(l["gst_rate"])
			taxable := round2(rate * acc)
			d.addLine(tenantID, printLineIn{SKU: strField(l, "sku"), POID: strField(l, "po_id"), BatchNo: strField(l, "batch_no"),
				Qty: numFromInterface(l["qty"]), Rate: rate, Taxable: taxable, GST: round2(taxable * gstRate / 100), GSTRate: gstRate,
				Extra: map[string]interface{}{
					"accepted_qty": acc, "rejected_qty": numFromInterface(l["rejected_qty"]), "damaged_qty": numFromInterface(l["damaged_qty"]),
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
		raw := strField(data, "items")
		if txn == PrintTxnTransferIn && strField(data, "received_items") != "" {
			raw = strField(data, "received_items")
		}
		var lines []map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &lines)
		for _, l := range lines {
			sku := strField(l, "sku")
			qty := numFromInterface(l["qty"])
			if txn == PrintTxnTransferIn && lineNumberSet(l, "received_qty") {
				qty = numFromInterface(l["received_qty"])
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
			d.addLine(tenantID, printLineIn{SKU: sku, Qty: qty, Rate: rate, Taxable: val, GST: gst, GSTRate: gstRate,
				Extra: map[string]interface{}{"sent_qty": numFromInterface(l["qty"])}}, cache, withImages)
		}
	case PrintTxnDebitNote, PrintTxnVendorCreditNote:
		d.Title = map[string]string{PrintTxnDebitNote: "Debit Note", PrintTxnVendorCreditNote: "Credit Note"}[txn]
		d.Number = firstNonEmpty(strField(data, "note_number"), strField(data, "code"), id)
		d.Vendor = strField(data, "vendor_id")
		d.Party, d.PartyTitle = loadPrintVendor(tenantID, d.Vendor), "Vendor"
		grnID := firstNonEmpty(strField(data, "grn_id"), strField(data, "source_doc_id"))
		if grn, _, errG := fetchDocData(tenantID, "GRN", grnID); errG == nil && grnID != "" {
			defaultStore = strField(grn, "location")
			d.Fields["po_numbers"] = strings.Join(GRNPOIDs(grn), ", ")
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
				Extra: map[string]interface{}{"po_rate": l.PORate, "invoice_rate": l.InvoiceRate}}, cache, withImages)
		}
		if len(lines) == 0 {
			amount, gst := numFromInterface(data["amount"]), numFromInterface(data["gst_amount"])
			gstRate := 0.0
			if amount > 0 {
				gstRate = round2(gst / amount * 100)
			}
			d.addLine(tenantID, printLineIn{Qty: 1, Rate: amount, Taxable: amount, GST: gst, GSTRate: gstRate,
				Extra: map[string]interface{}{"item_name": strField(data, "reason")}}, cache, false)
		}
	}

	d.Store = LoadPrintStore(tenantID, firstNonEmpty(strings.TrimSpace(store), defaultStore))
	d.Brand = map[string]string{"name": printBrandName(tenantID, d.Store)}
	d.finish()
	return d, nil
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
