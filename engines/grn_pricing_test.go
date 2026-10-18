package engines

import (
	"archive/zip"
	"bytes"
	"custom_erp/db"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestStage62GRNPricingAndNotes covers Stage 62's GRN half: one receipt
// against two POs of a vendor, the PO price read from the PO (never the
// client), stock at the invoice price, the debit / vendor credit notes for
// the difference with their GL, and the vendor invoice matching the priced
// GRN instead of holding - plus Stage 63's print data for the same receipt.
func TestStage62GRNPricingAndNotes(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		skuA, skuB = "TEST62-ITEM-A", "TEST62-ITEM-B"
		v1, v2     = "TEST62-VEND-1", "TEST62-VEND-2"
		po1, po2   = "TEST62-PO-1", "TEST62-PO-2"
		po3        = "TEST62-PO-3"
		grnID      = "TEST62-GRN"
		viID       = "TEST62-VI"
		location   = "TEST62-LOC"
	)
	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	lines := func(rows ...map[string]interface{}) string {
		raw, _ := json.Marshal(rows)
		return string(raw)
	}
	var notes []string
	cleanup := func() {
		for _, id := range append([]string{skuA, skuB, v1, v2, po1, po2, po3, grnID, viID}, notes...) {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype IN ('DebitNote','VendorCreditNote') AND data->>'source_doc_id' = $1", grnID)
		for _, sku := range []string{skuA, skuB} {
			db.DB.Exec("DELETE FROM "+schema+".item_cost WHERE item_code = $1", sku)
			db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		}
	}
	cleanup()
	defer cleanup()

	insert(skuA, "Item", "Active", map[string]interface{}{"code": skuA, "name": "Stage 62 Item A", "hsn_code": "6204", "gst_rate": 5.0, "tax_treatment": "Taxable"})
	insert(skuB, "Item", "Active", map[string]interface{}{"code": skuB, "name": "Stage 62 Item B", "hsn_code": "6204", "gst_rate": 5.0, "tax_treatment": "Taxable"})
	insert(v1, "Vendor", "Active", map[string]interface{}{"code": v1, "name": "Stage 62 Vendor One"})
	insert(v2, "Vendor", "Active", map[string]interface{}{"code": v2, "name": "Stage 62 Vendor Two"})
	insert(po1, "PurchaseOrder", "Approved", map[string]interface{}{"code": po1, "vendor": v1, "gst_mode": GSTModeExclusive, "total_amount": 4000, "grand_total": 4200,
		"items": lines(map[string]interface{}{"sku": skuA, "qty": 10, "rate": 400.0})})
	insert(po2, "PurchaseOrder", "Approved", map[string]interface{}{"code": po2, "vendor": v1, "gst_mode": GSTModeExclusive, "total_amount": 3360, "grand_total": 3528,
		"items": lines(map[string]interface{}{"sku": skuB, "qty": 8, "rate": 420.0})})
	insert(po3, "PurchaseOrder", "Approved", map[string]interface{}{"code": po3, "vendor": v2, "gst_mode": GSTModeExclusive, "total_amount": 1600, "grand_total": 1680,
		"items": lines(map[string]interface{}{"sku": skuB, "qty": 4, "rate": 400.0})})

	code := func(err error) string {
		var verr *ValidationError
		if errors.As(err, &verr) {
			return verr.Code
		}
		return ""
	}

	t.Run("POs of another vendor are refused", func(t *testing.T) {
		p := map[string]interface{}{"po_id": po1, "po_ids": po1 + "," + po3, "received_items": lines(
			map[string]interface{}{"po_id": po1, "sku": skuA, "qty": 1}, map[string]interface{}{"po_id": po3, "sku": skuB, "qty": 1})}
		if got := code(PrepareGRNPricing(tenantID, true, p)); got != "GOODSR-0318" {
			t.Fatalf("want GOODSR-0318, got %q", got)
		}
	})
	t.Run("a negative invoice rate is refused", func(t *testing.T) {
		p := map[string]interface{}{"po_id": po1, "received_items": lines(map[string]interface{}{"sku": skuA, "qty": 1, "invoice_rate": -5})}
		if got := code(PrepareGRNPricing(tenantID, true, p)); got != "GOODSR-0319" {
			t.Fatalf("want GOODSR-0319, got %q", got)
		}
	})

	payload := map[string]interface{}{
		"po_ids": po1 + "," + po2, "location": location, "vendor_invoice_amount": 7000,
		"received_items": lines(
			map[string]interface{}{"po_id": po1, "sku": skuA, "qty": 10, "accepted_qty": 9, "rejected_qty": 1, "rejection_reason": "stain", "invoice_rate": 440, "po_rate": 1},
			map[string]interface{}{"po_id": po2, "sku": skuB, "qty": 8, "accepted_qty": 8, "invoice_rate": 380},
		),
	}
	if err := PrepareGRNPricing(tenantID, true, payload); err != nil {
		t.Fatalf("PrepareGRNPricing: %v", err)
	}
	t.Run("header and line prices are stamped from the POs", func(t *testing.T) {
		if payload["po_id"] != po1 || payload["po_ids"] != po1+","+po2 || payload["vendor"] != v1 {
			t.Fatalf("header po_id/po_ids/vendor = %v / %v / %v", payload["po_id"], payload["po_ids"], payload["vendor"])
		}
		ls := grnLineMaps(payload)
		if numFromInterface(ls[0]["po_rate"]) != 400 || numFromInterface(ls[1]["po_rate"]) != 420 {
			t.Fatalf("po_rate must come from each line's own PO, ignoring the client's 1: %v / %v", ls[0]["po_rate"], ls[1]["po_rate"])
		}
		// 9 x 400 + 8 x 420 = 6960 at PO price; 9 x 440 + 8 x 380 = 7000 invoiced + 5% GST.
		if payload["po_value"] != 6960.0 || payload["invoice_value"] != 7000.0 || payload["invoice_gst"] != 350.0 || payload["invoice_total"] != 7350.0 {
			t.Fatalf("totals = %v %v %v %v", payload["po_value"], payload["invoice_value"], payload["invoice_gst"], payload["invoice_total"])
		}
		if payload["invoice_amount_difference"] != -350.0 {
			t.Fatalf("typed amount 7000 vs lines 7350 must record -350, got %v", payload["invoice_amount_difference"])
		}
	})
	t.Run("each PO is checked on its own lines", func(t *testing.T) {
		over := map[string]interface{}{"po_ids": po1 + "," + po2, "po_id": po1, "received_items": lines(
			map[string]interface{}{"po_id": po1, "sku": skuA, "qty": 1}, map[string]interface{}{"po_id": po2, "sku": skuB, "qty": 9})}
		if got := code(validateGRNRules(tenantID, "", over)); got != "PURCHA-0087" {
			t.Fatalf("9 of PO2's 8 must be refused as over-receipt, got %q", got)
		}
		wrongPO := map[string]interface{}{"po_ids": po1 + "," + po2, "po_id": po1, "received_items": lines(
			map[string]interface{}{"po_id": po2, "sku": skuA, "qty": 1})}
		if got := code(validateGRNRules(tenantID, "", wrongPO)); got != "PURCHA-0088" {
			t.Fatalf("item A is not on PO2, got %q", got)
		}
	})

	payload["code"] = grnID
	insert(grnID, "GRN", "Approved", payload)
	var items []interface{}
	_ = json.Unmarshal([]byte(payload["received_items"].(string)), &items)
	if _, err := PostGRNReceiptWithQC(tenantID, location, items, "system", grnID); err != nil {
		t.Fatalf("PostGRNReceiptWithQC: %v", err)
	}
	glSum := func(docID, account string) (dr, cr int64) {
		db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = $2", docID, account).Scan(&dr, &cr)
		return
	}
	t.Run("stock is booked at the invoice price", func(t *testing.T) {
		if dr, _ := glSum(grnID, "1200"); dr != 700000 {
			t.Fatalf("Dr 1200 = %d paise, want 700000 (7000.00 at invoice price)", dr)
		}
		if received, _ := fetchGRNReceivedQuantities(tenantID, po2, ""); received[skuB] != 8 || received[skuA] != 0 {
			t.Fatalf("PO2 must count only its own line: %v", received)
		}
	})

	res := PostGRNRateDifferenceNotes(tenantID, grnID, "system", "HQ")
	notes = append(notes, res.DebitNoteID, res.VendorCreditNoteID)
	t.Run("the price difference becomes posted notes", func(t *testing.T) {
		if res.Warning != "" || res.DebitNoteID == "" || res.VendorCreditNoteID == "" {
			t.Fatalf("notes = %+v", res)
		}
		dn, dnStatus, _ := fetchDocData(tenantID, "DebitNote", res.DebitNoteID)
		vcn, vcnStatus, _ := fetchDocData(tenantID, "VendorCreditNote", res.VendorCreditNoteID)
		// A: (440-400) x 9 = 360 + 18 GST; B: (420-380) x 8 = 320 + 16 GST.
		if dnStatus != "Posted" || dn["amount"] != 360.0 || dn["gst_amount"] != 18.0 || dn["vendor_id"] != v1 {
			t.Fatalf("debit note %s %v", dnStatus, dn)
		}
		if vcnStatus != "Posted" || vcn["amount"] != 320.0 || vcn["gst_amount"] != 16.0 {
			t.Fatalf("vendor credit note %s %v", vcnStatus, vcn)
		}
		if dr, _ := glSum(res.DebitNoteID, "2100"); dr != 37800 {
			t.Fatalf("debit note Dr 2100 = %d, want 37800", dr)
		}
		if _, cr := glSum(res.DebitNoteID, "1500"); cr != 1800 {
			t.Fatalf("debit note Cr 1500 = %d, want 1800", cr)
		}
		if _, cr := glSum(res.VendorCreditNoteID, "2100"); cr != 33600 {
			t.Fatalf("vendor credit note Cr 2100 = %d, want 33600", cr)
		}
		grn, _, _ := fetchDocData(tenantID, "GRN", grnID)
		if grn["debit_note_id"] != res.DebitNoteID || grn["vendor_credit_note_id"] != res.VendorCreditNoteID {
			t.Fatalf("GRN not linked to its notes: %v / %v", grn["debit_note_id"], grn["vendor_credit_note_id"])
		}
	})
	t.Run("raising the notes again changes nothing", func(t *testing.T) {
		again := PostGRNRateDifferenceNotes(tenantID, grnID, "system", "HQ")
		if again.DebitNoteID != res.DebitNoteID || again.VendorCreditNoteID != res.VendorCreditNoteID {
			t.Fatalf("second call raised new notes: %+v vs %+v", again, res)
		}
		if dr, _ := glSum(res.DebitNoteID, "2100"); dr != 37800 {
			t.Fatalf("second call double-posted: Dr 2100 = %d", dr)
		}
	})
	t.Run("the vendor invoice matches the priced GRN instead of holding", func(t *testing.T) {
		insert(viID, "VendorInvoice", "Draft", map[string]interface{}{"invoice_number": "INV-62", "vendor_id": v1, "po_id": po1, "grn_id": grnID, "invoice_amount": 7350, "financial_year": "2026-27"})
		m, err := MatchVendorInvoice(tenantID, viID, "", "", 0)
		if err != nil {
			t.Fatalf("MatchVendorInvoice: %v", err)
		}
		if !m.Matched || m.Status != "Matched" {
			t.Fatalf("want Matched (billed above PO1's total is settled by the debit note), got %s %v", m.Status, m.Reasons)
		}
	})
	t.Run("print data carries brand, store, party, tax split and totals", func(t *testing.T) {
		d, err := BuildPrintDocument(tenantID, PrintTxnGRN, grnID, "", false)
		if err != nil {
			t.Fatalf("BuildPrintDocument: %v", err)
		}
		if d.Brand["name"] == "" || d.Party.Name != "Stage 62 Vendor One" || len(d.Lines) != 2 || d.Fields["po_numbers"] != po1+", "+po2 {
			t.Fatalf("print header %+v %+v %d %v", d.Brand, d.Party, len(d.Lines), d.Fields["po_numbers"])
		}
		if d.Totals["taxable"] != 7000 || d.Totals["tax"] != 350 || d.Totals["grand_total"] != 7350 || len(d.TaxSummary) != 1 || d.TaxSummary[0].CGST != 175 {
			t.Fatalf("print totals %v tax %v", d.Totals, d.TaxSummary)
		}
		if d.Lines[0]["item"].(map[string]interface{})["hsn_code"] != "6204" || d.AmountInWords == "" {
			t.Fatalf("item fields / words missing: %v %q", d.Lines[0]["item"], d.AmountInWords)
		}
		n, err := BuildPrintDocument(tenantID, PrintTxnDebitNote, res.DebitNoteID, "", false)
		if err != nil || n.Totals["grand_total"] != 378 || len(n.Lines) != 1 || n.Lines[0]["po_rate"] != 400.0 {
			t.Fatalf("debit note print %v %v %v", err, n.Totals, n.Lines)
		}
		if _, err := BuildPrintDocument(tenantID, "Nope", grnID, "", false); code(err) != "GLOBAL-0002" {
			t.Fatalf("unknown transaction must be refused, got %v", err)
		}
	})
}

// TestPrintTemplateMapping is Stage 63.2's resolution rule: a mapping narrows
// which documents a template is offered for, and the most specific wins.
func TestPrintTemplateMapping(t *testing.T) {
	doc := PrintDocument{Transaction: PrintTxnGRN, Vendor: "V1", Families: []string{"Design/1"}, Categories: []string{"Kurta"}}
	cases := []struct {
		name  string
		t     map[string]interface{}
		ok    bool
		score int
	}{
		{"plain", map[string]interface{}{}, true, 0},
		{"default", map[string]interface{}{"is_default": "Yes"}, true, 1},
		{"vendor match", map[string]interface{}{"vendor": "v1"}, true, 8},
		{"other vendor", map[string]interface{}{"vendor": "V2"}, false, 0},
		{"family + category", map[string]interface{}{"item_families": "X, Design/1", "item_categories": "Kurta"}, true, 6},
		{"other family", map[string]interface{}{"item_families": "Design/9"}, false, 0},
	}
	for _, c := range cases {
		score, _, ok := scorePrintTemplate(c.t, doc)
		if ok != c.ok || (ok && score != c.score) {
			t.Errorf("%s: ok=%v score=%d, want ok=%v score=%d", c.name, ok, score, c.ok, c.score)
		}
	}
}

// TestWriteXLSX checks the hand-written workbook is a well-formed package:
// every part parses as XML, cells and the picture are where they were put.
func TestWriteXLSX(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nfake")
	out, err := WriteXLSX(XLSXSheet{Name: "PO 1/2", ColWidths: []float64{5, 10},
		Rows:       [][]XLSXCell{{{Text: "A & <b>", Bold: true}, {Num: 1234.5, IsNum: true}}},
		RowHeights: map[int]float64{0: 60},
		Images:     []XLSXImage{{Row: 0, Col: 1, Data: png, Mime: "image/png"}}})
	if err != nil {
		t.Fatalf("WriteXLSX: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(b)
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
			dec := xml.NewDecoder(bytes.NewReader(b))
			for {
				if _, err := dec.Token(); err != nil {
					if err != io.EOF {
						t.Fatalf("%s is not well-formed XML: %v", f.Name, err)
					}
					break
				}
			}
		}
	}
	for _, want := range []string{"[Content_Types].xml", "xl/workbook.xml", "xl/worksheets/sheet1.xml", "xl/drawings/drawing1.xml", "xl/drawings/_rels/drawing1.xml.rels", "xl/media/image1.png"} {
		if _, ok := parts[want]; !ok {
			t.Fatalf("missing part %s", want)
		}
	}
	sheet := parts["xl/worksheets/sheet1.xml"]
	if !strings.Contains(sheet, "A &amp; &lt;b&gt;") || !strings.Contains(sheet, "<v>1234.5</v>") || !strings.Contains(sheet, `<drawing r:id="rId1"/>`) {
		t.Fatalf("sheet content wrong: %s", sheet)
	}
	if !strings.Contains(parts["xl/workbook.xml"], `name="PO 1-2"`) {
		t.Fatalf("sheet name not sanitised: %s", parts["xl/workbook.xml"])
	}
	if xlsxColName(0) != "A" || xlsxColName(25) != "Z" || xlsxColName(26) != "AA" {
		t.Fatalf("column names wrong")
	}
}

// TestStage62Designs covers the design half: the design matrix, design
// numbering, matching an existing design, and the duplicate-combination
// guard that stops a create silently overwriting an existing Item.
func TestStage62Designs(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const attr, design = "TEST62-COLOUR", "TEST62-DESIGN"
	variant := design + "-Red"
	cleanup := func() {
		for _, id := range []string{attr, design, variant} {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	preserveSeries(t, schema, "ProductFamily") // Stage 76.5: a design's first number confirms its series
	ins := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	ins(attr, "ItemAttribute", map[string]interface{}{"code": attr, "label": "Test Colour", "value_type": "Select", "value_options": "Red, Blue ,", "status": "Active"})
	ins(design, "ProductFamily", map[string]interface{}{"code": design, "name": "Stage 62 Test Kurta", "design_attributes": "Fabric:Cotton;Fit:Slim", "hsn_code": "6204", "gst_rate": 5})
	ins(variant, "Item", map[string]interface{}{"code": variant, "name": "Kurta Red", "family": design, "parent_product_code": design, "variant_option_values": attr + ":Red", "hsn_code": "6204", "gst_rate": 5})

	m, err := GetDesignMatrix(tenantID)
	if err != nil {
		t.Fatalf("GetDesignMatrix: %v", err)
	}
	found := false
	for _, a := range m.Attributes {
		if a.Code == attr {
			found = true
			if strings.Join(a.Options, "|") != "Red|Blue" {
				t.Fatalf("options not trimmed: %q", a.Options)
			}
		}
	}
	if !found || (m.Mode != DesignNumberModeNew && m.Mode != DesignNumberModeMatch) {
		t.Fatalf("matrix missing attribute or mode: %v %q", found, m.Mode)
	}
	if got, _ := FindMatchingDesign(tenantID, "  stage 62 TEST kurta ", map[string]string{"fit": "slim", "Fabric": "COTTON"}); got != design {
		t.Fatalf("same name and attributes in any order/case must match, got %q", got)
	}
	if got, _ := FindMatchingDesign(tenantID, "Stage 62 Test Kurta", map[string]string{"Fabric": "Linen", "Fit": "Slim"}); got != "" {
		t.Fatalf("different attribute must not match, got %q", got)
	}
	d, err := GetDesign(tenantID, design)
	if err != nil || len(d.Combinations) != 1 || d.Combinations[0].Options[attr] != "Red" || d.HSNCode != "6204" {
		t.Fatalf("GetDesign: %v %+v", err, d)
	}
	dup := map[string]interface{}{"family": design, "parent_product_code": design, "variant_option_values": attr + ":Red"}
	if err := PrepareItemVariantCode(tenantID, true, dup); err == nil {
		t.Fatalf("re-creating an existing combination must be refused, got code %v", dup["code"])
	}
	fresh := map[string]interface{}{"family": design, "parent_product_code": design, "variant_option_values": attr + ":Blue"}
	if err := PrepareItemVariantCode(tenantID, true, fresh); err != nil || fresh["code"] != design+"-Blue" {
		t.Fatalf("new combination: %v %v", err, fresh["code"])
	}
	typed := map[string]interface{}{"code": "MY-DESIGN"}
	if err := PrepareDesignNumber(tenantID, "HQ", true, typed); err != nil || typed["code"] != "MY-DESIGN" {
		t.Fatalf("a typed design code must be kept: %v %v", err, typed["code"])
	}
	blank := map[string]interface{}{"name": "x"}
	if err := PrepareDesignNumber(tenantID, "HQ", true, blank); err != nil || !shortMasterCode.MatchString(strField(blank, "code")) || blank["id"] != blank["code"] {
		t.Fatalf("a blank design must get a short code from its series (Stage 76.5, e.g. D00001): %v %v", err, blank)
	}
}
