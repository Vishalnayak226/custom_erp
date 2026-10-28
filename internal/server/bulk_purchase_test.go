package server

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 129: the Bulk Purchase Upload - the template reads, rows and their
// problems read as typed, and against a real database an upload makes its
// vendor, designs and PO (routed to approval on its real amount), and the
// same file uploaded again makes nothing twice.

var bpTestPurchaseHead = []string{"Check", "PO Ref *", "Vendor *", "Store *", "Design No *", "Product Type *", "Category *",
	"HSN Code *", "GST %", "Colour", "Metal", "Qty *", "Purchase Rate (₹) *", "MRP (₹) *", "Amount (₹)",
	"Vendor Invoice No", "Received Qty", "Received Amount (₹)", "Photo"}

func bpRunFile(t *testing.T, c *swCaller, file []byte, apply bool) *bpResult {
	t.Helper()
	vendors, lines, bills, attrs, err := bpRead(file, engines.ClientDateOrder(c.tenantID))
	if err != nil {
		t.Fatal(err)
	}
	run, err := newBPRun(c, apply)
	if err != nil {
		t.Fatal(err)
	}
	res := run.process(vendors, lines, bills, attrs)
	for _, v := range res.Vendors {
		if v.Action == "error" || v.Result == "failed" {
			t.Logf("vendor row %d %s: %s", v.Row, v.Key, v.Message)
		}
	}
	for _, po := range res.POs {
		if po.Action == "error" || po.Result == "failed" {
			t.Logf("PO %s: %s", po.Ref, po.Message)
		}
	}
	return res
}

func TestBulkPurchaseTemplateReads(t *testing.T) {
	vendors, lines, bills, attrs, err := bpRead(bulkPurchaseTemplate, engines.DateOrderDMY)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 || len(bills) != 0 {
		t.Fatalf("the blank template should have no purchase rows or bills, got %d and %d", len(lines), len(bills))
	}
	if len(vendors) == 0 {
		t.Fatal("the template's sample vendors were not read")
	}
	var got []string
	for _, a := range attrs {
		got = append(got, a.Label)
	}
	if want := "Style, Colour, Polish, Size, Stone Type, Stone Colour"; strings.Join(got, ", ") != want {
		t.Fatalf("attribute columns = %q, want %q", strings.Join(got, ", "), want)
	}
}

func TestBulkPurchaseReadsRowsAsTyped(t *testing.T) {
	file := swBook(t, "", map[string][][]string{
		"Purchase": {
			bpTestPurchaseHead,
			{"OK", "P1", "V One", "HO", "D1", "Earring", "Antique", "711719 - Imitation jewellery of base metal", "3", "Red", "Brass", "3", "90", "349", "270", "INV1", "2", "180", "EI001-red.jpg; photos\\EI001-back.JPG"},
			{"", "P1", "V One", "HO", "D2", "Earring", "Antique", "7117", "3", "", "", "1.5", "90", "50", "", "", "1", ""},
			{"", "", "", "", "", "", "", "", "3", "", "", "", "", "", "", "", "", ""},
		},
		"Bills": {
			{"Check", "Vendor Invoice No *", "Vendor *", "Invoice Date *", "Invoice Total (₹) *"},
			{"OK", "INV1", "V One", "46315", "180"},
		},
	}, "Purchase", "Bills")
	_, lines, bills, attrs, err := bpRead(file, engines.DateOrderDMY)
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 2 || attrs[0].Label != "Colour" || attrs[1].Label != "Metal" {
		t.Fatalf("attribute columns = %+v, want Colour and Metal (any extra column is an attribute)", attrs)
	}
	if len(lines) != 2 {
		t.Fatalf("read %d rows, want 2 (a row of automatic columns only is not a row)", len(lines))
	}
	l := lines[0]
	if l.err != "" || l.hsn != "711719" || l.Qty != 3 || l.Rate != 90 || l.MRP != 349 || !l.HasRecv || l.Recv != 2 ||
		l.Invoice != "INV1" || l.Attrs[bpAttrKey("Colour")] != "Red" || l.Attrs[bpAttrKey("Metal")] != "Brass" {
		t.Fatalf("row 1 read as %+v", l)
	}
	if strings.Join(l.Photos, "|") != "EI001-red.jpg|EI001-back.JPG" {
		t.Fatalf("photos read as %q", l.Photos)
	}
	for _, want := range []string{"Qty must be a whole number", "MRP is below the Purchase Rate", "Received Qty needs the Vendor Invoice No"} {
		if !strings.Contains(lines[1].err, want) {
			t.Errorf("row 2 problems %q do not say %q", lines[1].err, want)
		}
	}
	if len(bills) != 1 || bills[0].Date != "2026-10-20" || bills[0].Total != 180 || bills[0].err != "" {
		t.Fatalf("bill read as %+v", bills)
	}
}

func TestBulkPurchaseMakesPOsAndNothingTwice(t *testing.T) {
	c := swTestCaller(t)
	probe, err := newSWRun(c, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := probe.resolveLink("Location", "HO"); err != nil || id == "" {
		t.Skip("the test database has no store HO")
	}
	sfx := fmt.Sprint(time.Now().UnixNano() % 1e9)
	vendor, category, ref := "BP Vendor "+sfx, "BPCat"+sfx, "BPT-"+sfx
	schema, _ := db.GetTenantSchema("default")
	vendorDesigns := `SELECT count(*) FROM %s.documents WHERE doctype = 'ProductFamily' AND data->>'vendor' IN
		(SELECT id FROM %s.documents WHERE doctype = 'Vendor' AND data->>'name' = $1)`
	countDesigns := func() int {
		var n int
		_ = db.DB.QueryRow(fmt.Sprintf(vendorDesigns, schema, schema), vendor).Scan(&n)
		return n
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM %[1]s.documents WHERE doctype = 'PurchaseOrder' AND data->>'upload_ref' LIKE 'BP__-` + sfx + `'`,
			`DELETE FROM %[1]s.documents WHERE doctype = 'Item' AND data->>'category' = '` + category + `'`,
			`DELETE FROM %[1]s.documents WHERE doctype = 'ProductFamily' AND data->>'vendor' IN (SELECT id FROM %[1]s.documents WHERE doctype = 'Vendor' AND data->>'name' = '` + vendor + `')`,
			`DELETE FROM %[1]s.documents WHERE doctype = 'ItemName' AND data->>'name' LIKE '%%` + category + `%%'`,
			`DELETE FROM %[1]s.documents WHERE doctype = 'Category' AND data->>'name' = '` + category + `'`,
			`DELETE FROM %[1]s.documents WHERE doctype = 'Vendor' AND data->>'name' = '` + vendor + `'`,
		} {
			_, _ = db.DB.Exec(fmt.Sprintf(q, schema))
		}
	})
	row := func(ref, design, colour, qty, rate, mrp string) []string {
		return []string{"", ref, vendor, "HO", design, "Earring", category, "7117 - Imitation / fashion jewellery", "3", colour, "", qty, rate, mrp, "", "", "", ""}
	}
	vendors := [][]string{{"Check", "Vendor Name *", "State *", "City", "GSTIN", "Phone", "Email"}, {"", vendor, "Maharashtra", "Mumbai", "", "", ""}}
	// The Design No is the client's own label: "1" twice = one design in two
	// colours; a blank one is a design of its own (129.7).
	file := swBook(t, "", map[string][][]string{
		"Vendors": vendors,
		"Purchase": {bpTestPurchaseHead, row(ref, "1", "Red", "2", "90", "349"), row(ref, "1", "Blue", "2", "90", "349"),
			row(ref, "2", "Red", "1", "120", "449"), row(ref, "", "Green", "1", "100", "299")},
	}, "Vendors", "Purchase")

	preview := bpRunFile(t, c, file, false)
	if len(preview.POs) != 1 || preview.POs[0].Action != "new" || preview.Totals.Errors != 0 {
		t.Fatalf("preview = %+v", preview.POs)
	}

	res := bpRunFile(t, c, file, true)
	if len(res.POs) != 1 || res.POs[0].POID == "" || res.Totals.Failed != 0 {
		t.Fatalf("apply made %+v", res.POs[0])
	}
	po := res.POs[0]
	skus := map[string]bool{}
	for _, l := range po.Lines {
		if l.SKU == "" || l.Code == "" {
			t.Fatalf("row %d has combination %q, design %q", l.Row, l.SKU, l.Code)
		}
		skus[l.SKU] = true
	}
	if len(skus) != 4 || po.Lines[0].Code != po.Lines[1].Code || po.Lines[0].Code == po.Lines[2].Code || po.Lines[3].Code == po.Lines[2].Code {
		t.Fatalf("designs %s %s %s %s for labels 1 1 2 and blank", po.Lines[0].Code, po.Lines[1].Code, po.Lines[2].Code, po.Lines[3].Code)
	}
	if n := countDesigns(); n != 3 {
		t.Fatalf("%d designs made, want 3", n)
	}
	var upload, mode, designNo string
	var base float64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'upload_ref', data->>'gst_mode', COALESCE((data->>'base_total_amount')::numeric, 0)
		FROM %s.documents WHERE id = $1`, schema), po.POID).Scan(&upload, &mode, &base); err != nil {
		t.Fatal(err)
	}
	// The upload sends no total; the approval amount must still be the PO's own.
	if upload != ref || mode != "Inclusive" || base < 555 || base > 565 {
		t.Fatalf("PO saved with upload_ref %q, gst_mode %q, base_total_amount %v - want %q, Inclusive, ~563 (580 incl. GST)", upload, mode, base, ref)
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'design_number', '') FROM %s.documents WHERE id = $1`, schema), po.Lines[0].Code).Scan(&designNo)
	if designNo != "" {
		t.Fatalf("the client's label %q was stored as the design number", designNo)
	}

	// The same file again (the bills filled in later): found, nothing made.
	again := bpRunFile(t, c, file, true)
	if again.POs[0].Action != "existing" || again.POs[0].POID != po.POID || again.Totals.NewVendors != 0 || countDesigns() != 3 {
		t.Fatalf("the same file again: %+v, totals %+v, designs %d", again.POs[0], again.Totals, countDesigns())
	}
	for i, l := range again.POs[0].Lines {
		if l.SKU != po.Lines[i].SKU {
			t.Fatalf("row %d is %s now, %s before", l.Row, l.SKU, po.Lines[i].SKU)
		}
	}

	// A new row under the PO Ref already used is refused, not added.
	extra := swBook(t, "", map[string][][]string{
		"Purchase": {bpTestPurchaseHead, row(ref, "1", "Red", "2", "90", "349"), row(ref, "1", "Blue", "2", "90", "349"),
			row(ref, "2", "Red", "1", "120", "449"), row(ref, "", "Green", "1", "100", "299"), row(ref, "2", "Green", "1", "120", "449")},
	}, "Purchase")
	if p := bpRunFile(t, c, extra, false).POs[0]; p.Action != "error" || !strings.Contains(p.Message, "new PO Ref") {
		t.Fatalf("a new row under a used PO Ref: %+v", p)
	}

	// Next month: the label "1" again is another design; Wholeops' own number
	// reorders the design it names - its colour again, and a new colour.
	next := swBook(t, "", map[string][][]string{
		"Purchase": {bpTestPurchaseHead, row("BPN-"+sfx, "1", "Red", "1", "90", "349"),
			row("BPR-"+sfx, po.Lines[0].Code, "Red", "3", "90", "349"), row("BPR-"+sfx, po.Lines[0].Code, "Yellow", "3", "90", "349")},
	}, "Purchase")
	month := bpRunFile(t, c, next, true)
	if month.Totals.Failed != 0 || month.Totals.Errors != 0 {
		t.Fatalf("next month's file: %+v", month.Totals)
	}
	var labelAgain, reorder *bpPOOut
	for _, p := range month.POs {
		if p.Ref == "BPN-"+sfx {
			labelAgain = p
		} else {
			reorder = p
		}
	}
	if labelAgain.Lines[0].Code == po.Lines[0].Code || labelAgain.Lines[0].Action != "new design" {
		t.Fatalf("label 1 next month joined last month's design %s (%s)", labelAgain.Lines[0].Code, labelAgain.Lines[0].Action)
	}
	if reorder.Lines[0].SKU != po.Lines[0].SKU || reorder.Lines[0].Action != "existing" ||
		reorder.Lines[1].Code != po.Lines[0].Code || reorder.Lines[1].Action != "new combination" || skus[reorder.Lines[1].SKU] {
		t.Fatalf("reorder by design number: %+v / %+v", reorder.Lines[0], reorder.Lines[1])
	}
	if n := countDesigns(); n != 4 {
		t.Fatalf("%d designs after next month's file, want 4 (one new for the reused label)", n)
	}
}

// The request envelope caps bodies at 2 MiB by default; a workbook of up to
// the handler's own 8 MB must reach it.
func TestBulkPurchaseUploadsPassTheEnvelope(t *testing.T) {
	for _, p := range []string{"/api/v1/procurement/bulk-upload/preview", "/api/v1/procurement/bulk-upload/apply"} {
		if got := envelopePolicyFor(p).maxBody; got < swMaxUpload {
			t.Errorf("%s: envelope allows %d bytes, the handler takes up to %d", p, got, swMaxUpload)
		}
	}
}
