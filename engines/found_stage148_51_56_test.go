package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 148.51-148.56: gaps earlier items found and left, each fixed on its own.

// 148.56: stock takes whole units per line, so the hold takes the same, never
// a piece from stock that was already there.
func TestStage14856HoldIsWhatTheReceiptPutIn(t *testing.T) {
	f := inspTenant(t)
	f.receive(t, "G-BEFORE", "V1", "L1", line("I-GOLD", 10))
	f.on(t)
	f.receive(t, "G-PART", "V1", "L1", line("I-GOLD", 2.5))
	if on, a, h := f.stock(t, "I-GOLD", "L1"); on != 12 || a != 10 || h != 2 {
		t.Fatalf("2.5 received: on_hand %d available %d hold %d, want 12/10/2", on, a, h)
	}
	f.receive(t, "G-PART2", "V1", "L1", line("I-GOLD", 1.5), line("I-GOLD", 1.5))
	if on, a, h := f.stock(t, "I-GOLD", "L1"); on != 14 || a != 10 || h != 4 {
		t.Fatalf("two lines of 1.5: on_hand %d available %d hold %d, want 14/10/4", on, a, h)
	}
	f.receive(t, "G-PART3", "V1", "L1", line("I-GOLD", 0.5))
	if on, a, h := f.stock(t, "I-GOLD", "L1"); on != 14 || a != 10 || h != 4 {
		t.Fatalf("0.5 received: on_hand %d available %d hold %d, want 14/10/4", on, a, h)
	}
	if lots := f.lots(t, "G-PART3"); len(lots) != 0 {
		t.Fatalf("no whole piece came in, so no lot: %v", lots)
	}
}

// 148.52: a drop-ship PO has its tax from the moment it is raised and keeps it
// through an order edit.
func TestStage14852DropShipPOCarriesItsTax(t *testing.T) {
	f := dsFixture(t)
	order := f.dropOrder(SalesOrderLineInput{SKU: "WS-D", Qty: 5, UnitPrice: 100})
	totals := func(wantTaxable, wantGrand, wantRound float64) {
		t.Helper()
		_, _, po := f.poOf(order)
		if po["gst_breakdown"] == nil {
			t.Fatalf("no tax on the PO: %v", po)
		}
		if got := numFromInterface(po["total_amount"]); got != wantTaxable {
			t.Errorf("total_amount %v, want %v", got, wantTaxable)
		}
		if got := numFromInterface(po["grand_total"]); got != wantGrand {
			t.Errorf("grand_total %v, want %v", got, wantGrand)
		}
		if got := numFromInterface(po["round_off"]); round2(got) != wantRound {
			t.Errorf("round_off %v, want %v", got, wantRound)
		}
		bd, _ := po["gst_breakdown"].(map[string]interface{})
		if got := numFromInterface(bd["taxable_amount"]); got != wantTaxable {
			t.Errorf("breakdown taxable %v, want %v", got, wantTaxable)
		}
	}
	totals(300, 354, 0)
	if _, err := EditOrderLines(f.tenantID, order, []OrderLineChange{{LineID: order + "-L1", Qty: 8}}, "customer asked", "u1"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	totals(480, 566, -0.4)
}

// 148.55: bill cancel and exchange reach the same return, so the gift goes back
// to the account the sale charged. A gift alone has no value to carry to a new
// sale: that is refused before anything moves.
func TestStage14855CancelAndExchangeOfAGift(t *testing.T) {
	f := stage1484Setup(t)
	f.insert("GIFT14845", "Item", "Active", "system", map[string]interface{}{"code": "GIFT14845", "name": "Free mug", "category": "Gifts", "sale_price": 0.0, "standard_cost": 25.0, "sellability": SellPromoOnly})
	if _, err := PostInventoryLedger(f.tenantID, "S1484", []interface{}{map[string]interface{}{"sku": "GIFT14845", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	sold := func(cart string) {
		t.Helper()
		stage14845Cart(t, f, cart, true)
		if _, _, err := FinalizePOSCheckout(f.tenantID, cart, ""); err != nil {
			t.Fatalf("checkout %s: %v", cart, err)
		}
	}
	const giftAcct = "5180"

	sold("CART-14855-C")
	out, err := CancelPOSBill(f.tenantID, RoleStoreManager, "mgr14855", "CART-14855-C", "S1484", "wrong bill")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := stage1484Posted(t, f.schema, "ReturnRequest", out.ReturnRequestID); got["C:"+giftAcct] != 2500 || got["C:5100"] != 4000 || got["D:1200"] != 6500 {
		t.Fatalf("bill cancel journal: %v", got)
	}

	sold("CART-14855-X")
	credit, err := StartTillExchange(f.tenantID, "cashier14855", ExchangeReturnRequest{OriginalOrderID: "CART-14855-X", Location: "S1484",
		Lines: []ReturnItemInput{{SKU: "TEE1484", Qty: 1}, {SKU: "GIFT14845", Qty: 1}}})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if got := stage1484Posted(t, f.schema, "ReturnRequest", credit.ReturnRequestID); got["C:"+giftAcct] != 2500 || got["C:5100"] != 4000 || got["D:1200"] != 6500 {
		t.Fatalf("exchange journal: %v", got)
	}

	sold("CART-14855-G")
	before, _, _ := giftStock(t, f, "GIFT14845")
	_, err = StartTillExchange(f.tenantID, "cashier14855", ExchangeReturnRequest{OriginalOrderID: "CART-14855-G", Location: "S1484",
		Lines: []ReturnItemInput{{SKU: "GIFT14845", Qty: 1}}})
	if t1481Code(err) != "POSOFF-0611" {
		t.Fatalf("a gift alone: want POSOFF-0611, got %v", err)
	}
	if after, _, _ := giftStock(t, f, "GIFT14845"); after != before {
		t.Fatalf("the refusal moved stock: %d to %d", before, after)
	}
}

func giftStock(t *testing.T, f stage1484Fixture, sku string) (onHand, available, hold int) {
	t.Helper()
	return inspFix{tenant: f.tenantID, schema: f.schema}.stock(t, sku, "S1484")
}

// 148.54: a summary invoice carries many orders. While it is Draft nothing was
// declared, so it goes and the orders can be billed again. Once posted, each
// cancelled order gets its own note for its own share, tax with it.
func TestStage14854CancelledOrderInASummaryInvoice(t *testing.T) {
	f := newWSFix(t, true)
	wsSummaryCustomers(f, nil)
	o1 := f.order("WS-S1", "Karnataka", "PO-1", "Shipped", wsLine{sku: "WS-A", qty: 2, price: 118})
	o2 := f.order("WS-S2", "Karnataka", "PO-2", "Shipped", wsLine{sku: "WS-B", qty: 1, price: 118})
	create := func() string {
		t.Helper()
		p, err := PreviewSummaryInvoices(f.tenantID, "", "", "")
		if err != nil || len(p.Groups) != 1 || len(p.Groups[0].Orders) != 2 {
			t.Fatalf("preview: %+v %v", p, err)
		}
		ids, err := CreateSummaryInvoices(f.tenantID, "system", "", "", "", []string{p.Groups[0].Key})
		if err != nil || len(ids) != 1 {
			t.Fatalf("create: %v %v", ids, err)
		}
		return ids[0]
	}

	draft := create()
	notes, err := IssueCancellationCreditNotes(f.tenantID, o1, "CUST", "u1")
	if err != nil || len(notes) != 0 {
		t.Fatalf("draft: notes %v, err %v", notes, err)
	}
	if _, status, _ := fetchDocData(f.tenantID, "SalesInvoice", draft); status != "Cancelled" {
		t.Fatalf("the draft should go, status %q", status)
	}

	inv := create()
	if _, err := PostSalesInvoice(f.tenantID, inv, "system"); err != nil {
		t.Fatal(err)
	}
	second, err := IssueCancellationCreditNotes(f.tenantID, o2, "CUST", "u1")
	if err != nil || len(second) != 1 {
		t.Fatalf("second order: notes %v, err %v", second, err)
	}
	note := f.doc("CreditNote", second[0])
	if numFromInterface(note["amount"]) != 118 || strField(note, "sales_invoice_id") != inv || strField(note, "sales_order_id") != o2 {
		t.Fatalf("note: %v", note)
	}
	if again, err := IssueCancellationCreditNotes(f.tenantID, o2, "CUST", "u1"); err != nil || len(again) != 1 || again[0] != second[0] {
		t.Fatalf("a retry must not double the credit: %v %v", again, err)
	}
	first, err := IssueCancellationCreditNotes(f.tenantID, o1, "CUST", "u1")
	if err != nil || len(first) != 1 || first[0] == second[0] {
		t.Fatalf("the other order gets its own note: %v, %v", first, err)
	}
	if got := numFromInterface(f.doc("CreditNote", first[0])["amount"]); got != 236 {
		t.Fatalf("first order's share %v, want 236", got)
	}
	if _, err := PostCreditNote(f.tenantID, second[0], "system"); err != nil {
		t.Fatalf("post note: %v", err)
	}
	if _, cr := f.gl("CreditNote", second[0], "1300"); cr != 11800 {
		t.Fatalf("receivable credited %d, want 11800", cr)
	}
	if dr, _ := f.gl("CreditNote", second[0], "2200"); dr != 900 {
		t.Fatalf("CGST given back %d, want 900", dr)
	}
	// The shares are the engine's: a form save cannot change them.
	forged := map[string]interface{}{"summary_order_amounts": map[string]interface{}{o1: 9999.0}}
	KeepEngineOnlyFields(f.tenantID, "SalesInvoice", inv, forged)
	shares, _ := forged["summary_order_amounts"].(map[string]interface{})
	if numFromInterface(shares[o1]) != 236 {
		t.Fatalf("forged share kept: %v", forged)
	}
}

// inclusiveStep receives a plain (no pack) line from a GST-inclusive PO. old
// takes the mark off, as on a GRN made before 148.51.
func inclusiveStep(t *testing.T, m *mc1481, qty, rate float64, old bool) mc1481Step {
	t.Helper()
	sku, vend, po, grn := m.id("I"), m.id("VEND"), m.id("PO"), m.id("GRN")
	m.doc(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku + " name", "hsn_code": "6204", "gst_rate": 18.0, "tax_treatment": "Taxable", "mrp": 999.0})
	m.skus = append(m.skus, sku)
	m.doc(vend, "Vendor", "Active", map[string]interface{}{"code": vend, "name": vend})
	items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": qty, "rate": rate}})
	pod := map[string]interface{}{"code": po, "vendor": vend, "gst_mode": GSTModeInclusive, "items": string(items)}
	if err := StampPOPricing(m.tenant, pod); err != nil {
		t.Fatalf("StampPOPricing: %v", err)
	}
	m.doc(po, "PurchaseOrder", "Approved", pod)
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "po_id": po, "qty": qty}})
	payload := map[string]interface{}{"flow": "steps", "po_ids": po, "location": m.loc, "vendor": vend, "vendor_invoice_no": m.id("INV"),
		"vendor_invoice_date": "2026-10-18", "vendor_invoice_amount": round2(qty * rate), "received_items": string(lines)}
	for _, step := range []func() error{
		func() error { return PrepareGRNPricing(m.tenant, true, payload) },
		func() error { return PrepareGRNSteps(m.tenant, "system", true, payload) },
		func() error { return PrepareGRNBatchNumbers(m.tenant, true, payload) },
		func() error { return validateGRNRules(m.tenant, "", payload) },
	} {
		if err := step(); err != nil {
			t.Fatalf("receive: %v", err)
		}
	}
	if old {
		ls := grnLineMaps(payload)
		for _, l := range ls {
			delete(l, "stock_round_settled")
		}
		raw, _ := json.Marshal(ls)
		payload["received_items"] = string(raw)
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
	return mc1481Step{sku: sku, grn: grn, po: po}
}

// 148.51: stock goes in at the rounded price a piece, the vendor is owed the
// line's own value. On a new GRN the bill settles the gap, so nothing stays on
// the payable. A GRN made before this keeps the books it has.
func TestStage14851InclusiveLineLeavesNothingOnThePayable(t *testing.T) {
	m := newMC1481(t)
	preserveSeries(t, m.schema, "DN", "VCN", "GRNBatchSeq", "VendorInvoice", "GRN")
	// Stage 173.1: the bill's rounding goes to Round Off (5280), so 1200 stays
	// at the stock value to the paisa.
	for _, c := range []struct {
		name               string
		qty, rate          float64
		old                bool
		stock, owed, round int64
	}{
		{"new small", 7, 99.99, false, 59318, 0, -2},
		{"new large", 1000, 99.99, false, 8474000, 0, -271},
		{"old small", 7, 99.99, true, 59318, -2, 0},
		{"old large", 1000, 99.99, true, 8474000, -271, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			m.t = t
			s := inclusiveStep(t, m, c.qty, c.rate, c.old)
			v := m.postStep(s)
			if got := m.glAll("1200", s.grn, v.VendorBillID); got != c.stock {
				t.Errorf("1200 after the bill %d, want %d", got, c.stock)
			}
			if got := m.glAll(accountRoundOff, s.grn, v.VendorBillID); got != c.round {
				t.Errorf("5280 after the bill %d, want %d", got, c.round)
			}
			if value := RupeesToPaise(numFromInterface(m.valuation(s.sku)["total_value"])); !c.old && value != c.stock {
				t.Errorf("stock value %d against 1200 %d", value, c.stock)
			}
			m.payStep(s)
			if got := m.glAll("2100", s.grn, v.VendorBillID); got != c.owed {
				t.Errorf("2100 after payment %d, want %d", got, c.owed)
			}
			if c.name == "new small" {
				s1731LegacyBillKeepsItsBooks(t, m, s, v.VendorBillID)
			}
		})
	}
	// The mark is the server's: a caller's is dropped, and an exclusive line has none.
	l := map[string]interface{}{grnStockRoundSettled: true}
	stampGRNExactPrice(l, grnPOLineRate{})
	if _, has := l[grnStockRoundSettled]; has {
		t.Errorf("a forged mark stayed: %v", l)
	}
}

// s1731LegacyBillKeepsItsBooks re-matches a bill as one booked before 173.1
// would hold it - the rounding inside its cost, no Round Off record - and wants
// nothing posted: the folded rounding equals what it booked.
func s1731LegacyBillKeepsItsBooks(t *testing.T, m *mc1481, s mc1481Step, billID string) {
	t.Helper()
	data, _, err := fetchDocData(m.tenant, "VendorInvoice", billID)
	if err != nil {
		t.Fatal(err)
	}
	cost, round := storedBillCost(data), storedBillRound(data)
	if len(round) == 0 {
		t.Fatalf("the bill kept no rounding: %v", data["bill_round_adjust"])
	}
	asStored := map[string]interface{}{} // as JSON reads it back
	for sku, v := range round {
		cost[sku] += v
	}
	for sku, v := range cost {
		asStored[sku] = float64(v)
	}
	data["bill_cost_adjust"] = asStored
	delete(data, "bill_round_adjust")
	before := [2]int64{m.glAll("1200", billID), m.glAll(accountRoundOff, billID)}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, m.schema); err != nil {
		t.Fatal(err)
	}
	if err := settleBillCostDifferenceTx(tx, m.tenant, m.schema, billID, data, map[string]interface{}{}, s.grn, true, false); err != nil {
		t.Fatalf("re-match: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if after := [2]int64{m.glAll("1200", billID), m.glAll(accountRoundOff, billID)}; after != before {
		t.Errorf("a re-match of a bill booked before 173.1 moved 1200 / 5280: %v -> %v", before, after)
	}
	if _, has := data["bill_round_adjust"]; has {
		t.Errorf("an old bill was moved onto the new rule: %v", data["bill_round_adjust"])
	}
}

// 148.53: a sheet's Items, attributes and content land together or not at all.
// s14853Trigger makes one write of the commit fail, which the preview cannot see.
func s14853Trigger(t *testing.T, schema, name, event, when, body string) {
	t.Helper()
	fn := schema + "." + name
	if _, err := db.DB.Exec(`CREATE OR REPLACE FUNCTION ` + fn + `() RETURNS trigger AS $f$ BEGIN ` + body + `; END $f$ LANGUAGE plpgsql`); err != nil {
		t.Fatal(err)
	}
	drop := func() {
		_, _ = db.DB.Exec(`DROP TRIGGER IF EXISTS ` + name + ` ON ` + schema + `.documents`)
		_, _ = db.DB.Exec(`DROP FUNCTION IF EXISTS ` + fn + `()`)
	}
	t.Cleanup(drop)
	if _, err := db.DB.Exec(`CREATE TRIGGER ` + name + ` BEFORE ` + event + ` ON ` + schema + `.documents FOR EACH ROW WHEN (` + when + `) EXECUTE FUNCTION ` + fn + `()`); err != nil {
		t.Fatal(err)
	}
}

func s14853Edits(schema, item string) int {
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.audit_logs WHERE action='EDIT_Item' AND details LIKE 'Edited Item ` + item + `:%'`).Scan(&n)
	return n
}

func TestStage14853FailedAttributeLeavesEveryItemAsItWas(t *testing.T) {
	schema := s14654Setup(t)
	prior := GetSettingInt("default", "platform.import_batch_rows")
	if err := SetSetting("default", "platform.import_batch_rows", "50", "test"); err != nil { // the registry's lowest, so the file spans two batches
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetSetting("default", "platform.import_batch_rows", fmt.Sprint(prior), "test") })

	kept, last := s14654P+"-K53", s14654P+"-L53"
	if r, err := ImportProductSheet(context.Background(), "default", strings.NewReader(s14654Sheet(kept+",Old name,7117,3,,,,x")), "system", "HR/Admin", true); err != nil || !r.Committed {
		t.Fatalf("first import: %v %+v", err, r)
	}
	edits := s14853Edits(schema, kept)
	s14853Trigger(t, schema, "s14853_attr", "INSERT", `NEW.doctype='ProductAttributeValue' AND NEW.data->>'item' = '`+last+`'`, `RAISE EXCEPTION 's14853 refused'`)

	rows := []string{kept + ",New name,7117,3,,,,x"}
	for i := 0; i < 50; i++ {
		rows = append(rows, fmt.Sprintf("%s-F53-%d,Fresh,7117,3,,,,x", s14654P, i))
	}
	rows = append(rows, last+",Last,7117,3,Gold,,,x")
	res, err := ImportProductSheet(context.Background(), "default", strings.NewReader(s14654Sheet(rows...)), "system", "HR/Admin", true)
	if err == nil {
		t.Fatalf("the failed attribute was not reported: %+v", res)
	}
	if v, _ := s14654Val(t, schema, "Item", kept, "name"); v != "Old name" {
		t.Errorf("the failed sheet changed an item: %q", v)
	}
	for _, id := range []string{s14654P + "-F53-0", s14654P + "-F53-49", last} {
		if _, ok := s14654Val(t, schema, "Item", id, "name"); ok {
			t.Errorf("the failed sheet created %s", id)
		}
	}
	if got := s14853Edits(schema, kept); got != edits {
		t.Errorf("an edit was logged for a change that was undone: %d then %d", edits, got)
	}
}

func TestStage14853ContentThatMovedLeavesNoItem(t *testing.T) {
	schema := s14654Setup(t)
	item := s14654P + "-M53"
	id := item + "::en"
	pimInsertDoc(t, schema, id, "ProductContent", "Draft", map[string]interface{}{"id": id, "product_id": item, "language": "en", "title": "Old"})
	// Skipping the update is what the write sees when the content moved after the check.
	s14853Trigger(t, schema, "s14853_content", "UPDATE", `NEW.doctype='ProductContent' AND NEW.id = '`+id+`'`, `RETURN NULL`)

	res, err := ImportProductSheet(context.Background(), "default", strings.NewReader(s14654Sheet(item+",Moved,7117,3,,New title,,x")), "system", "HR/Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Committed || len(res.Errors) == 0 {
		t.Fatalf("committed = %v, errors = %+v", res.Committed, res.Errors)
	}
	if _, ok := s14654Val(t, schema, "Item", item, "name"); ok {
		t.Error("the item was written though the content was refused")
	}
}

func TestStage14853CommitWritesEveryPartAndLogsTheEdit(t *testing.T) {
	schema := s14654Setup(t)
	item := s14654P + "-E53"
	if r, err := ImportProductSheet(context.Background(), "default", strings.NewReader(s14654Sheet(item+",Old name,7117,3,,,,x")), "system", "HR/Admin", true); err != nil || !r.Committed {
		t.Fatalf("first import: %v %+v", err, r)
	}
	edits := s14853Edits(schema, item)
	res, err := ImportProductSheet(context.Background(), "default", strings.NewReader(s14654Sheet(item+",New name,7117,3,Gold,New title,,x")), "system", "HR/Admin", true)
	if err != nil || !res.Committed {
		t.Fatalf("second import: %v %+v", err, res)
	}
	if res.Items == nil || res.Items.DryRun || len(res.Items.UpdatedIDs) != 1 {
		t.Errorf("result does not report the saved run: %+v", res.Items)
	}
	if v, _ := s14654Val(t, schema, "Item", item, "name"); v != "New name" {
		t.Errorf("item name = %q", v)
	}
	if v, _ := s14654Val(t, schema, "ProductAttributeValue", attributeValueID(item, s14654P+"-ATTR", "", ""), "value"); v != "Gold" {
		t.Errorf("attribute = %q", v)
	}
	if v, _ := s14654Val(t, schema, "ProductContent", item+"::en", "title"); v != "New title" {
		t.Errorf("content title = %q", v)
	}
	if got := s14853Edits(schema, item); got != edits+1 {
		t.Errorf("edit rows %d then %d, want one more", edits, got)
	}
}

// 148.57: a part receipt costs the whole units stock took, so the average holds.
func TestStage14857PartReceiptCostsWholeUnits(t *testing.T) {
	f := inspTenant(t)
	costed := func(grn string, qty float64) {
		t.Helper()
		lines, _ := json.Marshal([]map[string]interface{}{{"sku": "I-PLAIN", "qty": qty, "accepted_qty": qty, "invoice_rate": 250}})
		f.insert(grn, "GRN", "Draft", map[string]interface{}{"code": grn, "vendor": "V1", "location": "L1", "received_items": string(lines)})
		if _, err := PostGRNReceiptWithQC(f.tenant, "L1", []interface{}{line("I-PLAIN", qty)}, "system", grn); err != nil {
			t.Fatalf("receive %s: %v", grn, err)
		}
	}
	cost := func() (avg int64, qty float64) {
		t.Helper()
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT avg_unit_cost_paise, cumulative_qty_received FROM %s.item_cost WHERE item_code = 'I-PLAIN'`, f.schema)).Scan(&avg, &qty); err != nil {
			t.Fatal(err)
		}
		return
	}
	costed("G-57A", 10)
	costed("G-57B", 2.5)
	if avg, qty := cost(); avg != 25000 || qty != 12 {
		t.Fatalf("after 10 and 2.5 at 250: avg %d paise over %v units, want 25000 over 12", avg, qty)
	}
	costed("G-57C", 0.5)
	if avg, qty := cost(); avg != 25000 || qty != 12 {
		t.Fatalf("after 0.5 more: avg %d paise over %v units, want 25000 over 12", avg, qty)
	}
}

// 148.58: a cancelled order's note credits the invoice to the paise.
func TestStage14858CancellationNoteKeepsThePaise(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	insert("INV-14858", "SalesInvoice", "Approved", "system", map[string]interface{}{"code": "INV-14858", "sales_order_id": "SO-14858", "customer": "C1", "total_amount": 900.45})
	notes, err := IssueCancellationCreditNotes(tenantID, "SO-14858", "CUST", "u1")
	if err != nil || len(notes) != 1 {
		t.Fatalf("notes %v, err %v", notes, err)
	}
	var amount float64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT (data->>'amount')::numeric FROM %s.documents WHERE id = $1`, schema), notes[0]).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != 900.45 {
		t.Fatalf("note amount %v, want 900.45", amount)
	}
}

// 148.59: an exchange where nothing would refund is refused before stock or the
// journal moves, so no return is left behind.
func TestStage14859ExchangeWithNothingRefundableRefusesFirst(t *testing.T) {
	f := stage1484Setup(t)
	const cart = "CART-14859-W"
	stage14845Cart(t, f, cart, false)
	if _, _, err := FinalizePOSCheckout(f.tenantID, cart, ""); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	var before int
	stock := func() int {
		t.Helper()
		var n int
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(on_hand), 0) FROM %s.inventory_availability WHERE sku = 'TEE1484'`, f.schema)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before = stock()
	_, err := StartTillExchange(f.tenantID, "cashier14859", ExchangeReturnRequest{OriginalOrderID: cart, Location: "S1484",
		Lines: []ReturnItemInput{{SKU: "TEE1484", Qty: 1}}, Dispositions: map[string]string{"TEE1484": "Wrong-Item"}})
	if t1481Code(err) != "POSOFF-0611" {
		t.Fatalf("want POSOFF-0611, got %v", err)
	}
	if after := stock(); after != before {
		t.Fatalf("stock moved: %d then %d", before, after)
	}
	var returns int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ReturnRequest' AND data->>'original_order_id' = $1`, f.schema), cart).Scan(&returns); err != nil {
		t.Fatal(err)
	}
	if returns != 0 {
		t.Fatalf("%d return(s) left behind", returns)
	}
	// The same bill with a condition that refunds still exchanges.
	if _, err := StartTillExchange(f.tenantID, "cashier14859", ExchangeReturnRequest{OriginalOrderID: cart, Location: "S1484",
		Lines: []ReturnItemInput{{SKU: "TEE1484", Qty: 1}}}); err != nil {
		t.Fatalf("a sellable return should exchange: %v", err)
	}
}
