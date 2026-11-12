package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 148.16: memo stock. Fixtures come from the 148.1 costing tests.

func memoModule(t *testing.T, m *mc1481, on bool) {
	t.Helper()
	// The migration writes no per-tenant row, so the row may not exist: remember that.
	var before bool
	hadRow := db.DB.QueryRow(`SELECT enabled FROM `+m.schema+`.module_entitlements WHERE module_key = 'memo_stock'`).Scan(&before) == nil
	if _, err := db.DB.Exec(`INSERT INTO `+m.schema+`.module_entitlements (module_key, enabled, granted_by) VALUES ('memo_stock', $1, 'test')
		ON CONFLICT (module_key) DO UPDATE SET enabled = EXCLUDED.enabled`, on); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadRow {
			db.DB.Exec(`UPDATE `+m.schema+`.module_entitlements SET enabled = $1 WHERE module_key = 'memo_stock'`, before)
		} else {
			db.DB.Exec(`DELETE FROM ` + m.schema + `.module_entitlements WHERE module_key = 'memo_stock'`)
		}
		for _, sku := range m.skus {
			db.DB.Exec(`DELETE FROM ` + m.schema + `.memo_stock_sales WHERE sku = '` + sku + `'`)
			db.DB.Exec(`DELETE FROM ` + m.schema + `.memo_stock_lots WHERE sku = '` + sku + `'`)
		}
	})
}

// memoReceive posts a GRN marked Memo stock for vendor.
func memoReceive(t *testing.T, m *mc1481, vendor, sku string, qty, rate float64) string {
	t.Helper()
	lines := []mc1481Line{{sku: sku, qty: qty, rate: rate}}
	grnID, _ := m.grnDocs(m.loc, lines)
	if _, err := db.DB.Exec(`UPDATE `+m.schema+`.documents SET data = data || $2::jsonb WHERE id = $1`, grnID,
		`{"memo_stock":"Yes","vendor":"`+vendor+`"}`); err != nil {
		t.Fatal(err)
	}
	if err := m.postGRN(m.loc, grnID, lines); err != nil {
		t.Fatalf("memo receipt: %v", err)
	}
	return grnID
}

func memoVendor(m *mc1481) string {
	v := m.id("VEND")
	m.doc(v, "Vendor", "Active", map[string]interface{}{"code": v, "name": v})
	return v
}

func memoBills(t *testing.T, m *mc1481, source string) (ids []string, amounts []float64) {
	t.Helper()
	rows, err := db.DB.Query(`SELECT id, COALESCE((data->>'invoice_amount')::float8, 0) FROM `+m.schema+`.documents
		WHERE doctype = 'VendorInvoice' AND data->>'source_doc_id' = $1 ORDER BY id`, source)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var a float64
		rows.Scan(&id, &a)
		ids, amounts = append(ids, id), append(amounts, a)
		m.docs = append(m.docs, id)
	}
	return
}

func memoLotCount(t *testing.T, m *mc1481, sku string) (n int, open float64) {
	t.Helper()
	if err := db.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(qty_received - qty_sold - qty_returned - qty_lost), 0)::float8 FROM `+m.schema+`.memo_stock_lots WHERE sku = $1`, sku).Scan(&n, &open); err != nil {
		t.Fatal(err)
	}
	return
}

// A memo receipt adds quantity at zero value, a sale raises one bill for what
// sold, and a return to the vendor raises nothing.
func TestStage14816MemoReceiptSaleReturn(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, true)
	vendor := memoVendor(m)
	sku := m.item(nil)
	own, _ := m.receive(sku, 10, 100)
	memoGRN := memoReceive(t, m, vendor, sku, 5, 80)

	if n, open := memoLotCount(t, m, sku); n != 1 || open != 5 {
		t.Fatalf("memo lots %d open %v, want 1 lot of 5", n, open)
	}
	m.wantAvg(sku, 10000, "a memo receipt must not move the average")
	for _, acct := range []string{"1200", "2100"} {
		if got := m.gl(memoGRN, acct); got != 0 {
			t.Errorf("memo receipt posted %d paise to %s", got, acct)
		}
	}
	if v := m.valuation(sku); v["total_value"] != 1000.0 {
		t.Errorf("stock value %v, want 1000 (own 10 @ 100; memo 5 left out)", v["total_value"])
	}

	// 8 sold: 5 memo units at the receipt rate 80 plus 3 own units at 100.
	cart, cost := m.mustSell(sku, 8, nil)
	if cost != 300 { // the cart posts the average-cost part; the 400 of memo cost is booked on the vendor bill
		t.Errorf("sale cost %v, want 300 (3 own units; the 5 memo units are on the bill)", cost)
	}
	ids, amounts := memoBills(t, m, cart)
	if len(ids) != 1 || len(amounts) != 1 || amounts[0] != 400 {
		t.Fatalf("memo bills %v %v, want one bill of 400", ids, amounts)
	}
	if got := m.gl(ids[0], "5100"); got != 40000 {
		t.Errorf("bill cost posting Dr 5100 %d, want 40000", got)
	}
	if got := m.gl(ids[0], "2100"); got != -40000 {
		t.Errorf("bill Cr 2100 %d, want -40000", got)
	}
	m.wantBalanced(ids[0])
	if n, open := memoLotCount(t, m, sku); n != 1 || open != 0 {
		t.Errorf("after the sale lot open %v, want 0", open)
	}
	m.wantAvg(sku, 10000, "own stock keeps its rate")

	// A second memo receipt, returned in full: no debit note, no cost.
	g2 := memoReceive(t, m, vendor, sku, 4, 80)
	ret := m.id("PR")
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "stock_bucket": "Accepted", "qty": 4}})
	p := map[string]interface{}{"grn_id": g2, "reason": "unsold", "return_items": string(lines), "vendor_id": vendor, "location": m.loc}
	m.doc(ret, "PurchaseReturn", "Draft", p)
	res, err := PostPurchaseReturn(m.tenant, ret, "system", "HQ")
	if err != nil {
		t.Fatalf("PostPurchaseReturn: %v", err)
	}
	if res.DebitNoteID != "" {
		m.docs = append(m.docs, res.DebitNoteID)
		t.Errorf("memo return raised debit note %s", res.DebitNoteID)
	}
	for _, acct := range []string{"1200", "5150", "2100"} {
		if got := m.gl(ret, acct); got != 0 {
			t.Errorf("memo return posted %d paise to %s", got, acct)
		}
	}
	if _, open := memoLotCount(t, m, sku); open != 0 {
		t.Errorf("open memo units %v after the return, want 0", open)
	}
	_ = own
}

// With the module off the receipt is refused, the field is hidden, and a plain
// receipt, sale and return post exactly as before.
func TestStage14816ModuleOffChangesNothing(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, false)
	vendor := memoVendor(m)
	payload := map[string]interface{}{"memo_stock": "Yes", "vendor": vendor}
	if err := checkMemoGRN(m.tenant, payload); err == nil || t1481Code(err) != memoOffCode {
		t.Fatalf("memo receipt with the module off: %v, want %s", err, memoOffCode)
	}
	meta, err := GetDocTypeMeta(m.tenant, "GRN")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range meta {
		if f.Fieldname == "memo_stock" {
			t.Errorf("Memo stock field visible with the module off")
		}
	}
	sku := m.item(nil)
	grn, _ := m.receive(sku, 10, 100)
	if got := m.gl(grn, "1200"); got != 100000 {
		t.Errorf("plain receipt Dr 1200 %d, want 100000", got)
	}
	cart, cost := m.mustSell(sku, 4, nil)
	if cost != 400 {
		t.Errorf("plain sale cost %v, want 400", cost)
	}
	if ids, _ := memoBills(t, m, cart); len(ids) != 0 {
		t.Errorf("a plain sale raised memo bills %v", ids)
	}
	m.wantAvg(sku, 10000, "plain stock")
}

// A memo receipt needs a vendor; a sale that drew memo stock cannot be cancelled.
func TestStage14816Refusals(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, true)
	if err := checkMemoGRN(m.tenant, map[string]interface{}{"memo_stock": "Yes"}); t1481Code(err) != memoVendorCode {
		t.Errorf("no vendor: %v, want %s", err, memoVendorCode)
	}
	if err := checkMemoGRN(m.tenant, map[string]interface{}{"memo_stock": "Yes", "vendor": "V1"}); err != nil {
		t.Errorf("with a vendor: %v", err)
	}
	if err := checkMemoGRN(m.tenant, map[string]interface{}{"memo_stock": "No"}); t1481Code(err) != memoOffCode {
		t.Errorf("bad value: %v", err)
	}
	vendor := memoVendor(m)
	sku := m.item(nil)
	memoReceive(t, m, vendor, sku, 3, 50)
	cart, _ := m.mustSell(sku, 3, nil)
	if _, err := CancelPOSBill(m.tenant, RoleStoreManager, "system", cart, m.loc, "changed mind"); t1481Code(err) != memoCancelCode {
		t.Errorf("till cancel after a memo sale: %v, want %s", err, memoCancelCode)
	}
	// Selling the units, then returning more than the lot holds.
	g2 := memoReceive(t, m, vendor, sku, 2, 50)
	m.mustSell(sku, 1, nil)
	ret := m.id("PR")
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "stock_bucket": "Accepted", "qty": 2}})
	m.doc(ret, "PurchaseReturn", "Draft", map[string]interface{}{"grn_id": g2, "reason": "x", "return_items": string(lines), "vendor_id": vendor, "location": m.loc})
	if _, err := PostPurchaseReturn(m.tenant, ret, "system", "HQ"); t1481Code(err) != memoReturnCode {
		t.Errorf("return of sold memo units: %v, want %s", err, memoReturnCode)
	}
}

// A plain SKU with no lots keeps all its stock in the average, with the module
// off, and a vendor bill above the estimate still reaches 1200 and the average.
func TestStage14816PlainSKUKeepsItsStockInTheAverage(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, false)
	withSelfApproval(t, m.tenant, SelfApprovalNobody)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	if got, err := stockInHandForCost(db.DB, m.schema, sku); err != nil || got != 10 {
		t.Fatalf("stock in hand for cost %v (%v), want 10 with no memo lots", got, err)
	}
	m.mustSell(sku, 6, nil)
	if got, _ := stockInHandForCost(db.DB, m.schema, sku); got != 4 {
		t.Fatalf("stock in hand for cost %v after the sale, want 4", got)
	}
	bill := m.bill(po, grn, 1500, nil)
	if res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 2); err != nil || res.Matched {
		t.Fatalf("held: %v %+v", err, res)
	}
	if _, pending, err := PayVendorInvoice(m.tenant, bill, "manager1", "Store Manager", "confirmed"); err != nil || !pending {
		t.Fatalf("override: pending %v %v", pending, err)
	}
	if err := DecideApproval(m.tenant, "VendorInvoice", bill, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := FinalizeVendorInvoiceOverridePayment(m.tenant, bill, "admin"); err != nil {
		t.Fatal(err)
	}
	if a, v := m.gl(bill, "1200"), m.gl(bill, "5195"); a != 20000 || v != 30000 {
		t.Errorf("1200 %d, 5195 %d, want 20000 and 30000", a, v)
	}
	m.wantAvg(sku, 15000, "4 in hand take Rs 200")
}

// A customer return of a sale that drew memo stock: the units come back as own
// stock at the booked memo rate, the vendor bill stands, and the books balance.
func TestStage14816CustomerReturnOfMemoSale(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, true)
	vendor := memoVendor(m)
	sku := m.item(nil)
	memoReceive(t, m, vendor, sku, 5, 80)
	cart, _ := m.mustSell(sku, 5, nil)
	ids, amounts := memoBills(t, m, cart)
	if len(ids) != 1 || amounts[0] != 400 {
		t.Fatalf("bills %v %v, want one of 400", ids, amounts)
	}
	ret := m.custReturn(cart, sku, "Sellable", ReturnItemInput{Qty: 2})
	m.wantBalanced(ret)
	m.wantBalanced(cart)
	if n, _ := memoBills(t, m, cart); len(n) != 1 {
		t.Errorf("a customer return changed the vendor bills: %v", n)
	}
	// The 2 units are now our own stock at 80 each; valuation must equal the GL.
	if v := m.valuation(sku); v["total_value"] != 160.0 {
		t.Errorf("stock value %v, want 160 (2 returned units at the booked 80)", v["total_value"])
	}
}

// A memo receipt whose PO carries a charge: the charge is not capitalised, so the
// return to the vendor has nothing to reverse and raises no debit note.
func TestStage14816MemoReceiptWithChargeThenReturn(t *testing.T) {
	c := newTC1488(t)
	memoModule(t, c.mc1481, true)
	code := c.code(nil)
	a := c.item(nil)
	vendor := memoVendor(c.mc1481)
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(grn, po string) {
		c.onDoc(po, row(code, 200))
		if _, err := db.DB.Exec(`UPDATE `+c.schema+`.documents SET data = data || $2::jsonb WHERE id = $1`, grn,
			`{"memo_stock":"Yes","vendor":"`+vendor+`"}`); err != nil {
			t.Fatal(err)
		}
	})
	for _, acct := range []string{"1200", "2100", "5150"} {
		if got := c.gl(grn, acct); got != 0 {
			t.Errorf("memo receipt posted %d to %s", got, acct)
		}
	}
	if v := c.valuation(a); v["total_value"] != 0.0 {
		t.Errorf("valuation %v, want 0", v["total_value"])
	}
	ret, res := c.purchaseReturn(grn, a, 10)
	if res.DebitNoteID != "" {
		t.Errorf("debit note %s raised for a memo return", res.DebitNoteID)
	}
	for _, acct := range []string{"1200", "2100", "5150"} {
		if got := c.gl(ret, acct); got != 0 {
			t.Errorf("memo return posted %d to %s", got, acct)
		}
	}
}

// A serial or batch tracked item cannot be received as memo stock: its pieces are
// costed one by one, which the zero-value lot cannot carry. The receipt is refused
// whole (nothing posts) and the same item still receives as ordinary stock.
func TestStage14816SerialAndBatchItemsRefusedOnMemo(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, true)
	vendor := memoVendor(m)
	for _, mode := range []string{TrackingSerial, TrackingBatch} {
		sku := m.item(map[string]interface{}{"tracking_mode": mode})
		extra := map[string]interface{}{"serial_numbers": []interface{}{m.id("S")}}
		if mode == TrackingBatch {
			extra = map[string]interface{}{"batch_no": m.id("B")}
		}
		lines := []mc1481Line{{sku: sku, qty: 1, rate: 80, extra: extra}}
		grnID, _ := m.grnDocs(m.loc, lines)
		if _, err := db.DB.Exec(`UPDATE `+m.schema+`.documents SET data = data || $2::jsonb WHERE id = $1`, grnID,
			`{"memo_stock":"Yes","vendor":"`+vendor+`"}`); err != nil {
			t.Fatal(err)
		}
		if err := m.postGRN(m.loc, grnID, lines); t1481Code(err) != memoTrackedCode {
			t.Errorf("%s memo receipt: want %s, got %v", mode, memoTrackedCode, err)
		}
		if n, _ := memoLotCount(t, m, sku); n != 0 {
			t.Errorf("%s: %d lots left by a refused receipt", mode, n)
		}
		if got := m.onHand(sku); got != 0 {
			t.Errorf("%s: refused receipt left %v on hand", mode, got)
		}
		if got := m.gl(grnID, "1200"); got != 0 {
			t.Errorf("%s: refused receipt posted %d", mode, got)
		}
	}
}

// The migration writes no per-client row: a tenant with none reads as off, the
// module screen still lists it, and it switches on and off again.
func TestStage14816NoEntitlementRowReadsOff(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, false) // restores the tenant's own state at the end
	if _, err := db.DB.Exec(`DELETE FROM ` + m.schema + `.module_entitlements WHERE module_key = 'memo_stock'`); err != nil {
		t.Fatal(err)
	}
	if on, err := IsModuleEnabled(m.tenant, ModuleMemoStock); err != nil || on {
		t.Fatalf("no row: enabled %v err %v, want off", on, err)
	}
	listed := func() (found, enabled bool) {
		list, err := ListModuleEntitlements(m.tenant)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			if e.ModuleKey == ModuleMemoStock {
				return true, e.Enabled
			}
		}
		return false, false
	}
	if found, en := listed(); !found || en {
		t.Fatalf("module screen: listed %v enabled %v, want listed and off", found, en)
	}
	for _, want := range []bool{true, false, true, false} {
		if err := SetModuleEntitlement(m.tenant, ModuleMemoStock, want, "test"); err != nil {
			t.Fatalf("switch to %v: %v", want, err)
		}
		if on, _ := IsModuleEnabled(m.tenant, ModuleMemoStock); on != want {
			t.Fatalf("after switching to %v it reads %v", want, on)
		}
		if _, en := listed(); en != want {
			t.Fatalf("module screen shows %v, want %v", en, want)
		}
	}
}

// A credit note on an invoice that drew memo stock gives money back only: it moves
// no stock, no cost and no vendor bill. A note that names a piece the invoice did
// not sell is refused (FIN-0851) and changes nothing. Memo items cannot be
// serial tracked, so a note never has a memo piece to bring back.
func TestStage14816CreditNoteOnMemoSale(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, true)
	vendor := memoVendor(m)
	sku := m.item(nil)
	m.receive(sku, 10, 100)
	memoReceive(t, m, vendor, sku, 5, 80)
	invoice := m.salesInvoice(map[string]interface{}{"sku": sku, "qty": 8, "rate": 1000, "amount": 8000})
	ids, amounts := memoBills(t, m, invoice)
	if len(ids) != 1 || amounts[0] != 400 {
		t.Fatalf("bills %v %v, want one of 400", ids, amounts)
	}
	avg, hand := m.avg(sku), m.onHand(sku)
	_, openBefore := memoLotCount(t, m, sku)
	valueBefore := m.valuation(sku)["total_value"]

	note := func(extra map[string]interface{}) string {
		id := m.id("CN")
		data := map[string]interface{}{"note_number": id, "amount": 100.0, "reason": "price", "sales_invoice_id": invoice, "return_location": m.loc}
		for k, v := range extra {
			data[k] = v
		}
		m.doc(id, "CreditNote", "Draft", data)
		return id
	}
	same := func(why string) {
		t.Helper()
		if m.avg(sku) != avg || m.onHand(sku) != hand {
			t.Errorf("%s: average %d on hand %v, was %d and %v", why, m.avg(sku), m.onHand(sku), avg, hand)
		}
		if _, open := memoLotCount(t, m, sku); open != openBefore {
			t.Errorf("%s: memo open %v, was %v", why, open, openBefore)
		}
		if got := m.valuation(sku)["total_value"]; got != valueBefore {
			t.Errorf("%s: value %v, was %v", why, got, valueBefore)
		}
		if b, _ := memoBills(t, m, invoice); len(b) != 1 {
			t.Errorf("%s: %d vendor bills", why, len(b))
		}
	}
	cn := note(nil)
	if _, err := PostCreditNote(m.tenant, cn, "system"); err != nil {
		t.Fatalf("PostCreditNote: %v", err)
	}
	for _, acct := range []string{"1200", "5100"} {
		if got := m.gl(cn, acct); got != 0 {
			t.Errorf("money-only note posted %d to %s", got, acct)
		}
	}
	m.wantBalanced(cn)
	same("money-only note")

	bad := note(map[string]interface{}{"serial_no": m.id("S")})
	if _, err := PostCreditNote(m.tenant, bad, "system"); t1481Code(err) != "FIN-0851" {
		t.Errorf("a piece the invoice did not sell: want FIN-0851, got %v", err)
	}
	same("refused note")
}
