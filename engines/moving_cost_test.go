package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stage 148.1: cost of goods follows the stock in hand. Every scenario uses
// its own item codes, documents and location in the shared test database, so
// no figure depends on another test or on a previous run.

type mc1481 struct {
	t              *testing.T
	tenant, schema string
	run, loc       string
	seq            int
	docs, skus     []string
}

func newMC1481(t *testing.T) *mc1481 {
	t.Helper()
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	m := &mc1481{t: t, tenant: "default", schema: schema, run: fmt.Sprintf("%x", time.Now().UnixNano())}
	m.loc = "L1481-" + m.run
	m.doc(m.loc, "Location", "Active", map[string]interface{}{"code": m.loc, "name": m.loc, "type": "Store", "sellable": "Yes"})
	t.Cleanup(m.cleanup)
	return m
}

func (m *mc1481) cleanup() {
	for _, id := range m.docs {
		db.DB.Exec("DELETE FROM "+m.schema+".gl_postings WHERE document_id = $1", id)
		db.DB.Exec("DELETE FROM "+m.schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", id)
		db.DB.Exec("DELETE FROM "+m.schema+".documents WHERE id = $1", id)
	}
	for _, sku := range m.skus {
		db.DB.Exec("DELETE FROM "+m.schema+".documents WHERE doctype IN ('StockLedgerEntry') AND data->>'item_id' = $1", sku)
		db.DB.Exec("DELETE FROM "+m.schema+".documents WHERE doctype IN ('SerialNumber','Batch') AND data->>'item' = $1", sku)
		db.DB.Exec("DELETE FROM "+m.schema+".item_cost WHERE item_code = $1", sku)
		db.DB.Exec("DELETE FROM "+m.schema+".inventory_availability WHERE sku = $1", sku)
	}
}

func (m *mc1481) id(prefix string) string {
	m.seq++
	return fmt.Sprintf("T1481-%s-%s-%d", prefix, m.run, m.seq)
}

func (m *mc1481) doc(id, doctype, status string, data map[string]interface{}) {
	m.t.Helper()
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+m.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
		m.t.Fatalf("insert %s: %v", id, err)
	}
	m.docs = append(m.docs, id)
}

func (m *mc1481) item(extra map[string]interface{}) string {
	m.t.Helper()
	sku := m.id("I")
	data := map[string]interface{}{"code": sku, "name": sku + " name", "hsn_code": "7113", "tax_treatment": "Exempt"}
	for k, v := range extra {
		data[k] = v
	}
	m.doc(sku, "Item", "Active", data)
	m.skus = append(m.skus, sku)
	return sku
}

// receive raises a PO and a GRN at rate rupees and posts the receipt at loc.
func (m *mc1481) receiveAt(loc, sku string, qty, rate float64, line map[string]interface{}) (grnID, poID string) {
	m.t.Helper()
	grnID, poID = m.grnDocs(loc, []mc1481Line{{sku: sku, qty: qty, rate: rate, extra: line}})
	if err := m.postGRN(loc, grnID, []mc1481Line{{sku: sku, qty: qty, rate: rate, extra: line}}); err != nil {
		m.t.Fatalf("receive %s: %v", grnID, err)
	}
	return grnID, poID
}

func (m *mc1481) receive(sku string, qty, rate float64) (grnID, poID string) {
	m.t.Helper()
	return m.receiveAt(m.loc, sku, qty, rate, nil)
}

type mc1481Line struct {
	sku       string
	qty, rate float64
	extra     map[string]interface{}
}

func (l mc1481Line) posted() map[string]interface{} {
	out := map[string]interface{}{"sku": l.sku, "qty": l.qty, "accepted_qty": l.qty}
	for k, v := range l.extra {
		out[k] = v
	}
	return out
}

func (m *mc1481) grnDocs(loc string, lines []mc1481Line) (grnID, poID string) {
	m.t.Helper()
	poID, grnID = m.id("PO"), m.id("GRN")
	var poLines, grnLines []map[string]interface{}
	total := 0.0
	for _, l := range lines {
		poLines = append(poLines, map[string]interface{}{"sku": l.sku, "qty": l.qty, "rate": l.rate})
		grnLines = append(grnLines, l.posted())
		total += l.qty * l.rate
	}
	poRaw, _ := json.Marshal(poLines)
	grnRaw, _ := json.Marshal(grnLines)
	m.doc(poID, "PurchaseOrder", "Approved", map[string]interface{}{"code": poID, "gst_mode": GSTModeExclusive,
		"total_amount": total, "grand_total": total, "items": string(poRaw)})
	m.doc(grnID, "GRN", "Received", map[string]interface{}{"code": grnID, "po_id": poID, "location": loc, "received_items": string(grnRaw)})
	return grnID, poID
}

func (m *mc1481) postGRN(loc, grnID string, lines []mc1481Line) error {
	items := make([]interface{}, 0, len(lines))
	for _, l := range lines {
		items = append(items, l.posted())
	}
	_, err := PostGRNReceiptWithQC(m.tenant, loc, items, "system", grnID)
	return err
}

// sell rings a real till sale and returns the cart id and the cost it booked.
func (m *mc1481) sell(sku string, qty int, extra map[string]interface{}) (cartID string, costRupees float64, err error) {
	m.t.Helper()
	cartID = m.id("CART")
	line := map[string]interface{}{"sku": sku, "qty": qty, "sale_price": 1000.0}
	for k, v := range extra {
		line[k] = v
	}
	m.doc(cartID, "POSCart", "Processing", map[string]interface{}{"location": m.loc, "payment_mode": "Cash", "items": []map[string]interface{}{line}})
	_, costRupees, err = FinalizePOSCheckout(m.tenant, cartID, "")
	return cartID, costRupees, err
}

func (m *mc1481) mustSell(sku string, qty int, extra map[string]interface{}) (string, float64) {
	m.t.Helper()
	cart, cost, err := m.sell(sku, qty, extra)
	if err != nil {
		m.t.Fatalf("sell %s: %v", sku, err)
	}
	return cart, cost
}

func (m *mc1481) avg(sku string) int64 {
	m.t.Helper()
	got, _, err := GetItemUnitCost(m.tenant, sku)
	if err != nil {
		m.t.Fatalf("GetItemUnitCost: %v", err)
	}
	return got
}

func (m *mc1481) wantAvg(sku string, want int64, why string) {
	m.t.Helper()
	if got := m.avg(sku); got != want {
		m.t.Errorf("%s: average %d paise, want %d", why, got, want)
	}
}

// gl is debit minus credit on one account for one document.
func (m *mc1481) gl(docID, account string) int64 {
	m.t.Helper()
	var net int64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit - credit), 0) FROM "+m.schema+".gl_postings WHERE document_id = $1 AND account_code = $2", docID, account).Scan(&net); err != nil {
		m.t.Fatalf("gl %s %s: %v", docID, account, err)
	}
	return net
}

func (m *mc1481) wantBalanced(docID string) {
	m.t.Helper()
	var net int64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit - credit), 0) FROM "+m.schema+".gl_postings WHERE document_id = $1", docID).Scan(&net); err != nil {
		m.t.Fatalf("balance %s: %v", docID, err)
	}
	if net != 0 {
		m.t.Errorf("%s is out of balance by %d paise", docID, net)
	}
}

func (m *mc1481) pieceData(doctype, field, no string) map[string]interface{} {
	m.t.Helper()
	var raw []byte
	if err := db.DB.QueryRow("SELECT data FROM "+m.schema+".documents WHERE doctype = $1 AND data->>'"+field+"' = $2", doctype, no).Scan(&raw); err != nil {
		m.t.Fatalf("%s %s: %v", doctype, no, err)
	}
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	return d
}

func (m *mc1481) valuation(sku string) map[string]interface{} {
	m.t.Helper()
	rows, err := GetInventoryValuation(m.tenant)
	if err != nil {
		m.t.Fatalf("GetInventoryValuation: %v", err)
	}
	for _, r := range rows {
		if r["sku"] == sku {
			return r
		}
	}
	m.t.Fatalf("%s is not in the valuation", sku)
	return nil
}

func (m *mc1481) bill(poID, grnID string, amount float64, extra map[string]interface{}) string {
	m.t.Helper()
	id := m.id("VI")
	data := map[string]interface{}{"id": id, "invoice_number": id, "vendor_id": m.id("V"), "po_id": poID, "grn_id": grnID,
		"invoice_amount": amount, "financial_year": "T1481", "status": "Draft"}
	for k, v := range extra {
		data[k] = v
	}
	m.doc(id, "VendorInvoice", "Draft", data)
	return id
}

func (m *mc1481) custReturn(cartID, sku string, disposition string, input ReturnItemInput) string {
	m.t.Helper()
	input.SKU = sku
	if input.Qty == 0 {
		input.Qty = 1
	}
	id, err := CreateReturnRequest(m.tenant, "Customer Return", m.loc, cartID, "", "t1481", []ReturnItemInput{input})
	if err != nil {
		m.t.Fatalf("CreateReturnRequest: %v", err)
	}
	m.docs = append(m.docs, id)
	if err := ApproveReturnRequest(m.tenant, id, "t1481-a"); err != nil {
		m.t.Fatalf("ApproveReturnRequest: %v", err)
	}
	if err := ReceiveReturnRequest(m.tenant, id, "t1481-w"); err != nil {
		m.t.Fatalf("ReceiveReturnRequest: %v", err)
	}
	if _, refundID, err := ApplyReturnQC(m.tenant, id, map[string]string{sku: disposition}, "t1481-q"); err != nil {
		m.t.Fatalf("ApplyReturnQC: %v", err)
	} else if refundID != "" {
		m.docs = append(m.docs, refundID)
	}
	return id
}

func t1481Code(err error) string {
	var verr *ValidationError
	if errors.As(err, &verr) {
		return verr.Code
	}
	return ""
}

// The required example: 1,000 @ Rs 100 received and sold, then 10 @ Rs 200
// received. The lifetime average gave Rs 100.99; the stock in hand is 10 at Rs 200.
func TestStage1481HundredThenTwoHundredCostsTwoHundred(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	m.receive(sku, 1000, 100)
	if _, cost := m.mustSell(sku, 1000, nil); cost != 100000 {
		t.Fatalf("the first 1,000 cost Rs %.2f, want Rs 100000", cost)
	}
	m.receive(sku, 10, 200)
	m.wantAvg(sku, 20000, "10 @ Rs 200 are all that is in hand")
	if _, cost := m.mustSell(sku, 1, nil); cost != 200 {
		t.Errorf("the next sale cost Rs %.2f, want Rs 200", cost)
	}
	// The lifetime figures are still kept and still written.
	var cumQty float64
	var cumValue int64
	if err := db.DB.QueryRow("SELECT cumulative_qty_received, cumulative_value_received_paise FROM "+m.schema+".item_cost WHERE item_code = $1", sku).Scan(&cumQty, &cumValue); err != nil {
		t.Fatal(err)
	}
	if cumQty != 1010 || cumValue != 1000*10000+10*20000 {
		t.Errorf("lifetime figures %v / %d", cumQty, cumValue)
	}
}

// Stock still in hand keeps its value in the blend; stock sold leaves it.
func TestStage1481BlendsOverStockInHandOnly(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	m.receive(sku, 10, 100)
	m.receive(sku, 10, 200)
	m.wantAvg(sku, 15000, "10 @ 100 + 10 @ 200")
	m.mustSell(sku, 10, nil)
	m.receive(sku, 10, 300)
	m.wantAvg(sku, 22500, "10 left at 150 + 10 @ 300 (a lifetime average says 200)")
	v := m.valuation(sku)
	if v["total_value"] != 4500.0 || v["qty_on_hand"] != int64(20) {
		t.Errorf("valuation %+v, want 20 worth Rs 4500", v)
	}
	if v["rate_source"] != "Average in hand" {
		t.Errorf("rate source %v", v["rate_source"])
	}
}

// Goods on a transfer have left the store but are still owned, so moving them
// never moves the average, even when a receipt lands while they are on the road.
func TestStage1481TransferStaysCostNeutral(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	to := "L1481B-" + m.run
	m.doc(to, "Location", "Active", map[string]interface{}{"code": to, "name": to, "type": "Store", "sellable": "Yes"})
	m.receive(sku, 10, 100)
	// Sold first so a lifetime average and an in-hand average disagree.
	m.mustSell(sku, 2, nil)

	toID := m.id("TO")
	items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 4}})
	m.doc(toID, "TransferOrder", "Approved", map[string]interface{}{"id": toID, "transfer_number": toID, "from_warehouse": m.loc, "to_warehouse": to, "status": "Approved", "items": string(items)})
	if err := DispatchTransferOrder(m.tenant, toID, "system"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	m.wantAvg(sku, 10000, "dispatch")
	// 8 held (4 here, 4 on the road) @ 100 + 8 @ 200 = 150. Counting only the
	// shelf would give (4x100 + 1600) / 12 = 166.67.
	m.receive(sku, 8, 200)
	m.wantAvg(sku, 15000, "a receipt while 4 are on the road")
	if err := ReceiveTransferOrder(m.tenant, toID, "system", []interface{}{map[string]interface{}{"sku": sku, "qty": 4}}); err != nil {
		t.Fatalf("receive: %v", err)
	}
	m.wantAvg(sku, 15000, "transfer receipt")
}

// On hand at or below zero: the next receipt's rate is the average. No divide
// by zero, no negative average, and uncosted stock does not dilute a first cost.
func TestStage1481ZeroAndNegativeStockTakeTheNextRate(t *testing.T) {
	t.Run("negative", func(t *testing.T) {
		m := newMC1481(t)
		sku := m.item(nil)
		m.receive(sku, 10, 100)
		if _, err := db.DB.Exec("UPDATE "+m.schema+".inventory_availability SET on_hand = -5, available = -5 WHERE sku = $1", sku); err != nil {
			t.Fatal(err)
		}
		m.receive(sku, 10, 200)
		m.wantAvg(sku, 20000, "stock crossed zero")
	})
	t.Run("uncosted opening stock", func(t *testing.T) {
		m := newMC1481(t)
		sku := m.item(nil)
		if _, err := PostInventoryLedger(m.tenant, m.loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 100.0}}, false); err != nil {
			t.Fatal(err)
		}
		m.receive(sku, 10, 200)
		m.wantAvg(sku, 20000, "first ever cost")
	})
}

// Rejected and damaged units of a GRN were never valued, so they must not dilute
// the next receipt. A blend over raw on hand would give Rs 10.90 here.
func TestStage1481RejectedGRNUnitsStayOutOfThePool(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	lines := []mc1481Line{{sku: sku, qty: 200, rate: 100, extra: map[string]interface{}{"accepted_qty": 100.0, "rejected_qty": 100.0, "rejection_reason": "scratched"}}}
	grn, _ := m.grnDocs(m.loc, lines)
	if err := m.postGRN(m.loc, grn, lines); err != nil {
		t.Fatal(err)
	}
	m.wantAvg(sku, 10000, "accepted part")
	m.mustSell(sku, 100, nil)
	m.receive(sku, 10, 20)
	m.wantAvg(sku, 2000, "only the new 10 are valued")
}

// A late charge on 10 units with 6 already sold: 4 units' share raises the cost
// in hand, the 6 units' share goes to COGS variance now.
func TestStage1481LateChargeSplitsOnHandAndSold(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, _ := m.receive(sku, 10, 100)
	m.mustSell(sku, 6, nil)
	voucher, err := CreateLandedCostVoucher(m.tenant, grn, []map[string]interface{}{{"charge_type": "Freight", "amount": 100.0}}, "system")
	if err != nil {
		t.Fatalf("CreateLandedCostVoucher: %v", err)
	}
	m.docs = append(m.docs, voucher)
	if err := ApplyLandedCostVoucher(m.tenant, voucher, "system"); err != nil {
		t.Fatalf("ApplyLandedCostVoucher: %v", err)
	}
	m.wantAvg(sku, 11000, "4 in hand take Rs 40")
	if got := m.gl(voucher, "1200"); got != 4000 {
		t.Errorf("1200 Dr %d, want 4000", got)
	}
	if got := m.gl(voucher, "5195"); got != 6000 {
		t.Errorf("5195 Dr %d, want 6000", got)
	}
	if got := m.gl(voucher, "2110"); got != -10000 {
		t.Errorf("2110 Cr %d, want 10000", -got)
	}
	m.wantBalanced(voucher)
	// Stock value agrees: 4 x Rs 110.
	if v := m.valuation(sku); v["total_value"] != 440.0 {
		t.Errorf("valuation %v", v["total_value"])
	}
}

// With nothing sold the whole charge goes on the stock, exactly as today.
func TestStage1481LateChargeNothingSoldAllOnStock(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, _ := m.receive(sku, 10, 100)
	voucher, err := CreateLandedCostVoucher(m.tenant, grn, []map[string]interface{}{{"charge_type": "Freight", "amount": 200.0}}, "system")
	if err != nil {
		t.Fatal(err)
	}
	m.docs = append(m.docs, voucher)
	if err := ApplyLandedCostVoucher(m.tenant, voucher, "system"); err != nil {
		t.Fatal(err)
	}
	m.wantAvg(sku, 12000, "all 10 in hand")
	if got := m.gl(voucher, "1200"); got != 20000 {
		t.Errorf("1200 Dr %d", got)
	}
	if got := m.gl(voucher, "5195"); got != 0 {
		t.Errorf("5195 %d, want none", got)
	}
}

// A bill higher than the GRN, 6 of 10 sold: same split, posted when it matches.
// Matching it again never posts twice, and a corrected bill takes it back.
func TestStage1481BillDifferenceSplitsAtMatch(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	m.mustSell(sku, 6, nil)
	bill := m.bill(po, grn, 1100, nil)

	res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 20)
	if err != nil || !res.Matched {
		t.Fatalf("match: %v %+v", err, res)
	}
	m.wantAvg(sku, 11000, "4 in hand take Rs 40")
	if a, v, s := m.gl(bill, "1200"), m.gl(bill, "5195"), m.gl(bill, "2100"); a != 4000 || v != 6000 || s != -10000 {
		t.Errorf("1200 %d, 5195 %d, 2100 %d", a, v, s)
	}
	m.wantBalanced(bill)
	if res.Details["cost_difference"] == nil {
		t.Errorf("the match does not say what it did: %v", res.Details)
	}

	if _, err := MatchVendorInvoice(m.tenant, bill, po, grn, 20); err != nil {
		t.Fatal(err)
	}
	if a, v := m.gl(bill, "1200"), m.gl(bill, "5195"); a != 4000 || v != 6000 {
		t.Errorf("a second match posted again: 1200 %d, 5195 %d", a, v)
	}
	m.wantAvg(sku, 11000, "second match")

	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = jsonb_set(data, '{invoice_amount}', '1000') WHERE id = $1", bill); err != nil {
		t.Fatal(err)
	}
	if _, err := MatchVendorInvoice(m.tenant, bill, po, grn, 20); err != nil {
		t.Fatal(err)
	}
	if a, v, s := m.gl(bill, "1200"), m.gl(bill, "5195"), m.gl(bill, "2100"); a != 0 || v != 0 || s != 0 {
		t.Errorf("after the correction: 1200 %d, 5195 %d, 2100 %d, want all 0", a, v, s)
	}
	m.wantAvg(sku, 10000, "corrected bill")
}

func TestStage1481BillEqualToGRNOrHeldPostsNothing(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	equal := m.bill(po, grn, 1000, nil)
	if res, err := MatchVendorInvoice(m.tenant, equal, po, grn, 2); err != nil || !res.Matched {
		t.Fatalf("match: %v %+v", err, res)
	}
	if n := m.gl(equal, "1200"); n != 0 {
		t.Errorf("an equal bill posted %d", n)
	}
	m.wantAvg(sku, 10000, "equal bill")

	m2sku := m.item(nil)
	grn2, po2 := m.receive(m2sku, 10, 100)
	held := m.bill(po2, grn2, 1500, nil)
	if res, err := MatchVendorInvoice(m.tenant, held, po2, grn2, 2); err != nil || res.Matched {
		t.Fatalf("held: %v %+v", err, res)
	}
	if n := m.gl(held, "1200"); n != 0 {
		t.Errorf("a held bill posted %d", n)
	}
	m.wantAvg(m2sku, 10000, "held bill")
}

// Per-line match: one line over-billed and one under-billed add up to the GRN,
// which the whole-bill rule waves through. Named lines catch it; a line under
// its item type's tolerance passes; under-billing never holds.
func TestStage1481PerLineMatchHoldsAnOverBilledLine(t *testing.T) {
	m := newMC1481(t)
	typeName := m.id("TYPE")
	m.doc(typeName, "ItemType", "Active", map[string]interface{}{"code": typeName, "name": typeName, "bill_price_tolerance_percent": 40.0})
	InvalidateItemTypeCache(m.tenant)
	t.Cleanup(func() { InvalidateItemTypeCache(m.tenant) })
	a, b, c := m.item(nil), m.item(nil), m.item(map[string]interface{}{"item_type": typeName})
	lines := []mc1481Line{{sku: a, qty: 10, rate: 100}, {sku: b, qty: 10, rate: 100}, {sku: c, qty: 10, rate: 100}}
	grn, po := m.grnDocs(m.loc, lines)
	if err := m.postGRN(m.loc, grn, lines); err != nil {
		t.Fatal(err)
	}
	billLines := func(ra, rb, rc float64) string {
		raw, _ := json.Marshal([]map[string]interface{}{{"sku": a, "qty": 10, "rate": ra}, {"sku": b, "qty": 10, "rate": rb}, {"sku": c, "qty": 10, "rate": rc}})
		return string(raw)
	}

	// Without named lines the whole bill matches, as today.
	plain := m.bill(po, grn, 3000, nil)
	if res, err := MatchVendorInvoice(m.tenant, plain, po, grn, 2); err != nil || !res.Matched {
		t.Fatalf("whole bill: %v %+v", err, res)
	}
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = jsonb_set(data, '{status}', '\"Cancelled\"'), status = 'Cancelled' WHERE id = $1", plain); err != nil {
		t.Fatal(err)
	}

	over := m.bill(po, grn, 3000, map[string]interface{}{"bill_lines": billLines(130, 70, 100)})
	res, err := MatchVendorInvoice(m.tenant, over, po, grn, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched || !strings.Contains(strings.Join(res.Reasons, " | "), a) {
		t.Fatalf("line a is 30%% over and must hold the bill naming it: %+v", res)
	}
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = jsonb_set(data, '{status}', '\"Cancelled\"'), status = 'Cancelled' WHERE id = $1", over); err != nil {
		t.Fatal(err)
	}

	// c is 30% over, inside its item type's 40%; b is under, which never holds.
	within := m.bill(po, grn, 3000, map[string]interface{}{"bill_lines": billLines(100, 70, 130)})
	res, err = MatchVendorInvoice(m.tenant, within, po, grn, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Matched {
		t.Fatalf("within the item type's tolerance: %+v", res)
	}

	wrongSum := m.bill(po, grn, 3500, map[string]interface{}{"bill_lines": billLines(100, 100, 100)})
	res, err = MatchVendorInvoice(m.tenant, wrongSum, po, grn, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched {
		t.Fatalf("lines that do not add up to the bill must hold it: %+v", res)
	}
}

// Bullion: the billed grams are checked against the grams received, over only.
func TestStage1481WeightToleranceHoldsOverWeightBill(t *testing.T) {
	m := newMC1481(t)
	vendor := m.id("VEND")
	m.doc(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": vendor, "bill_weight_tolerance_g": 2.0})
	sku := m.item(nil)
	lines := []mc1481Line{{sku: sku, qty: 10, rate: 100, extra: map[string]interface{}{"actual_weight": 100.0, "weight_uom": "g"}}}
	grn, po := m.grnDocs(m.loc, lines)
	if err := m.postGRN(m.loc, grn, lines); err != nil {
		t.Fatal(err)
	}
	bl := func(weight float64) string {
		raw, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 10, "rate": 100, "weight_g": weight}})
		return string(raw)
	}
	heavy := m.bill(po, grn, 1000, map[string]interface{}{"vendor_id": vendor, "bill_lines": bl(103)})
	res, err := MatchVendorInvoice(m.tenant, heavy, po, grn, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched {
		t.Fatalf("3 g over a 2 g tolerance must hold: %+v", res)
	}
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = jsonb_set(data, '{status}', '\"Cancelled\"'), status = 'Cancelled' WHERE id = $1", heavy); err != nil {
		t.Fatal(err)
	}
	close := m.bill(po, grn, 1000, map[string]interface{}{"vendor_id": vendor, "bill_lines": bl(101.5)})
	if res, err = MatchVendorInvoice(m.tenant, close, po, grn, 2); err != nil || !res.Matched {
		t.Fatalf("1.5 g over a 2 g tolerance: %v %+v", err, res)
	}
}

// Valuation = on hand x rate for bulk (above), and sums own costs for pieces.
// The named sale takes the piece's own cost, not the mean, and a return puts it back.
func TestStage1481SerialPieceSellsAndReturnsAtItsOwnCost(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(map[string]interface{}{"tracking_mode": TrackingSerial})
	s1, s2 := m.id("S"), m.id("S")
	m.receiveAt(m.loc, sku, 1, 100, map[string]interface{}{"serial_numbers": []interface{}{s1}})
	m.receiveAt(m.loc, sku, 1, 300, map[string]interface{}{"serial_numbers": []interface{}{s2}})
	m.wantAvg(sku, 20000, "two pieces in hand")
	if got := numFromInterface(m.pieceData("SerialNumber", "serial_no", s2)["receipt_cost_paise"]); got != 30000 {
		t.Fatalf("piece cost stamped %v, want 30000", got)
	}
	v := m.valuation(sku)
	if v["total_value"] != 400.0 || v["rate_source"] != "Own cost" {
		t.Errorf("valuation %+v, want Rs 400 by own cost", v)
	}

	cart, cost := m.mustSell(sku, 1, map[string]interface{}{"serial_nos": []string{s2}})
	if cost != 300 {
		t.Fatalf("the piece sold at Rs %.2f, want its own Rs 300", cost)
	}
	if got := m.gl(cart, "5100"); got != 30000 {
		t.Errorf("COGS %d, want 30000", got)
	}
	m.wantAvg(sku, 10000, "the piece left, the Rs 100 piece is what is in hand")
	if st := m.pieceData("SerialNumber", "serial_no", s2)["status"]; st != SerialShipped {
		t.Errorf("sold piece status %v", st)
	}
	if v := m.valuation(sku); v["total_value"] != 100.0 {
		t.Errorf("after the sale: %+v", v)
	}

	ret := m.custReturn(cart, sku, "Sellable", ReturnItemInput{})
	if got := m.gl(ret, "1200"); got != 30000 {
		t.Errorf("the return put back %d, want the piece's own 30000", got)
	}
	if got := m.gl(ret, "5100"); got != -30000 {
		t.Errorf("COGS reversal %d", got)
	}
	m.wantAvg(sku, 20000, "the piece is back")
	if st := m.pieceData("SerialNumber", "serial_no", s2)["status"]; st != SerialInStock {
		t.Errorf("returned piece status %v", st)
	}
	m.wantBalanced(ret)
}

// A batch carries its own receipt cost too.
func TestStage1481BatchSellsAtItsOwnCost(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(map[string]interface{}{"tracking_mode": TrackingBatch})
	b1, b2 := m.id("B"), m.id("B")
	m.receiveAt(m.loc, sku, 2, 100, map[string]interface{}{"batch_no": b1})
	m.receiveAt(m.loc, sku, 2, 300, map[string]interface{}{"batch_no": b2})
	m.wantAvg(sku, 20000, "two batches")
	if got := numFromInterface(m.pieceData("Batch", "batch_no", b2)["receipt_cost_paise"]); got != 30000 {
		t.Fatalf("batch cost stamped %v", got)
	}
	if v := m.valuation(sku); v["total_value"] != 800.0 || v["rate_source"] != "Own cost" {
		t.Errorf("valuation %+v", v)
	}
	cart, cost := m.mustSell(sku, 1, map[string]interface{}{"batch_no": b2})
	if cost != 300 {
		t.Fatalf("batch unit sold at Rs %.2f, want Rs 300", cost)
	}
	// (4 x 200 - 300) / 3
	m.wantAvg(sku, 16667, "the Rs 300 unit left")
	if v := m.valuation(sku); v["total_value"] != 500.0 {
		t.Errorf("own costs after the sale: %+v", v)
	}
	ret := m.custReturn(cart, sku, "Sellable", ReturnItemInput{})
	if got := m.gl(ret, "1200"); got != 30000 {
		t.Errorf("batch return %d, want 30000", got)
	}
	m.wantAvg(sku, 20000, "the unit is back")
}

// A piece that is unknown, gone, or another item's refuses the sale, and nothing moves.
func TestStage1481SaleRefusesAPieceItCannotSell(t *testing.T) {
	m := newMC1481(t)
	sku, other := m.item(map[string]interface{}{"tracking_mode": TrackingSerial}), m.item(map[string]interface{}{"tracking_mode": TrackingSerial})
	s1, s2, s3 := m.id("S"), m.id("S"), m.id("S")
	m.receiveAt(m.loc, sku, 2, 100, map[string]interface{}{"serial_numbers": []interface{}{s1, s2}})
	m.receiveAt(m.loc, other, 1, 100, map[string]interface{}{"serial_numbers": []interface{}{s3}})
	m.mustSell(sku, 1, map[string]interface{}{"serial_nos": []string{s1}})

	cases := map[string][]string{"unknown": {m.id("NOPE")}, "already sold": {s1}, "another item": {s3}, "more than the qty": {s2, s1}}
	for name, serials := range cases {
		_, _, err := m.sell(sku, 1, map[string]interface{}{"serial_nos": serials})
		if t1481Code(err) != "FIN-0850" {
			t.Errorf("%s: want FIN-0850, got %v", name, err)
		}
	}
	var onHand int
	db.DB.QueryRow("SELECT COALESCE(SUM(on_hand),0) FROM "+m.schema+".inventory_availability WHERE sku = $1", sku).Scan(&onHand)
	if onHand != 1 {
		t.Errorf("refused sales moved stock: %d", onHand)
	}
}

// A customer return reverses what the sale booked, not today's average.
func TestStage1481CustomerReturnReversesTheBookedCost(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	m.receive(sku, 1, 100)
	cart, _ := m.mustSell(sku, 1, nil)
	m.receive(sku, 1, 200)
	m.wantAvg(sku, 20000, "only the new unit in hand")
	ret := m.custReturn(cart, sku, "Sellable", ReturnItemInput{})
	if got := m.gl(ret, "1200"); got != 10000 {
		t.Errorf("the return put %d into stock, want the Rs 100 the sale booked", got)
	}
	m.wantAvg(sku, 15000, "1 @ 200 + 1 back @ 100")
	if v := m.valuation(sku); v["total_value"] != 300.0 {
		t.Errorf("valuation %+v", v)
	}
	m.wantBalanced(ret)
}

// A purchase return takes its value out of what is in hand.
func TestStage1481PurchaseReturnTakesItsValueOut(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	m.receive(sku, 10, 100)
	grn2, _ := m.receive(sku, 10, 200)
	m.mustSell(sku, 5, nil)
	ret := m.id("PR")
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "stock_bucket": "Accepted", "qty": 5}})
	vendor := m.id("VEND")
	m.doc(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": vendor})
	p := map[string]interface{}{"grn_id": grn2, "reason": "wrong size", "return_items": string(lines), "vendor_id": vendor, "location": m.loc}
	if err := validatePurchaseReturnRules(m.tenant, "", "", nil, p); err != nil {
		t.Fatalf("validate: %v", err)
	}
	m.doc(ret, "PurchaseReturn", "Draft", p)
	res, err := PostPurchaseReturn(m.tenant, ret, "system", "HQ")
	if err != nil {
		t.Fatalf("PostPurchaseReturn: %v", err)
	}
	if res.DebitNoteID != "" {
		m.docs = append(m.docs, res.DebitNoteID)
	}
	// 15 in hand @ 150 less 5 @ 200 leaves 10 worth Rs 1250. A lifetime average says 133.33.
	m.wantAvg(sku, 12500, "purchase return")
	if v := m.valuation(sku); v["total_value"] != 1250.0 {
		t.Errorf("valuation %+v", v)
	}
}

// Count gains and losses post at the current average, never the last price, and
// do not move it. Already true before this stage: this test holds it there.
func TestStage1481CountPostsAtTheAverage(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	m.receive(sku, 10, 100)
	m.receive(sku, 10, 200)
	line := m.id("CCL")
	m.doc(line, "CycleCountLine", "Approved", map[string]interface{}{"sku": sku, "location": m.loc, "variance": 2.0, "variance_reason_code": "FOUND", "status": "Approved"})
	if err := PostCycleCountAdjustment(m.tenant, line, "counter"); err != nil {
		t.Fatalf("gain: %v", err)
	}
	if got := m.gl(line, "1200"); got != 30000 {
		t.Errorf("2 found at 150 = 30000, got %d (the last price would be 40000)", got)
	}
	m.wantAvg(sku, 15000, "a count gain")
	loss := m.id("CCL")
	m.doc(loss, "CycleCountLine", "Approved", map[string]interface{}{"sku": sku, "location": m.loc, "variance": -3.0, "variance_reason_code": "MISSING", "status": "Approved"})
	if err := PostCycleCountAdjustment(m.tenant, loss, "counter"); err != nil {
		t.Fatalf("loss: %v", err)
	}
	if got := m.gl(loss, "1200"); got != -45000 {
		t.Errorf("3 lost at 150 = -45000, got %d", got)
	}
	m.wantAvg(sku, 15000, "a count loss")
}

// Two receipts of one item in parallel, at two stores: the row lock serialises
// them, so neither is lost from the blend.
func TestStage1481ParallelReceiptsBothCount(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	other := "L1481P-" + m.run
	m.doc(other, "Location", "Active", map[string]interface{}{"code": other, "name": other, "type": "Store", "sellable": "Yes"})
	m.receive(sku, 10, 100)
	m.mustSell(sku, 10, nil) // nothing in hand: a lifetime average would keep the 100
	lineA := []mc1481Line{{sku: sku, qty: 10, rate: 100}}
	lineB := []mc1481Line{{sku: sku, qty: 10, rate: 200}}
	grnA, _ := m.grnDocs(m.loc, lineA)
	grnB, _ := m.grnDocs(other, lineB)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = m.postGRN(m.loc, grnA, lineA) }()
	go func() { defer wg.Done(); errs[1] = m.postGRN(other, grnB, lineB) }()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("parallel receipt: %v", err)
		}
	}
	m.wantAvg(sku, 15000, "both receipts blended")
}

// The arithmetic alone: nothing divides by zero, no rate goes below zero, and a
// blend over no stock is the receipt's own rate.
func TestStage1481RateArithmeticHasNoTrapDoors(t *testing.T) {
	for _, c := range []struct {
		name        string
		held        float64
		rate, value int64
		qty         float64
		want        int64
	}{
		{"blends over what is held", 10, 10000, 200000, 10, 15000},
		{"nothing held takes the receipt", 0, 10000, 200000, 10, 20000},
		{"negative stock takes the receipt", -5, 10000, 200000, 10, 20000},
		{"no quantity keeps the rate", 10, 10000, 0, 0, 10000},
		{"no quantity, nothing at all", 0, 0, 0, 0, 0},
		{"a free receipt only dilutes", 10, 10000, 0, 10, 5000},
		{"an absurd credit stops at zero", 1, 100, -1000000, 1, 0},
		{"rounds to the nearest paisa", 3, 100, 0, 1, 75},
		{"half a paisa rounds up", 1, 1, 0, 1, 1},
	} {
		if got := blendRatePaise(c.held, c.rate, c.value, c.qty); got != c.want {
			t.Errorf("%s: blend = %d, want %d", c.name, got, c.want)
		}
	}
	if got := rateAfterTakingOutPaise(0, 10000, 5, 50000); got != 10000 {
		t.Errorf("nothing left keeps the rate, got %d", got)
	}
	if got := rateAfterTakingOutPaise(-3, 10000, 5, 50000); got != 10000 {
		t.Errorf("negative stock keeps the rate, got %d", got)
	}
	if got := rateAfterTakingOutPaise(5, 10000, 5, 75000); got != 5000 {
		t.Errorf("what stays carries what is left, got %d", got)
	}
	if got := rateAfterTakingOutPaise(1, 100, 1, 1000000); got != 0 {
		t.Errorf("taking out more than the pool stops at zero, got %d", got)
	}
	if got := rateAfterChargePaise(0, 10000, 500); got != 10000 {
		t.Errorf("a charge on nothing keeps the rate, got %d", got)
	}
	if got := rateAfterChargePaise(4, 10000, 4000); got != 11000 {
		t.Errorf("a charge over 4 units, got %d", got)
	}
	if got := rateAfterChargePaise(4, 10000, -80000); got != 0 {
		t.Errorf("a credit past the value stops at zero, got %d", got)
	}
	if got := sharePaise(10000, 0, 0); got != 0 {
		t.Errorf("share of nothing, got %d", got)
	}
	if got := sharePaise(-10000, 4, 10); got != -4000 {
		t.Errorf("a negative share keeps its sign, got %d", got)
	}
}

// salesInvoice raises and posts a one-line tax-free invoice.
func (m *mc1481) salesInvoice(line map[string]interface{}) string {
	m.t.Helper()
	id := m.id("SI")
	raw, _ := json.Marshal([]map[string]interface{}{line})
	m.doc(id, "SalesInvoice", "Draft", map[string]interface{}{"invoice_number": id, "customer": "C1481", "location": m.loc,
		"items": string(raw), "total_amount": 1000.0, "taxable_amount": 1000.0, "cgst": 0.0, "sgst": 0.0})
	if _, err := PostSalesInvoice(m.tenant, id, "system"); err != nil {
		m.t.Fatalf("PostSalesInvoice: %v", err)
	}
	return id
}

func (m *mc1481) onHand(sku string) float64 {
	m.t.Helper()
	var qty float64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(on_hand), 0) FROM "+m.schema+".inventory_availability WHERE sku = $1", sku).Scan(&qty); err != nil {
		m.t.Fatalf("on hand: %v", err)
	}
	return qty
}

// A credit note naming a sold piece brings that piece back at what the sale
// booked. A note cannot bring back what it did not sell, or bring it back twice.
func TestStage1481CreditNoteBringsThePieceBackAtItsOwnCost(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(map[string]interface{}{"tracking_mode": TrackingSerial})
	s1, s2 := m.id("S"), m.id("S")
	m.receiveAt(m.loc, sku, 1, 100, map[string]interface{}{"serial_numbers": []interface{}{s1}})
	m.receiveAt(m.loc, sku, 1, 300, map[string]interface{}{"serial_numbers": []interface{}{s2}})
	invoice := m.salesInvoice(map[string]interface{}{"sku": sku, "qty": 1, "rate": 1000, "amount": 1000, "serial_nos": []string{s2}})
	if got := m.gl(invoice, "5100"); got != 30000 {
		t.Fatalf("invoice COGS %d, want the piece's own 30000", got)
	}
	m.wantAvg(sku, 10000, "the Rs 100 piece is what is in hand")

	note := func(serial string) string {
		id := m.id("CN")
		m.doc(id, "CreditNote", "Draft", map[string]interface{}{"note_number": id, "amount": 100.0, "reason": "back", "sales_invoice_id": invoice,
			"serial_no": serial, "return_location": m.loc})
		return id
	}
	if _, err := PostCreditNote(m.tenant, note(s1), "system"); t1481Code(err) != "FIN-0851" {
		t.Fatalf("a piece this invoice did not sell: want FIN-0851, got %v", err)
	}
	if got := m.onHand(sku); got != 1 {
		t.Fatalf("the refused note moved stock: %v on hand", got)
	}
	cn := note(s2)
	if _, err := PostCreditNote(m.tenant, cn, "system"); err != nil {
		t.Fatalf("PostCreditNote: %v", err)
	}
	if got := m.gl(cn, "1200"); got != 30000 {
		t.Errorf("stock back %d, want 30000", got)
	}
	if got := m.gl(cn, "5100"); got != -30000 {
		t.Errorf("COGS reversal %d, want -30000", got)
	}
	m.wantBalanced(cn)
	m.wantAvg(sku, 20000, "two pieces in hand again")
	if got := m.onHand(sku); got != 2 {
		t.Errorf("on hand %v, want 2", got)
	}
	if st := m.pieceData("SerialNumber", "serial_no", s2)["status"]; st != SerialInStock {
		t.Errorf("piece status %v", st)
	}
	if v := m.valuation(sku); v["total_value"] != 400.0 {
		t.Errorf("valuation %+v, want the two own costs", v)
	}
	if _, err := PostCreditNote(m.tenant, note(s2), "system"); t1481Code(err) != "FIN-0851" {
		t.Errorf("the same piece twice: want FIN-0851, got %v", err)
	}
}

// A note that names no piece is the money-only note it always was.
func TestStage1481CreditNoteWithoutAPieceMovesNoStock(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	m.receive(sku, 2, 100)
	invoice := m.salesInvoice(map[string]interface{}{"sku": sku, "qty": 1, "rate": 1000, "amount": 1000})
	id := m.id("CN")
	m.doc(id, "CreditNote", "Draft", map[string]interface{}{"note_number": id, "amount": 100.0, "reason": "goodwill", "sales_invoice_id": invoice})
	if _, err := PostCreditNote(m.tenant, id, "system"); err != nil {
		t.Fatalf("PostCreditNote: %v", err)
	}
	if got := m.gl(id, "1200"); got != 0 {
		t.Errorf("stock moved by %d", got)
	}
	if got := m.onHand(sku); got != 1 {
		t.Errorf("on hand %v", got)
	}
}

// A cancelled invoice undoes what it booked: its piece goes back in stock and the
// average of the rest comes back with it, agreeing with the reversed GL.
func TestStage1481CancelledInvoiceBringsThePieceAndItsCostBack(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(map[string]interface{}{"tracking_mode": TrackingSerial})
	s1, s2 := m.id("S"), m.id("S")
	m.receiveAt(m.loc, sku, 1, 100, map[string]interface{}{"serial_numbers": []interface{}{s1}})
	m.receiveAt(m.loc, sku, 1, 300, map[string]interface{}{"serial_numbers": []interface{}{s2}})
	invoice := m.salesInvoice(map[string]interface{}{"sku": sku, "qty": 1, "rate": 1000, "amount": 1000, "serial_nos": []string{s2}})
	m.wantAvg(sku, 10000, "after the sale")
	if err := CancelSalesInvoice(m.tenant, invoice, "system", "billed in error"); err != nil {
		t.Fatalf("CancelSalesInvoice: %v", err)
	}
	if got := m.gl(invoice, "5100"); got != 0 {
		t.Errorf("COGS after cancel %d, want 0", got)
	}
	m.wantBalanced(invoice)
	m.wantAvg(sku, 20000, "the piece is back")
	if st := m.pieceData("SerialNumber", "serial_no", s2)["status"]; st != SerialInStock {
		t.Errorf("piece status %v", st)
	}
	if v := m.valuation(sku); v["total_value"] != 400.0 {
		t.Errorf("valuation %+v", v)
	}

	// Bulk: the same, by the booked figure rather than whatever the rate is now.
	bulk := m.item(nil)
	m.receive(bulk, 2, 100)
	sale := m.salesInvoice(map[string]interface{}{"sku": bulk, "qty": 2, "rate": 500, "amount": 1000})
	m.receive(bulk, 2, 300)
	if err := CancelSalesInvoice(m.tenant, sale, "system", "billed in error"); err != nil {
		t.Fatalf("CancelSalesInvoice: %v", err)
	}
	m.wantAvg(bulk, 20000, "2 back at Rs 100 and 2 at Rs 300")
}

// A late charge raises the own cost of the pieces still here, and only those.
func TestStage1481LateChargeRaisesTheOwnCostOfPiecesStillHere(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(map[string]interface{}{"tracking_mode": TrackingSerial})
	s1, s2 := m.id("S"), m.id("S")
	grn, _ := m.receiveAt(m.loc, sku, 2, 100, map[string]interface{}{"serial_numbers": []interface{}{s1, s2}})
	m.mustSell(sku, 1, map[string]interface{}{"serial_nos": []string{s1}})
	voucher, err := CreateLandedCostVoucher(m.tenant, grn, []map[string]interface{}{{"charge_type": "Freight", "amount": 100.0}}, "system")
	if err != nil {
		t.Fatalf("CreateLandedCostVoucher: %v", err)
	}
	m.docs = append(m.docs, voucher)
	if err := ApplyLandedCostVoucher(m.tenant, voucher, "system"); err != nil {
		t.Fatalf("ApplyLandedCostVoucher: %v", err)
	}
	if got := m.gl(voucher, "1200"); got != 5000 {
		t.Errorf("1200 Dr %d, want 5000", got)
	}
	if got := m.gl(voucher, "5195"); got != 5000 {
		t.Errorf("5195 Dr %d, want 5000", got)
	}
	if got := numFromInterface(m.pieceData("SerialNumber", "serial_no", s1)["receipt_cost_paise"]); got != 10000 {
		t.Errorf("the sold piece moved to %v", got)
	}
	if got := numFromInterface(m.pieceData("SerialNumber", "serial_no", s2)["receipt_cost_paise"]); got != 15000 {
		t.Errorf("the piece still here is %v, want 15000", got)
	}
	if v := m.valuation(sku); v["total_value"] != 150.0 {
		t.Errorf("valuation %+v", v)
	}
	if _, cost := m.mustSell(sku, 1, map[string]interface{}{"serial_nos": []string{s2}}); cost != 150 {
		t.Errorf("the piece sold at Rs %.2f, want Rs 150", cost)
	}
}

// A correction to a posted GRN moves the rate by exactly what it books to stock.
func TestStage1481GRNCorrectionMovesTheRate(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, _ := m.receive(sku, 10, 100)
	if err := correctReceiptCost(m.tenant, sku, grn, 0, -12000, -1200); err != nil {
		t.Fatal(err)
	}
	m.wantAvg(sku, 8800, "price cut from Rs 100 to Rs 88, nothing sold")

	cut := m.item(nil)
	grnCut, _ := m.receive(cut, 10, 100)
	m.receive(cut, 10, 200)
	line := []interface{}{map[string]interface{}{"sku": cut, "qty": -2.0}}
	if _, err := PostInventoryLedgerWithVoucher(m.tenant, m.loc, line, false, "GRNCorrection", grnCut+":FIX", "system"); err != nil {
		t.Fatal(err)
	}
	if err := correctReceiptCost(m.tenant, cut, grnCut, -2, -20000, 0); err != nil {
		t.Fatal(err)
	}
	// (20 x Rs 150 - 2 x Rs 100) / 18
	m.wantAvg(cut, 15556, "two of the Rs 100 units were never received")

	more := m.item(nil)
	grnMore, _ := m.receive(more, 10, 100)
	line = []interface{}{map[string]interface{}{"sku": more, "qty": 2.0}}
	if _, err := PostInventoryLedgerWithVoucher(m.tenant, m.loc, line, false, "GRNCorrection", grnMore+":FIX", "system"); err != nil {
		t.Fatal(err)
	}
	if err := correctReceiptCost(m.tenant, more, grnMore, 2, 24000, 0); err != nil {
		t.Fatal(err)
	}
	// (10 x Rs 100 + 2 x Rs 120) / 12
	m.wantAvg(more, 10333, "two more found at Rs 120")
}

// A cashier's scan names a piece through the store lookup, and the quote carries
// what was named to the cart the sale posts from.
func TestStage1481ScanNamesAPieceAndTheQuoteCarriesIt(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(map[string]interface{}{"tracking_mode": TrackingSerial})
	s1 := m.id("S")
	m.receiveAt(m.loc, sku, 1, 100, map[string]interface{}{"serial_numbers": []interface{}{s1}})
	access := combineStoreAccess("Cashier", m.loc, StoreAccessHome, nil, StoreAccessHome, nil)
	got, err := storeAppLookup(m.tenant, access, m.loc, s1)
	if err != nil {
		t.Fatalf("storeAppLookup: %v", err)
	}
	if got.MatchedOn != "serial" || got.SerialNo != s1 || got.Code != sku {
		t.Errorf("scan found %+v", got)
	}
	if byCode, err := storeAppLookup(m.tenant, access, m.loc, sku); err != nil || byCode.MatchedOn == "serial" || byCode.MatchedOn == "batch" {
		t.Errorf("the item's own code is not a piece: %+v %v", byCode, err)
	}

	other := m.item(nil)
	quote, err := ResolvePOSQuote(m.tenant, QuoteRequest{Location: m.loc, Lines: []QuoteLineRequest{
		{Sku: sku, Qty: 1, FallbackUnitPrice: 500, SerialNos: []string{s1}},
		{Sku: other, Qty: 2, FallbackUnitPrice: 500, BatchNo: "B-1"},
	}})
	if err != nil {
		t.Fatalf("ResolvePOSQuote: %v", err)
	}
	items := POSCartItemsFromQuote(quote)
	if len(items) != 2 {
		t.Fatalf("items %+v", items)
	}
	if got := fmt.Sprint(items[0]["serial_nos"]); got != "["+s1+"]" {
		t.Errorf("the piece did not reach the cart: %v", got)
	}
	if items[1]["batch_no"] != "B-1" {
		t.Errorf("the batch did not reach the cart: %v", items[1]["batch_no"])
	}
}

// Stock value and the ledger agree after a run of ordinary events on one item,
// within the rounding of a rate held in whole paise.
func TestStage1481StockValueAgreesWithTheLedger(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn1, _ := m.receive(sku, 10, 100)
	cart1, _ := m.mustSell(sku, 6, nil)
	grn2, _ := m.receive(sku, 10, 200)
	cart2, _ := m.mustSell(sku, 3, nil)
	voucher, err := CreateLandedCostVoucher(m.tenant, grn2, []map[string]interface{}{{"charge_type": "Freight", "amount": 110.0}}, "system")
	if err != nil {
		t.Fatalf("CreateLandedCostVoucher: %v", err)
	}
	m.docs = append(m.docs, voucher)
	if err := ApplyLandedCostVoucher(m.tenant, voucher, "system"); err != nil {
		t.Fatalf("ApplyLandedCostVoucher: %v", err)
	}
	ret := m.custReturn(cart2, sku, "Sellable", ReturnItemInput{})
	var ledger int64
	for _, id := range []string{grn1, cart1, grn2, cart2, voucher, ret} {
		ledger += m.gl(id, "1200")
	}
	if got := m.onHand(sku); got != 12 {
		t.Fatalf("on hand %v, want 12", got)
	}
	if ledger <= 0 {
		t.Fatalf("GL 1200 for the run is %d", ledger)
	}
	value := RupeesToPaise(numFromInterface(m.valuation(sku)["total_value"]))
	if diff := value - ledger; diff < -100 || diff > 100 {
		t.Errorf("stock value %d against GL 1200 %d: more than Rs 1 apart", value, ledger)
	}
}

// The control total's summary follows the report's rows when pieces are in stock:
// three pieces at Rs 100, 100, 101 are worth Rs 301, not 3 x the rounded Rs 100.33.
func TestStage1481ControlTotalSummaryCountsOwnCosts(t *testing.T) {
	m := newMC1481(t)
	gap := func() float64 {
		rows, err := GetInventoryValuation(m.tenant)
		if err != nil {
			t.Fatalf("GetInventoryValuation: %v", err)
		}
		c, err := inventoryValuationControlTotals(m.tenant, nil, rows)
		if err != nil {
			t.Fatalf("control totals: %v", err)
		}
		return c.DetailTotal - c.SummaryTotal
	}
	adjustBefore, err := ownCostAdjustmentPaise(db.DB, m.schema)
	if err != nil {
		t.Fatal(err)
	}
	gapBefore := gap()
	sku := m.item(map[string]interface{}{"tracking_mode": TrackingSerial})
	for _, rate := range []float64{100, 100, 101} {
		m.receiveAt(m.loc, sku, 1, rate, map[string]interface{}{"serial_numbers": []interface{}{m.id("S")}})
	}
	m.wantAvg(sku, 10033, "Rs 301 over 3")
	if v := m.valuation(sku); v["total_value"] != 301.0 {
		t.Errorf("valuation %+v", v)
	}
	adjustAfter, err := ownCostAdjustmentPaise(db.DB, m.schema)
	if err != nil {
		t.Fatal(err)
	}
	if got := adjustAfter - adjustBefore; got != 1 {
		t.Errorf("adjustment moved %d paise, want 1", got)
	}
	if moved := gap() - gapBefore; moved > 0.001 || moved < -0.001 {
		t.Errorf("the report's rows and summary moved apart by %v", moved)
	}
}

// A bill that lists lines settles only what it lists: an item it leaves out, or
// units it has not billed yet, are no credit from the vendor.
func TestStage1481BillLinesSettleOnlyWhatTheyList(t *testing.T) {
	m := newMC1481(t)
	a, b := m.item(nil), m.item(nil)
	lines := []mc1481Line{{sku: a, qty: 10, rate: 100}, {sku: b, qty: 10, rate: 100}}
	grn, po := m.grnDocs(m.loc, lines)
	if err := m.postGRN(m.loc, grn, lines); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]map[string]interface{}{{"sku": a, "qty": 5, "rate": 110}})
	bill := m.bill(po, grn, 550, map[string]interface{}{"bill_lines": string(raw)})
	res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 20) // Rs 110 on Rs 100 is 10% over
	if err != nil || !res.Matched {
		t.Fatalf("a part bill naming its lines: %v %+v", err, res)
	}
	// Rs 10 over on 5 units is Rs 50, nothing sold, so all of it on stock.
	if s, i := m.gl(bill, "2100"), m.gl(bill, "1200"); s != -5000 || i != 5000 {
		t.Errorf("2100 %d, 1200 %d, want -5000 and 5000", s, i)
	}
	m.wantBalanced(bill)
	m.wantAvg(a, 10500, "the Rs 50 spreads over the 10 in hand")
	m.wantAvg(b, 10000, "an item the bill leaves out has not moved")
}

// More units than were accepted is over-billing however the rates read, and the
// units sent back on a purchase return are no longer there to bill.
func TestStage1481BillLineOverQuantityIsHeld(t *testing.T) {
	m := newMC1481(t)
	a, b := m.item(nil), m.item(nil)
	lines := []mc1481Line{{sku: a, qty: 10, rate: 100}, {sku: b, qty: 10, rate: 100}}
	grn, po := m.grnDocs(m.loc, lines)
	if err := m.postGRN(m.loc, grn, lines); err != nil {
		t.Fatal(err)
	}
	billLines := func(qa, qb float64) string {
		raw, _ := json.Marshal([]map[string]interface{}{{"sku": a, "qty": qa, "rate": 100}, {"sku": b, "qty": qb, "rate": 100}})
		return string(raw)
	}
	// The total agrees with the GRN to the rupee; only the quantity gives it away.
	over := m.bill(po, grn, 2000, map[string]interface{}{"bill_lines": billLines(12, 8)})
	res, err := MatchVendorInvoice(m.tenant, over, po, grn, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched || !strings.Contains(strings.Join(res.Reasons, " | "), a) {
		t.Fatalf("12 billed, 10 accepted: must hold the bill naming %s: %+v", a, res)
	}
	if i, v, s := m.gl(over, "1200"), m.gl(over, "5195"), m.gl(over, "2100"); i != 0 || v != 0 || s != 0 {
		t.Errorf("a held bill posted: 1200 %d, 5195 %d, 2100 %d", i, v, s)
	}
	m.wantAvg(a, 10000, "held bill")
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = jsonb_set(data, '{status}', '\"Cancelled\"'), status = 'Cancelled' WHERE id = $1", over); err != nil {
		t.Fatal(err)
	}

	ret := m.id("PR")
	back, _ := json.Marshal([]map[string]interface{}{{"sku": a, "stock_bucket": "Accepted", "qty": 4}})
	vendor := m.id("VEND")
	m.doc(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": vendor})
	m.doc(ret, "PurchaseReturn", "Draft", map[string]interface{}{"grn_id": grn, "reason": "wrong size", "return_items": string(back), "vendor_id": vendor, "location": m.loc})
	pr, err := PostPurchaseReturn(m.tenant, ret, "system", "HQ")
	if err != nil {
		t.Fatalf("PostPurchaseReturn: %v", err)
	}
	if pr.DebitNoteID != "" {
		m.docs = append(m.docs, pr.DebitNoteID)
	}
	sentBack := m.bill(po, grn, 1600, map[string]interface{}{"bill_lines": billLines(8, 8)})
	if res, err = MatchVendorInvoice(m.tenant, sentBack, po, grn, 2); err != nil {
		t.Fatal(err)
	}
	if res.Matched || !strings.Contains(strings.Join(res.Reasons, " | "), a) {
		t.Fatalf("8 billed, 6 left after the return: must hold: %+v", res)
	}
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = jsonb_set(data, '{status}', '\"Cancelled\"'), status = 'Cancelled' WHERE id = $1", sentBack); err != nil {
		t.Fatal(err)
	}
	net := m.bill(po, grn, 1600, map[string]interface{}{"bill_lines": billLines(6, 10)})
	if res, err = MatchVendorInvoice(m.tenant, net, po, grn, 2); err != nil || !res.Matched {
		t.Fatalf("6 and 10 are exactly what is left: %v %+v", err, res)
	}
	if i, s := m.gl(net, "1200"), m.gl(net, "2100"); i != 0 || s != 0 {
		t.Errorf("a bill at the GRN's rates moved value: 1200 %d, 2100 %d", i, s)
	}
}
