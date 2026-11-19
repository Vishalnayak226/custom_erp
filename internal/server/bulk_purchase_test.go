package server

import (
	_ "embed"
	"fmt"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// bulkPurchaseTemplate is the Stage 129.1 workbook clients were given before
// ask 237 built the template per client; files made from it still upload.
//
//go:embed bulk_purchase_template.xlsx
var bulkPurchaseTemplate []byte

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
	storeHasState(t, "HO") // 49.5.6e
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
			`DELETE FROM %[1]s.documents WHERE doctype = 'PurchaseOrder' AND data->>'upload_ref' LIKE 'BP_-` + sfx + `'`,
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
	// 237.3: the blank-label Green row fits label 1's design (same item name,
	// vendor and attribute keys), so it joins it as a combination.
	if len(skus) != 4 || po.Lines[0].Code != po.Lines[1].Code || po.Lines[0].Code == po.Lines[2].Code || po.Lines[3].Code != po.Lines[0].Code {
		t.Fatalf("designs %s %s %s %s for labels 1 1 2 and blank", po.Lines[0].Code, po.Lines[1].Code, po.Lines[2].Code, po.Lines[3].Code)
	}
	if n := countDesigns(); n != 2 {
		t.Fatalf("%d designs made, want 2", n)
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
	// 199.1: a combination is named by its item name (Category + Type); its
	// colour stays in its values.
	var comboName, comboValues string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'name', ''), COALESCE(data->>'variant_option_values', '') FROM %s.documents
		WHERE doctype = 'Item' AND (id = $1 OR data->>'code' = $1)`, schema), po.Lines[0].SKU).Scan(&comboName, &comboValues)
	if want := engines.ItemNameForSchema(schema, category, "Earring"); comboName != want || !strings.Contains(comboValues, "Red") {
		t.Fatalf("combination named %q with values %q, want the item name %q and Red in its values", comboName, comboValues, want)
	}

	// The same file again (the bills filled in later): found, nothing made.
	again := bpRunFile(t, c, file, true)
	if again.POs[0].Action != "existing" || again.POs[0].POID != po.POID || again.Totals.NewVendors != 0 || countDesigns() != 2 {
		t.Fatalf("the same file again: %+v, totals %+v, designs %d", again.POs[0], again.Totals, countDesigns())
	}
	for i, l := range again.POs[0].Lines {
		if l.SKU != po.Lines[i].SKU {
			t.Fatalf("row %d is %s now, %s before", l.Row, l.SKU, po.Lines[i].SKU)
		}
	}

	// A new row under the PO Ref already used is left out to fix (237.6), with
	// a new PO Ref in the rows-to-fix file; the PO itself is found as before.
	extra := swBook(t, "", map[string][][]string{
		"Purchase": {bpTestPurchaseHead, row(ref, "1", "Red", "2", "90", "349"), row(ref, "1", "Blue", "2", "90", "349"),
			row(ref, "2", "Red", "1", "120", "449"), row(ref, "", "Green", "1", "100", "299"), row(ref, "2", "Green", "1", "120", "449")},
	}, "Purchase")
	extraRes := bpRunFile(t, c, extra, false)
	p := extraRes.POs[0]
	if p.Action != "existing" || len(p.Lines) != 5 || p.Lines[4].Action != "error" || !strings.Contains(p.Lines[4].Message, "new PO Ref") {
		t.Fatalf("a new row under a used PO Ref: %+v / %+v", p, p.Lines[len(p.Lines)-1])
	}
	if len(extraRes.Fix) != 1 || extraRes.Fix[0].Row != 7 || extraRes.Fix[0].NewRef != ref+"-2" {
		t.Fatalf("rows to fix = %+v, want row 7 under PO Ref %s-2", extraRes.Fix, ref)
	}

	// Next month: the label "1" again finds the design whose combination the
	// row already is (237.3); Wholeops' own number reorders the design it
	// names - its colour again, and a new colour.
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
	if l := labelAgain.Lines[0]; l.Code != po.Lines[0].Code || l.Action != "existing" || l.SKU != po.Lines[0].SKU {
		t.Fatalf("label 1 next month: %+v, want last month's Red combination %s of design %s", l, po.Lines[0].SKU, po.Lines[0].Code)
	}
	if reorder.Lines[0].SKU != po.Lines[0].SKU || reorder.Lines[0].Action != "existing" ||
		reorder.Lines[1].Code != po.Lines[0].Code || reorder.Lines[1].Action != "new combination" || skus[reorder.Lines[1].SKU] {
		t.Fatalf("reorder by design number: %+v / %+v", reorder.Lines[0], reorder.Lines[1])
	}
	if n := countDesigns(); n != 2 {
		t.Fatalf("%d designs after next month's file, want 2 (nothing new: the rows fit the designs there)", n)
	}

	// 237.5: the reorder template - a Combination ID, or a design with one
	// combination. What does not exist is left out to fix; a blank vendor,
	// rate and MRP come from the design.
	reHead := []string{"PO Ref *", "Vendor", "Store *", "Design No", "Combination ID", "Qty *", "Purchase Rate (₹)", "MRP (₹)"}
	reFile := swBook(t, "", map[string][][]string{"Purchase": {reHead,
		{"BPO-" + sfx, vendor, "HO", "", po.Lines[0].SKU, "2", "", ""},
		{"BPO-" + sfx, vendor, "HO", "", "NOPE-" + sfx, "1", "", ""},
		{"BPO-" + sfx, vendor, "HO", po.Lines[0].Code, "", "1", "", ""},
		{"BPQ-" + sfx, "", "HO", "", po.Lines[1].SKU, "1", "", ""},
	}}, "Purchase")
	re := bpRunFile(t, c, reFile, false)
	if len(re.POs) != 2 {
		t.Fatalf("reorder file made %d POs, want 2: %+v", len(re.POs), re.POs)
	}
	bpo, bpq := re.POs[0], re.POs[1]
	if bpo.Action != "new" || bpo.Qty != 2 || bpo.Amount != 180 || bpo.Lines[0].SKU != po.Lines[0].SKU {
		t.Fatalf("reorder PO = %+v, want the one good row (2 x 90)", bpo)
	}
	if bpq.Action != "new" || bpq.Vendor != vendor || bpq.Lines[0].Rate != 90 || bpq.Lines[0].MRP != 349 {
		t.Fatalf("reorder with a blank vendor = %+v / %+v, want the design's vendor, rate and MRP", bpq, bpq.Lines[0])
	}
	if len(re.Fix) != 2 || !strings.Contains(re.Fix[0].Reason, "does not exist") || !strings.Contains(re.Fix[1].Reason, "give the Combination ID") ||
		re.Fix[0].NewRef != "BPO-"+sfx+"-2" {
		t.Fatalf("reorder rows to fix = %+v", re.Fix)
	}
	fixed, err := bpFixBook(reFile, re.Fix, nil, engines.DateOrderDMY)
	if err != nil {
		t.Fatal(err)
	}
	_, fixLines, _, _, err := bpRead(fixed, engines.DateOrderDMY)
	if err != nil || len(fixLines) != 2 || fixLines[0].Ref != "BPO-"+sfx+"-2" || fixLines[0].Combo != "NOPE-"+sfx || !fixLines[0].reorder {
		t.Fatalf("rows-to-fix file read back as %+v (%v)", fixLines, err)
	}

	// 237.2: the PO + GRN template - every row carries its bill; one bill per
	// vendor + invoice, its total the rows'. A row with no date is left out,
	// and the bill it is on is held.
	grnHead := []string{"PO Ref *", "Vendor *", "Store *", "Design No", "Product Type *", "Category *", "HSN Code *", "GST %", "Colour",
		"Qty *", "Purchase Rate (₹) *", "MRP (₹) *", "Vendor Invoice No *", "Invoice Date *", "Received Qty", "Photo"}
	grnRow := func(colour, qty, inv, date string) []string {
		return []string{"BPG-" + sfx, vendor, "HO", "", "Earring", category, "7117", "3", colour, qty, "90", "349", inv, date, "", ""}
	}
	grn := bpRunFile(t, c, swBook(t, "", map[string][][]string{"Purchase": {grnHead,
		grnRow("Red", "1", "INV-"+sfx, "18-11-2026"), grnRow("Blue", "2", "INV-"+sfx, "18-11-2026"), grnRow("Green", "1", "INV2-"+sfx, ""),
	}}, "Purchase"), false)
	var inv1, inv2 *bpBillOut
	for _, b := range grn.Bills {
		switch b.Invoice {
		case "INV-" + sfx:
			inv1 = b
		case "INV2-" + sfx:
			inv2 = b
		}
	}
	if inv1 == nil || inv1.Action != "new" || inv1.Total != 270 || inv1.Date != "2026-11-18" {
		t.Fatalf("bill from rows = %+v", inv1)
	}
	if inv2 == nil || inv2.Action != "error" || !strings.Contains(inv2.Message, "row 5 is to fix") {
		t.Fatalf("bill with its row to fix = %+v", inv2)
	}
	if len(grn.Fix) != 1 || grn.Fix[0].Row != 5 || !strings.Contains(grn.Fix[0].Reason, "Invoice Date") {
		t.Fatalf("PO + GRN rows to fix = %+v", grn.Fix)
	}

	// 237.4: with new designs turned off, a row that needs a new combination
	// is left out; the rest of its PO goes on.
	if err := engines.SetSetting("default", bpNewDesignsSetting, "false", "test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engines.SetSetting("default", bpNewDesignsSetting, "true", "test") })
	off := bpRunFile(t, c, swBook(t, "", map[string][][]string{"Purchase": {bpTestPurchaseHead,
		row("BPS-"+sfx, "", "Red", "1", "90", "349"), row("BPS-"+sfx, "", "Purple", "1", "90", "349"),
	}}, "Purchase"), false)
	if len(off.POs) != 1 || off.POs[0].Action != "new" || off.POs[0].Qty != 1 || off.POs[0].Lines[0].SKU != po.Lines[0].SKU {
		t.Fatalf("new designs off: %+v", off.POs)
	}
	if len(off.Fix) != 1 || off.Fix[0].Row != 4 || !strings.Contains(off.Fix[0].Reason, "turned off") {
		t.Fatalf("new designs off, rows to fix = %+v", off.Fix)
	}
}

// 237.1: each template's own headers read back as that template - the
// reorder one as reorder rows, the GRN ones with a bill on every row - and the
// lists give each column its drop-down.
func TestBulkPurchaseTemplateKinds(t *testing.T) {
	ls := &bpLists{byKey: map[string]*bpList{}}
	ls.add("store", "Stores", []string{"HO"}, true)
	ls.add("vendor", "Vendors", []string{"V One"}, false)
	ls.add("state", "States", []string{"Maharashtra"}, true)
	ls.add("attr:"+bpAttrKey("Colour"), "Colour", []string{"Red", "Blue"}, false)
	ls.attrs = []engines.DesignAttribute{{Code: "COLOUR", Label: "Colour", Options: []string{"Red", "Blue"}}, {Code: "SIZE", Label: "Size"}}
	for _, k := range []bpTplKind{{false, false}, {false, true}, {true, false}, {true, true}} {
		book, err := bpTemplateBook(ls, k, engines.DateOrderDMY)
		if err != nil {
			t.Fatal(err)
		}
		sheets, err := engines.ReadXLSX(book)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, s := range sheets {
			names = append(names, s.Name)
		}
		want := "Start Here, Vendors, Purchase, Lists"
		if k.reorder {
			want = "Start Here, Purchase, Lists"
		}
		if strings.Join(names, ", ") != want {
			t.Fatalf("%s sheets = %q, want %q", k.fileName(), strings.Join(names, ", "), want)
		}
		// A row under the template's own header reads as that kind.
		heads := bpHeads(ls.purchaseCols(k, engines.DateOrderDMY))
		cells := make([]string, len(heads))
		for i, h := range heads {
			cells[i] = map[string]string{"po ref": "P1", "vendor": "V One", "store": "HO", "product type": "Earring", "category": "Antique",
				"hsn code": "7117", "colour": "Red", "size": "2.4", "qty": "1", "purchase rate": "90", "mrp": "349", "combination id": "SKU1",
				"vendor invoice no": "I1", "invoice date": "18-11-2026"}[swHeaderNorm(h)]
		}
		_, lines, _, attrs, err := bpRead(swBook(t, "", map[string][][]string{"Purchase": {heads, cells}}, "Purchase"), engines.DateOrderDMY)
		if err != nil || len(lines) != 1 {
			t.Fatalf("%s: %d rows (%v)", k.fileName(), len(lines), err)
		}
		l := lines[0]
		if l.err != "" || l.reorder != k.reorder || l.invMode != k.grn || (k.grn && l.InvDate != "2026-11-18") {
			t.Fatalf("%s row read as %+v", k.fileName(), l)
		}
		if !k.reorder && (len(attrs) != 2 || l.Attrs[bpAttrKey("Colour")] != "Red" || l.Attrs[bpAttrKey("Size")] != "2.4") {
			t.Fatalf("%s attributes = %+v / %+v", k.fileName(), attrs, l.Attrs)
		}
		// Store refuses a value off its list; Vendor warns on the new
		// template and refuses on the reorder one; Colour warns.
		got := map[string]engines.XLSXListValidation{}
		for _, v := range ls.validations("purchase", heads, 2, k.reorder) {
			got[swHeaderNorm(heads[v.Col])] = v
		}
		if v, ok := got["store"]; !ok || v.AllowOther || v.Range != "'Lists'!$A$2:$A$2" {
			t.Fatalf("%s store drop-down = %+v", k.fileName(), v)
		}
		if v, ok := got["vendor"]; !ok || v.AllowOther != !k.reorder {
			t.Fatalf("%s vendor drop-down = %+v", k.fileName(), v)
		}
		if v, ok := got["colour"]; ok == k.reorder || (ok && !v.AllowOther) {
			t.Fatalf("%s colour drop-down = %+v (present %v)", k.fileName(), v, ok)
		}
	}
}

// 237.6: the rows-to-fix file is the upload's own rows that did not load,
// with Reason, a new PO Ref where one is needed and the date in the client's
// order - and it reads back as the same rows.
func TestBulkPurchaseFixBook(t *testing.T) {
	head := []string{"PO Ref *", "Vendor *", "Store *", "Design No", "Product Type *", "Category *", "HSN Code *", "Colour",
		"Qty *", "Purchase Rate (₹) *", "MRP (₹) *", "Vendor Invoice No *", "Invoice Date *"}
	file := swBook(t, "", map[string][][]string{"Purchase": {head,
		{"P1", "V One", "HO", "", "Earring", "Antique", "7117", "Red", "1", "90", "349", "I1", "46344"},
		{"P1", "V One", "HO", "", "Earring", "Antique", "7117", "Blue", "2", "90", "349", "I1", "46344"},
		{"P2", "V One", "HO", "", "Earring", "Antique", "7117", "Pink", "3", "90", "349", "I2", "46344"},
	}}, "Purchase")
	// swBook: header on row 1, a "#" hint on row 2, data from row 3.
	fix := []bpFixRow{{Sheet: "purchase", Row: 4, Reason: "colour Blue is off", NewRef: "P1-2"}, {Sheet: "purchase", Row: 5, Reason: "bill I2 not received"}}
	book, err := bpFixBook(file, fix, nil, engines.DateOrderDMY)
	if err != nil {
		t.Fatal(err)
	}
	sheets, err := engines.ReadXLSX(book)
	if err != nil || len(sheets) != 1 || sheets[0].Name != "Purchase" {
		t.Fatalf("rows-to-fix sheets = %+v (%v)", sheets, err)
	}
	rows := sheets[0].Rows
	if len(rows) != 3 || rows[0][len(head)] != "Reason" || rows[1][0] != "P1-2" || rows[1][len(head)] != "colour Blue is off" ||
		rows[1][12] != "18-11-2026" || rows[2][0] != "P2" || rows[2][8] != "3" {
		t.Fatalf("rows-to-fix rows = %q", rows)
	}
	_, lines, _, attrs, err := bpRead(book, engines.DateOrderDMY)
	if err != nil || len(lines) != 2 || len(attrs) != 1 || lines[0].err != "" || lines[0].InvDate != "2026-11-18" || lines[1].Qty != 3 {
		t.Fatalf("rows-to-fix read back as %+v / attrs %+v (%v)", lines, attrs, err)
	}
	if _, err := bpFixBook(file, []bpFixRow{{Sheet: "bills", Row: 2}}, nil, engines.DateOrderDMY); err == nil {
		t.Fatal("a fix row on a sheet the file does not have should be refused")
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
