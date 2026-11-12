package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 148.35: buy in packs, stock in pieces. Each test names its own items,
// documents and location (mc1481 helpers), so none depends on another.

func packOn(t *testing.T, on bool) {
	db.InitDB(testConnStr())
	t.Helper()
	v := "false"
	if on {
		v = "true"
	}
	if err := SetSetting("default", SettingPackUnits, v, "test"); err != nil {
		t.Fatalf("setting: %v", err)
	}
	t.Cleanup(func() { _ = SetSetting("default", SettingPackUnits, "false", "test") })
}

func packItem(m *mc1481, extra map[string]interface{}) string {
	d := map[string]interface{}{"uom": "pc", "purchase_unit": "Pack12", "purchase_unit_factor": 12.0}
	for k, v := range extra {
		d[k] = v
	}
	return m.item(d)
}

func packLines(t *testing.T, raw interface{}) []map[string]interface{} {
	t.Helper()
	s, _ := raw.(string)
	var out []map[string]interface{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("lines: %v (%q)", err, s)
	}
	return out
}

// packPO stamps a PO keyed in packs and stores it as an approved PO.
func packPO(t *testing.T, m *mc1481, lines []map[string]interface{}) (poID string, stamped []map[string]interface{}) {
	t.Helper()
	raw, _ := json.Marshal(lines)
	payload := map[string]interface{}{"items": string(raw), "vendor": "V", "gst_mode": GSTModeExclusive}
	if err := StampPOPricing(m.tenant, payload); err != nil {
		t.Fatalf("StampPOPricing: %v", err)
	}
	stamped = packLines(t, payload["items"])
	poID = m.id("PO")
	m.doc(poID, "PurchaseOrder", "Approved", map[string]interface{}{"code": poID, "gst_mode": GSTModeExclusive, "items": payload["items"]})
	return poID, stamped
}

// packGRN prices a GRN keyed in packs the way the save does and stores it.
func packGRN(t *testing.T, m *mc1481, poID string, lines []map[string]interface{}) (grnID string, prepared []map[string]interface{}, err error) {
	t.Helper()
	raw, _ := json.Marshal(lines)
	payload := map[string]interface{}{"po_id": poID, "location": m.loc, "received_items": string(raw)}
	if err = PrepareGRNPacks(m.tenant, payload); err != nil {
		return "", nil, err
	}
	if err = PrepareGRNPricing(m.tenant, true, payload); err != nil {
		return "", nil, err
	}
	prepared = packLines(t, payload["received_items"])
	grnID = m.id("GRN")
	m.doc(grnID, "GRN", "Received", map[string]interface{}{"code": grnID, "po_id": poID, "location": m.loc, "received_items": payload["received_items"]})
	return grnID, prepared, nil
}

func packPost(t *testing.T, m *mc1481, grnID string, prepared []map[string]interface{}) {
	t.Helper()
	items := make([]interface{}, 0, len(prepared))
	for _, l := range prepared {
		items = append(items, l)
	}
	if _, err := PostGRNReceiptWithQC(m.tenant, m.loc, items, "system", grnID); err != nil {
		t.Fatalf("post GRN: %v", err)
	}
}

// The headline: 5 packs of 12 at Rs 120 a pack. Stock +60, Rs 10 a piece, the
// journal Rs 600, the bill of Rs 600 matches, then freight, the second buy's
// average and a return of one pack (12 pieces) all read pieces.
func TestStage14835PackOfTwelveStockCostJournalAndBill(t *testing.T) {
	packOn(t, true)
	m := newMC1481(t)
	sku := packItem(m, nil)

	po, stamped := packPO(t, m, []map[string]interface{}{{"sku": sku, "pack_qty": 5, "pack_rate": 120}})
	if stamped[0]["qty"] != 60.0 || stamped[0]["rate"] != 10.0 || stamped[0]["pack_factor"] != 12.0 || stamped[0]["pack_unit"] != "Pack12" {
		t.Fatalf("PO line %+v, want 60 pieces at Rs 10 with factor 12", stamped[0])
	}
	grn, prepared, err := packGRN(t, m, po, []map[string]interface{}{{"sku": sku, "pack_qty": 5, "invoice_pack_rate": 120}})
	if err != nil {
		t.Fatalf("GRN: %v", err)
	}
	l := prepared[0]
	if l["qty"] != 60.0 || l["invoice_rate"] != 10.0 || l["po_rate"] != 10.0 || l["pack_factor"] != 12.0 {
		t.Fatalf("GRN line %+v, want 60 pieces, invoice Rs 10, PO Rs 10", l)
	}
	packPost(t, m, grn, prepared)

	v := m.valuation(sku)
	if v["qty_on_hand"] != int64(60) || v["total_value"] != 600.0 {
		t.Errorf("valuation %+v, want 60 pieces worth Rs 600", v)
	}
	m.wantAvg(sku, 1000, "Rs 10 a piece")
	if got := m.gl(grn, "1200"); got != 60000 {
		t.Errorf("stock account %d paise, want 60000", got)
	}
	m.wantBalanced(grn)

	// Three-way match: the vendor's bill is 5 packs x Rs 120.
	bill := m.bill(po, grn, 600, nil)
	if res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 2); err != nil || !res.Matched {
		t.Fatalf("a bill of Rs 600 for 5 packs must match: %v %+v", err, res)
	}
	if n := m.gl(bill, "1200"); n != 0 {
		t.Errorf("an equal bill moved stock by %d paise", n)
	}

	// Landed cost: Rs 120 freight over 60 pieces is Rs 2 a piece.
	voucher, err := CreateLandedCostVoucher(m.tenant, grn, []map[string]interface{}{{"charge_type": "Freight", "amount": 120.0}}, "system")
	if err != nil {
		t.Fatalf("landed cost: %v", err)
	}
	m.docs = append(m.docs, voucher)
	if err := ApplyLandedCostVoucher(m.tenant, voucher, "system"); err != nil {
		t.Fatalf("apply landed cost: %v", err)
	}
	m.wantAvg(sku, 1200, "Rs 10 + Rs 2 freight a piece")

	// A return of one pack is 12 pieces, valued at the average.
	ret := m.id("PR")
	rl, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "stock_bucket": "Accepted", "qty": 12}})
	vendor := m.id("VEND")
	m.doc(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": vendor})
	p := map[string]interface{}{"grn_id": grn, "reason": "wrong size", "return_items": string(rl), "vendor_id": vendor, "location": m.loc}
	if err := validatePurchaseReturnRules(m.tenant, "", "", nil, p); err != nil {
		t.Fatalf("validate return: %v", err)
	}
	m.doc(ret, "PurchaseReturn", "Draft", p)
	res, err := PostPurchaseReturn(m.tenant, ret, "system", "HQ")
	if err != nil {
		t.Fatalf("PostPurchaseReturn: %v", err)
	}
	if res.DebitNoteID != "" {
		m.docs = append(m.docs, res.DebitNoteID)
	}
	if v := m.valuation(sku); v["qty_on_hand"] != int64(48) {
		t.Errorf("after returning one pack %+v, want 48 pieces", v)
	}
}

// The moving average (148.1) across two pack purchases at different pack prices.
func TestStage14835MovingAverageReadsPieces(t *testing.T) {
	packOn(t, true)
	m := newMC1481(t)
	sku := packItem(m, nil)
	for _, price := range []float64{120, 180} {
		po, _ := packPO(t, m, []map[string]interface{}{{"sku": sku, "pack_qty": 5, "pack_rate": price}})
		grn, prepared, err := packGRN(t, m, po, []map[string]interface{}{{"sku": sku, "pack_qty": 5, "invoice_pack_rate": price}})
		if err != nil {
			t.Fatalf("GRN: %v", err)
		}
		packPost(t, m, grn, prepared)
	}
	// 60 @ Rs 10 + 60 @ Rs 15 = Rs 1500 over 120 pieces.
	m.wantAvg(sku, 1250, "blend of the two pack prices")
	if v := m.valuation(sku); v["qty_on_hand"] != int64(120) || v["total_value"] != 1500.0 {
		t.Errorf("valuation %+v", v)
	}
}

// Off means off: pack keys are dropped, quantities stay as typed, the Item boxes
// are not in the meta, and a save of an item with a purchase unit is not checked.
func TestStage14835OffMeansOff(t *testing.T) {
	packOn(t, false)
	m := newMC1481(t)
	sku := packItem(m, nil)

	po, stamped := packPO(t, m, []map[string]interface{}{{"sku": sku, "qty": 7, "rate": 5, "pack_qty": 5, "pack_rate": 120}})
	if stamped[0]["qty"] != 7.0 || stamped[0]["rate"] != 5.0 {
		t.Errorf("off: PO line %+v, want qty 7 rate 5 as typed", stamped[0])
	}
	for _, k := range packKeysPO {
		if _, has := stamped[0][k]; has {
			t.Errorf("off: PO line kept %s", k)
		}
	}
	_, prepared, err := packGRN(t, m, po, []map[string]interface{}{{"sku": sku, "qty": 7, "pack_qty": 5, "invoice_pack_rate": 120}})
	if err != nil {
		t.Fatalf("GRN: %v", err)
	}
	if prepared[0]["qty"] != 7.0 {
		t.Errorf("off: GRN qty %v, want 7", prepared[0]["qty"])
	}
	for _, k := range packKeysGRN {
		if _, has := prepared[0][k]; has {
			t.Errorf("off: GRN line kept %s", k)
		}
	}
	fields, err := GetDocTypeMeta(m.tenant, "Item")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		switch f.Fieldname {
		case "uom", "purchase_unit", "purchase_unit_factor", "allow_fraction":
			t.Errorf("off: Item meta shows %s", f.Fieldname)
		}
	}
	if err := PrepareItemPackUnits(m.tenant, "", map[string]interface{}{"purchase_unit": "Box"}); err != nil {
		t.Errorf("off: item save checked: %v", err)
	}
	if u, f := packOffer(m.tenant, map[string]interface{}{"purchase_unit": "Box", "purchase_unit_factor": 4.0}); u != "" || f != 0 {
		t.Errorf("off: item offers a pack %q %v", u, f)
	}

	packOn(t, true)
	fields, _ = GetDocTypeMeta(m.tenant, "Item")
	seen := 0
	for _, f := range fields {
		switch f.Fieldname {
		case "uom", "purchase_unit", "purchase_unit_factor", "allow_fraction":
			seen++
		}
	}
	if seen != 4 {
		t.Errorf("on: Item meta shows %d of the 4 pack boxes", seen)
	}
}

// A document keeps the factor it used: the item's factor changing later moves
// neither the PO line nor a GRN against it.
func TestStage14835FactorChangeNeverRewritesPastDocuments(t *testing.T) {
	packOn(t, true)
	m := newMC1481(t)
	sku := packItem(m, nil)
	po, _ := packPO(t, m, []map[string]interface{}{{"sku": sku, "pack_qty": 2, "pack_rate": 120}})

	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = jsonb_set(data, '{purchase_unit_factor}', '24') WHERE id = $1", sku); err != nil {
		t.Fatal(err)
	}
	// Re-stamping the stored PO line (an edit) keeps factor 12.
	var stored map[string]interface{}
	data, _, _ := fetchDocData(m.tenant, "PurchaseOrder", po)
	stored = data
	payload := map[string]interface{}{"items": stored["items"], "vendor": "V"}
	if err := StampPOPricing(m.tenant, payload); err != nil {
		t.Fatal(err)
	}
	if l := packLines(t, payload["items"])[0]; l["qty"] != 24.0 || l["pack_factor"] != 12.0 {
		t.Errorf("re-saved PO line %+v, want 24 pieces on factor 12", l)
	}
	// A GRN against that PO follows the PO's factor, not the item's new one.
	_, prepared, err := packGRN(t, m, po, []map[string]interface{}{{"sku": sku, "pack_qty": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if prepared[0]["qty"] != 24.0 || prepared[0]["pack_factor"] != 12.0 {
		t.Errorf("GRN line %+v, want 24 pieces on factor 12", prepared[0])
	}
	// A new PO takes the new factor.
	_, fresh := packPO(t, m, []map[string]interface{}{{"sku": sku, "pack_qty": 1, "pack_rate": 240}})
	if fresh[0]["qty"] != 24.0 || fresh[0]["pack_factor"] != 24.0 || fresh[0]["rate"] != 10.0 {
		t.Errorf("new PO line %+v, want 24 pieces on factor 24 at Rs 10", fresh[0])
	}
}

// A part-piece is refused unless the item allows it; a PO is whole pieces always.
func TestStage14835PartPiecesRefusedUnlessAllowed(t *testing.T) {
	packOn(t, true)
	m := newMC1481(t)
	plain := packItem(m, nil)
	loose := packItem(m, map[string]interface{}{"allow_fraction": "Yes"})

	raw, _ := json.Marshal([]map[string]interface{}{{"sku": plain, "pack_qty": 0.3}})
	err := StampPOPricing(m.tenant, map[string]interface{}{"items": string(raw), "vendor": "V"})
	if t14835Code(err) != "PURCHA-5132" {
		t.Errorf("PO of 3.6 pieces: %v, want PURCHA-5132", err)
	}
	po, _ := packPO(t, m, []map[string]interface{}{{"sku": plain, "pack_qty": 1}, {"sku": loose, "pack_qty": 1}})
	if _, _, err := packGRN(t, m, po, []map[string]interface{}{{"sku": plain, "pack_qty": 0.3}}); t14835Code(err) != "PURCHA-5131" {
		t.Errorf("GRN of 3.6 pieces on a plain item: %v, want PURCHA-5131", err)
	}
	if _, g, err := packGRN(t, m, po, []map[string]interface{}{{"sku": loose, "pack_qty": 0.3}}); err != nil || g[0]["qty"] != 3.6 {
		t.Errorf("GRN of 3.6 pieces where allowed: %v %+v", err, g)
	}
	if _, g, err := packGRN(t, m, po, []map[string]interface{}{{"sku": plain, "pack_qty": 0.5}}); err != nil || g[0]["qty"] != 6.0 {
		t.Errorf("half a pack is 6 whole pieces: %v %+v", err, g)
	}
	// An item with no purchase unit takes no packs.
	bare := m.item(nil)
	raw, _ = json.Marshal([]map[string]interface{}{{"sku": bare, "pack_qty": 2}})
	if err := StampPOPricing(m.tenant, map[string]interface{}{"items": string(raw), "vendor": "V"}); t14835Code(err) != "PURCHA-5130" {
		t.Errorf("packs of an item with no purchase unit: %v, want PURCHA-5130", err)
	}
}

func t14835Code(err error) string {
	if err == nil {
		return ""
	}
	if v, ok := err.(*ValidationError); ok {
		return v.Code
	}
	return err.Error()
}

// The Item save: a unit needs its factor, and the stock unit is locked once the
// item has moved (an item that moved before the setting was on, too).
func TestStage14835ItemSaveAndStockUnitLock(t *testing.T) {
	packOn(t, true)
	m := newMC1481(t)

	if c := t14835Code(PrepareItemPackUnits(m.tenant, "", map[string]interface{}{"purchase_unit": "Box"})); c != "INVENT-1400" {
		t.Errorf("unit without factor: %q, want INVENT-1400", c)
	}
	if c := t14835Code(PrepareItemPackUnits(m.tenant, "", map[string]interface{}{"purchase_unit_factor": 6.0})); c != "INVENT-1400" {
		t.Errorf("factor without unit: %q, want INVENT-1400", c)
	}
	if c := t14835Code(PrepareItemPackUnits(m.tenant, "", map[string]interface{}{"uom": "pc", "purchase_unit": "PC", "purchase_unit_factor": 6.0})); c != "INVENT-1402" {
		t.Errorf("unit = stock unit: %q, want INVENT-1402", c)
	}
	if err := PrepareItemPackUnits(m.tenant, "", map[string]interface{}{"uom": "pc", "purchase_unit": "Box", "purchase_unit_factor": "6"}); err != nil {
		t.Errorf("a good unit: %v", err)
	}

	still := m.item(map[string]interface{}{"uom": "pc"})
	if err := PrepareItemPackUnits(m.tenant, still, map[string]interface{}{"uom": "kg"}); err != nil {
		t.Errorf("no movement yet, unit change: %v", err)
	}
	moved := m.item(map[string]interface{}{"uom": "pc"})
	m.receive(moved, 4, 10)
	if c := t14835Code(PrepareItemPackUnits(m.tenant, moved, map[string]interface{}{"uom": "kg"})); c != "INVENT-1401" {
		t.Errorf("moved item, unit change: %q, want INVENT-1401", c)
	}
	if err := PrepareItemPackUnits(m.tenant, moved, map[string]interface{}{"uom": "PC", "purchase_unit": "Box", "purchase_unit_factor": 6.0}); err != nil {
		t.Errorf("same unit, a purchase unit added: %v", err)
	}
	// Moved before the setting existed: blank stock unit is locked too.
	old := m.item(nil)
	m.receive(old, 2, 10)
	if c := t14835Code(PrepareItemPackUnits(m.tenant, old, map[string]interface{}{"uom": "kg"})); c != "INVENT-1401" {
		t.Errorf("old moved item, first unit: %q, want INVENT-1401", c)
	}
}

// The PO screen asks the preview what an item offers; off it offers nothing.
func TestStage14835PreviewOffersPackUnit(t *testing.T) {
	m := newMC1481(t)
	sku := packItem(m, nil)
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 60, "rate": 10}})
	for _, on := range []bool{true, false} {
		packOn(t, on)
		p, err := PreviewPurchaseOrder(m.tenant, map[string]interface{}{"items": string(lines)})
		if err != nil || len(p.Lines) != 1 {
			t.Fatalf("preview (on=%v): %v", on, err)
		}
		got := p.Lines[0]
		if on && (got.PackOfferUnit != "Pack12" || got.PackOfferFactor != 12) {
			t.Errorf("on: offer %q x %v, want Pack12 x 12", got.PackOfferUnit, got.PackOfferFactor)
		}
		if !on && (got.PackOfferUnit != "" || got.PackOfferFactor != 0) {
			t.Errorf("off: offered %q x %v", got.PackOfferUnit, got.PackOfferFactor)
		}
	}
	packOn(t, false)
}

// 144 to a pack at Rs 1,000: 100 packs must be Rs 1,00,000, not Rs 99,999.36.
func TestStage14835PackPriceAddsBackUp(t *testing.T) {
	packOn(t, true)
	m := newMC1481(t)
	sku := packItem(m, map[string]interface{}{"purchase_unit": "Gross", "purchase_unit_factor": 144.0})
	po, stamped := packPO(t, m, []map[string]interface{}{{"sku": sku, "pack_qty": 100, "pack_rate": 1000}})
	if got := round2(stamped[0]["rate"].(float64) * stamped[0]["qty"].(float64)); got != 100000 {
		t.Fatalf("PO line %+v comes to %v, want 100000", stamped[0], got)
	}
	grn, prepared, err := packGRN(t, m, po, []map[string]interface{}{{"sku": sku, "pack_qty": 100, "invoice_pack_rate": 1000}})
	if err != nil {
		t.Fatal(err)
	}
	packPost(t, m, grn, prepared)
	// Stock is costed in whole paise a piece (Rs 6.94), so the receipt books a
	// little under, and the bill's match brings the stock account to what was paid.
	m.wantBalanced(grn)
	bill := m.bill(po, grn, 100000, nil)
	res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 2)
	if err != nil || !res.Matched {
		t.Fatalf("a bill of Rs 1,00,000 for 100 packs must match: %v %+v", err, res)
	}
	t.Logf("receipt %d paise, bill %d paise on 1200", m.gl(grn, "1200"), m.gl(bill, "1200"))
	if got := m.gl(grn, "1200") + m.gl(bill, "1200"); got != 10000000 {
		t.Errorf("stock account %d paise after the bill, want 10000000", got)
	}
}

// A GRN against a PO follows the PO line's factor, whatever the line was sent with.
func TestStage14835GRNFollowsThePOFactor(t *testing.T) {
	packOn(t, true)
	m := newMC1481(t)
	sku := packItem(m, nil)
	po, _ := packPO(t, m, []map[string]interface{}{{"sku": sku, "pack_qty": 5, "pack_rate": 120}})
	_, prepared, err := packGRN(t, m, po, []map[string]interface{}{{"sku": sku, "pack_qty": 5, "pack_factor": 1000, "invoice_pack_rate": 120}})
	if err != nil {
		t.Fatal(err)
	}
	if prepared[0]["qty"] != 60.0 || prepared[0]["pack_factor"] != 12.0 {
		t.Fatalf("GRN line %+v, want 60 pieces at the PO's 12", prepared[0])
	}
}

// A vendor's bill for whole packs adds up on a stepped receipt at any factor: 144
// to a pack at Rs 1,000 shows Rs 6.94 a piece, yet 50 packs bill Rs 50,000.
func TestStage14835BillOfWholePacksAddsUp(t *testing.T) {
	packOn(t, true)
	m := newMC1481(t)
	sku := packItem(m, map[string]interface{}{"purchase_unit": "Gross", "purchase_unit_factor": 144.0})
	receive := func(mode string, line map[string]interface{}, typed float64) (map[string]interface{}, []map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "pack_qty": 100, "pack_rate": 1000}})
		po := map[string]interface{}{"items": string(raw), "vendor": "V", "gst_mode": mode}
		if err := StampPOPricing(m.tenant, po); err != nil {
			t.Fatalf("StampPOPricing: %v", err)
		}
		poID := m.id("PO")
		m.doc(poID, "PurchaseOrder", "Approved", map[string]interface{}{"code": poID, "gst_mode": mode, "items": po["items"]})
		line["sku"] = sku
		lines, _ := json.Marshal([]map[string]interface{}{line})
		payload := map[string]interface{}{"po_id": poID, "location": m.loc, "flow": GRNFlowSteps, "vendor_invoice_amount": typed, "received_items": string(lines)}
		if err := PrepareGRNPacks(m.tenant, payload); err != nil {
			t.Fatal(err)
		}
		if err := PrepareGRNPricing(m.tenant, true, payload); err != nil {
			t.Fatal(err)
		}
		out := grnLineMaps(payload)
		grnStampTotals(payload, out)
		return payload, out
	}
	for _, mode := range []string{GSTModeExclusive, GSTModeInclusive} {
		// Packs keyed.
		if got, _ := receive(mode, map[string]interface{}{"pack_qty": 50}, 50000); got["invoice_unexplained"] != 0.0 || got["note_difference"] != 0.0 {
			t.Errorf("%s 50 packs: unexplained %v note %v, want 0", mode, got["invoice_unexplained"], got["note_difference"])
		}
		// Left in pieces, as the PO loaded it.
		if got, _ := receive(mode, map[string]interface{}{"qty": 14400, "accepted_qty": 14400}, 100000); got["invoice_unexplained"] != 0.0 {
			t.Errorf("%s all pieces: unexplained %v, want 0", mode, got["invoice_unexplained"])
		}
		// The vendor's own pack price wins.
		if got, _ := receive(mode, map[string]interface{}{"pack_qty": 50, "invoice_pack_rate": 1010}, 50500); got["invoice_unexplained"] != 0.0 {
			t.Errorf("%s 1010 a pack: unexplained %v, want 0", mode, got["invoice_unexplained"])
		}
		// So does a typed price a piece, exactly as typed.
		rateKey := "invoice_rate"
		if mode == GSTModeInclusive {
			rateKey = "invoice_rate_incl"
		}
		if got, _ := receive(mode, map[string]interface{}{"qty": 100, "accepted_qty": 100, rateKey: 7.0}, 700); got["invoice_unexplained"] != 0.0 {
			t.Errorf("%s typed 7.00: unexplained %v, want 0", mode, got["invoice_unexplained"])
		}
		// A forged exact price is thrown away.
		if _, lines := receive(mode, map[string]interface{}{"qty": 100, "accepted_qty": 100, rateKey: 7.0, "exact_unit_price": 7.004}, 700); numFromInterface(lines[0]["exact_unit_price"]) == 7.004 {
			t.Errorf("%s forged exact price kept: %+v", mode, lines[0])
		}
	}
	// A line not priced by packs keeps its old rounding.
	if got := func() float64 {
		v, _ := grnLineAmount(map[string]interface{}{}, 3, 6.94)
		return v
	}(); got != 20.82 {
		t.Errorf("plain line %v, want 20.82", got)
	}
}

// Stock costs whole paise a piece, so a pack line goes in a little off its value:
// the bill settles the gap. Once paid, 2100 is clear and 1200 holds the true value.
// The item's own average stays whole paise, on any item, so it can sit up to half a
// paisa a piece from 1200.
func TestStage14835StockSettlesToTheBill(t *testing.T) {
	packOn(t, true)
	m := newMC1481(t)
	preserveSeries(t, m.schema, "DN", "VCN", "GRNBatchSeq", "VendorInvoice", "GRN")
	for _, c := range []struct {
		mode        string
		typed       float64
		value       int64
		bookedFirst int64
	}{
		{GSTModeExclusive, 59000, 5000000, 4996800}, // 6.94 x 7,200 goes in, Rs 50,000 is owed
		{GSTModeInclusive, 50000, 4237288, 4240800}, // 5.89 x 7,200 goes in, Rs 42,372.88 is owed
	} {
		t.Run(c.mode, func(t *testing.T) {
			m.t = t
			sku := packItem(m, map[string]interface{}{"purchase_unit": "Gross", "purchase_unit_factor": 144.0, "mrp": 999.0, "gst_rate": 18.0, "hsn_code": "6204", "tax_treatment": "Taxable"})
			vend, po, grn := m.id("VEND"), m.id("PO"), m.id("GRN")
			m.doc(vend, "Vendor", "Active", map[string]interface{}{"code": vend, "name": vend})
			items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "pack_qty": 100, "pack_rate": 1000}})
			pod := map[string]interface{}{"code": po, "vendor": vend, "gst_mode": c.mode, "items": string(items)}
			if err := StampPOPricing(m.tenant, pod); err != nil {
				t.Fatalf("StampPOPricing: %v", err)
			}
			m.doc(po, "PurchaseOrder", "Approved", pod)
			lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "po_id": po, "pack_qty": 50}})
			payload := map[string]interface{}{"flow": "steps", "po_ids": po, "location": m.loc, "vendor": vend, "vendor_invoice_no": m.id("INV"),
				"vendor_invoice_date": "2026-10-18", "vendor_invoice_amount": c.typed, "received_items": string(lines)}
			for _, step := range []func() error{
				func() error { return PrepareGRNPacks(m.tenant, payload) },
				func() error { return PrepareGRNPricing(m.tenant, true, payload) },
				func() error { return PrepareGRNSteps(m.tenant, "system", true, payload) },
				func() error { return PrepareGRNBatchNumbers(m.tenant, true, payload) },
				func() error { return validateGRNRules(m.tenant, "", payload) },
			} {
				if err := step(); err != nil {
					t.Fatalf("receive: %v", err)
				}
			}
			payload["code"] = grn
			m.doc(grn, "GRN", strField(payload, "status"), payload)
			SyncGRNLineRows(m.tenant, grn)
			t.Cleanup(func() {
				db.DB.Exec("DELETE FROM "+m.schema+".grn_line WHERE grn_id = $1", grn)
				db.DB.Exec("DELETE FROM "+m.schema+".grn_line_change WHERE grn_id = $1", grn)
			})
			if _, err := VerifyGRNStep(m.tenant, "system", "Super Admin", grn, GRNVerifyInput{Reason: "checked"}); err != nil {
				t.Fatalf("verify: %v", err)
			}
			s := mc1481Step{sku: sku, grn: grn, po: po}
			v := m.postStep(s)
			if v.DebitNoteID != "" || v.VendorCreditNoteID != "" {
				t.Fatalf("no note wanted, got %q %q", v.DebitNoteID, v.VendorCreditNoteID)
			}
			if got := m.gl(grn, "1200"); got != c.bookedFirst {
				t.Errorf("receipt booked %d, want %d", got, c.bookedFirst)
			}
			if got := m.glAll("1200", grn, v.VendorBillID); got != c.value {
				t.Errorf("1200 after the bill %d, want %d", got, c.value)
			}
			m.payStep(s)
			if got := m.glAll("2100", grn, v.VendorBillID); got != 0 {
				t.Errorf("2100 after payment %d, want 0", got)
			}
		})
	}
}
