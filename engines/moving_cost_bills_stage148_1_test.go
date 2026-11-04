package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// Stage 148.1 left, bill paths: a held bill, a step-flow GRN, a second bill on
// one GRN and a cancelled bill. Each ends with 2100 cleared and the stock value
// equal to GL 1200.

// glAll is debit minus credit on one account across documents.
func (m *mc1481) glAll(account string, docs ...string) int64 {
	m.t.Helper()
	var net int64
	for _, id := range docs {
		net += m.gl(id, account)
	}
	return net
}

func (m *mc1481) wantStockEqualsGL(sku string, docs ...string) {
	m.t.Helper()
	ledger := m.glAll("1200", docs...)
	value := RupeesToPaise(numFromInterface(m.valuation(sku)["total_value"]))
	if diff := value - ledger; diff < -100 || diff > 100 {
		m.t.Errorf("stock value %d against GL 1200 %d: more than Rs 1 apart", value, ledger)
	}
}

func (m *mc1481) wantCleared(account string, docs ...string) {
	m.t.Helper()
	if net := m.glAll(account, docs...); net != 0 {
		m.t.Errorf("%s is left at %d paise", account, net)
	}
}

func (m *mc1481) billData(id string) map[string]interface{} {
	m.t.Helper()
	data, _, err := fetchDocData(m.tenant, "VendorInvoice", id)
	if err != nil {
		m.t.Fatalf("bill %s: %v", id, err)
	}
	return data
}

// overridePay pays a bill as a checker's approved override does.
func (m *mc1481) overridePay(bill string) {
	m.t.Helper()
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET status = 'Approved', data = jsonb_set(data, '{status}', '\"Approved\"') WHERE id = $1", bill); err != nil {
		m.t.Fatal(err)
	}
	if _, err := FinalizeVendorInvoiceOverridePayment(m.tenant, bill, "system"); err != nil {
		m.t.Fatalf("override pay %s: %v", bill, err)
	}
}

func (m *mc1481) mustPay(bill string) {
	m.t.Helper()
	if _, _, err := PayVendorInvoice(m.tenant, bill, "system", "Super Admin", ""); err != nil {
		m.t.Fatalf("pay %s: %v", bill, err)
	}
}

func billLinesJSON(rows ...map[string]interface{}) string {
	raw, _ := json.Marshal(rows)
	return string(raw)
}

// --- A held bill ------------------------------------------------------------

// 10 @ Rs 100, 6 sold, a bill of Rs 1,500 held and paid by override: the Rs 500
// was stranded in 2100. It books when the money goes, once, through the real
// maker-checker path.
func TestStage1481HeldBillPaidByOverrideBooksOnce(t *testing.T) {
	m := newMC1481(t)
	withSelfApproval(t, m.tenant, SelfApprovalNobody)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	cart, _ := m.mustSell(sku, 6, nil)
	bill := m.bill(po, grn, 1500, nil)
	if res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 2); err != nil || res.Matched {
		t.Fatalf("held: %v %+v", err, res)
	}
	if n := m.gl(bill, "1200"); n != 0 {
		t.Fatalf("a hold posted %d", n)
	}
	if _, pending, err := PayVendorInvoice(m.tenant, bill, "manager1", "Store Manager", "vendor confirmed the price"); err != nil || !pending {
		t.Fatalf("override: pending %v %v", pending, err)
	}
	if n := m.gl(bill, "1200"); n != 0 {
		t.Fatalf("a claim for approval posted %d", n)
	}
	if err := DecideApproval(m.tenant, "VendorInvoice", bill, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := FinalizeVendorInvoiceOverridePayment(m.tenant, bill, "admin"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if a, v := m.gl(bill, "1200"), m.gl(bill, "5195"); a != 20000 || v != 30000 {
		t.Errorf("1200 %d, 5195 %d, want 20000 and 30000", a, v)
	}
	m.wantAvg(sku, 15000, "4 in hand take Rs 200")
	m.wantCleared("2100", grn, bill)
	m.wantBalanced(bill)
	m.wantStockEqualsGL(sku, grn, cart, bill)

	data := m.billData(bill)
	details, _ := data["match_details"].(map[string]interface{})
	if details["cost_difference_skipped"] != nil || numFromInterface(details["cost_difference"]) != 500 {
		t.Errorf("the bill does not say what it booked: %v", details)
	}

	// A retry books nothing: the bill is Paid.
	if _, err := FinalizeVendorInvoiceOverridePayment(m.tenant, bill, "admin"); err == nil {
		t.Errorf("a second finalize went through")
	}
	if a, v := m.gl(bill, "1200"), m.gl(bill, "5195"); a != 20000 || v != 30000 {
		t.Errorf("a retry posted again: 1200 %d, 5195 %d", a, v)
	}
	m.wantAvg(sku, 15000, "retry")
}

// A hold released by a re-match books at the release; paying it books no more.
func TestStage1481HeldBillReleasedByMatchBooksOnce(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	bill := m.bill(po, grn, 1100, nil)
	if res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 2); err != nil || res.Matched {
		t.Fatalf("held: %v %+v", err, res)
	}
	if res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 20); err != nil || !res.Matched {
		t.Fatalf("release: %v %+v", err, res)
	}
	if a := m.gl(bill, "1200"); a != 10000 {
		t.Errorf("1200 %d at release, want 10000", a)
	}
	m.mustPay(bill)
	if a := m.gl(bill, "1200"); a != 10000 {
		t.Errorf("1200 %d after paying, want 10000", a)
	}
	m.wantAvg(sku, 11000, "released")
	m.wantCleared("2100", grn, bill)
	m.wantStockEqualsGL(sku, grn, bill)
}

// Matched and booked, corrected into a hold (booking taken back), then paid by
// override at the new figure: net once.
func TestStage1481TakenBackThenOverrideBooksOnce(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	bill := m.bill(po, grn, 1100, nil)
	if res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 20); err != nil || !res.Matched {
		t.Fatalf("match: %v %+v", err, res)
	}
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = jsonb_set(data, '{invoice_amount}', '1600') WHERE id = $1", bill); err != nil {
		t.Fatal(err)
	}
	if res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 2); err != nil || res.Matched {
		t.Fatalf("hold: %v %+v", err, res)
	}
	if a := m.gl(bill, "1200"); a != 0 {
		t.Fatalf("the hold did not take it back: %d", a)
	}
	m.overridePay(bill)
	if a := m.gl(bill, "1200"); a != 60000 {
		t.Errorf("1200 %d, want 60000", a)
	}
	m.wantAvg(sku, 16000, "override")
	m.wantCleared("2100", grn, bill)
	m.wantStockEqualsGL(sku, grn, bill)
}

// The amount typed over after the last match: what is paid is what books.
func TestStage1481OverrideAfterTheAmountWasCorrected(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	bill := m.bill(po, grn, 1500, nil)
	if res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 2); err != nil || res.Matched {
		t.Fatalf("held: %v %+v", err, res)
	}
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = jsonb_set(data, '{invoice_amount}', '1400') WHERE id = $1", bill); err != nil {
		t.Fatal(err)
	}
	m.overridePay(bill)
	if a := m.gl(bill, "1200"); a != 40000 {
		t.Errorf("1200 %d, want 40000", a)
	}
	m.wantCleared("2100", grn, bill)
	m.wantStockEqualsGL(sku, grn, bill)
}

// With GST the bill's value before tax is what settles: the tax goes to 1500.
func TestStage1481OverrideWithGSTBooksTheValueBeforeTax(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = jsonb_set(data, '{grand_total}', '1180') WHERE id = $1", po); err != nil {
		t.Fatal(err)
	}
	bill := m.bill(po, grn, 1770, nil)
	if res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 2); err != nil || res.Matched {
		t.Fatalf("held: %v %+v", err, res)
	}
	m.overridePay(bill)
	if a, g := m.gl(bill, "1200"), m.gl(bill, "1500"); a != 50000 || g != 27000 {
		t.Errorf("1200 %d, 1500 %d, want 50000 and 27000", a, g)
	}
	m.wantCleared("2100", grn, bill)
	m.wantStockEqualsGL(sku, grn, bill)
}

// A bill never matched has no value before tax on it: nothing books, and it says so.
func TestStage1481DraftPaidByOverrideBooksNothing(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	bill := m.bill(po, grn, 1500, nil)
	m.overridePay(bill)
	if a := m.gl(bill, "1200"); a != 0 {
		t.Errorf("1200 %d, want 0", a)
	}
	details, _ := m.billData(bill)["match_details"].(map[string]interface{})
	if details["cost_difference_skipped"] != "Never matched" {
		t.Errorf("no reason on the bill: %v", details)
	}
	m.wantAvg(sku, 10000, "draft override")
}

// Foreign currency stays skipped: realised FX is booked at payment.
func TestStage1481ForeignBillAndNoGRNStaySkipped(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = data || '{\"transaction_currency\":\"USD\",\"exchange_rate\":80,\"currency\":\"USD\"}' WHERE id = $1", po); err != nil {
		t.Fatal(err)
	}
	bill := m.bill(po, grn, 1100, map[string]interface{}{"transaction_currency": "USD", "exchange_rate": 80.0, "currency": "USD"})
	res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !documentFXPosition(m.tenant, m.billData(bill), "invoice_amount").Foreign {
		t.Skip("this tenant reads no foreign position from the fixture")
	}
	if res.Details["cost_difference_skipped"] != "Foreign currency bill" {
		t.Errorf("details %v", res.Details)
	}
	if a := m.gl(bill, "1200"); a != 0 {
		t.Errorf("1200 %d, want 0", a)
	}
}

// --- A GRN received in steps ------------------------------------------------

type mc1481Step struct {
	sku, grn, po string
}

// stepReceipt receives one line in steps: the vendor's invoice at invoiceRate,
// our verified price at grnPrice, stopped before the posting so a test can raise
// its own bill first.
func (m *mc1481) stepReceipt(qty, invoiceRate, grnPrice, gstRate, typed float64) mc1481Step {
	m.t.Helper()
	preserveSeries(m.t, m.schema, "DN", "VCN", "GRNBatchSeq", "VendorInvoice", "GRN")
	sku, vend, po, grn := m.id("I"), m.id("VEND"), m.id("PO"), m.id("GRN")
	m.doc(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku + " name", "hsn_code": "6204", "gst_rate": gstRate, "mrp": 999.0})
	m.skus = append(m.skus, sku)
	m.doc(vend, "Vendor", "Active", map[string]interface{}{"code": vend, "name": vend})
	items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": qty, "rate": grnPrice}})
	m.doc(po, "PurchaseOrder", "Approved", map[string]interface{}{"code": po, "vendor": vend, "gst_mode": GSTModeExclusive,
		"total_amount": qty * grnPrice, "grand_total": qty * grnPrice * (1 + gstRate/100), "items": string(items)})
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": qty, "invoice_rate": invoiceRate}})
	payload := map[string]interface{}{"flow": "steps", "po_ids": po, "location": m.loc, "vendor_invoice_no": m.id("INV"), "vendor_invoice_date": "2026-10-18",
		"vendor_invoice_amount": typed, "received_items": string(lines)}
	for _, step := range []func() error{
		func() error { return PrepareGRNPricing(m.tenant, true, payload) },
		func() error { return PrepareGRNSteps(m.tenant, "system", true, payload) },
		func() error { return PrepareGRNBatchNumbers(m.tenant, true, payload) },
		func() error { return validateGRNRules(m.tenant, "", payload) },
	} {
		if err := step(); err != nil {
			m.t.Fatalf("receive in steps: %v", err)
		}
	}
	payload["code"] = grn
	m.doc(grn, "GRN", strField(payload, "status"), payload)
	SyncGRNLineRows(m.tenant, grn)
	m.t.Cleanup(func() {
		db.DB.Exec("DELETE FROM "+m.schema+".grn_line WHERE grn_id = $1", grn)
		db.DB.Exec("DELETE FROM "+m.schema+".grn_line_change WHERE grn_id = $1", grn)
	})
	price := grnPrice
	if _, err := VerifyGRNStep(m.tenant, "system", "Super Admin", grn, GRNVerifyInput{Lines: []GRNVerifyLine{{LineNo: 1, GRNPrice: &price}}, Reason: "checked"}); err != nil {
		m.t.Fatalf("verify: %v", err)
	}
	return mc1481Step{sku: sku, grn: grn, po: po}
}

// post finishes the receipt, every child, and returns its bill and note.
func (m *mc1481) postStep(s mc1481Step) *GRNStepView {
	m.t.Helper()
	v, err := PriceCheckGRNStep(m.tenant, "system", s.grn, GRNPriceCheckInput{})
	if err != nil {
		m.t.Fatalf("price check: %v", err)
	}
	for _, c := range v.Children {
		if v, err = PostGRNChildStep(m.tenant, "system", "", s.grn, c.Key, GRNPostInput{MRP: map[string]float64{"1": 999}}); err != nil {
			m.t.Fatalf("post child %s: %v", c.Key, err)
		}
	}
	if v.Step != GRNStepPosted {
		m.t.Fatalf("not posted: %s", v.Step)
	}
	for _, id := range []string{v.VendorBillID, v.DebitNoteID, v.VendorCreditNoteID} {
		if id != "" {
			m.docs = append(m.docs, id)
		}
	}
	return v
}

func (m *mc1481) payStep(s mc1481Step) {
	m.t.Helper()
	if _, err := PayGRNStep(m.tenant, "system", s.grn, GRNPayInput{Mode: "UPI", Reference: "UTR" + m.run}); err != nil {
		m.t.Fatalf("pay: %v", err)
	}
}

// A bill made from the Receive figures: the note carries the whole gap, so the
// cost must not move, debit note or credit note.
func TestStage1481StepBillFromReceiveFiguresBooksNothing(t *testing.T) {
	cases := []struct {
		name               string
		invoice, typed     float64
		wantDebit, wantVCN bool
	}{
		{name: "debit note", invoice: 120, typed: 1416, wantDebit: true},
		{name: "credit note", invoice: 90, typed: 1062, wantVCN: true},
		{name: "no note", invoice: 100, typed: 1180},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newMC1481(t)
			s := m.stepReceipt(10, c.invoice, 100, 18, c.typed)
			v := m.postStep(s)
			if (v.DebitNoteID != "") != c.wantDebit || (v.VendorCreditNoteID != "") != c.wantVCN {
				t.Fatalf("notes: debit %q credit %q", v.DebitNoteID, v.VendorCreditNoteID)
			}
			if a := m.gl(v.VendorBillID, "1200"); a != 0 {
				t.Errorf("the bill moved stock by %d", a)
			}
			m.wantAvg(s.sku, 10000, "bill from Receive")
			m.payStep(s)
			m.wantCleared("2100", s.grn, v.VendorBillID, v.DebitNoteID, v.VendorCreditNoteID)
			m.wantStockEqualsGL(s.sku, s.grn, v.VendorBillID)
		})
	}
}

// A bill the vendor actually raised for Rs 12 more than the invoice keyed at
// Receive: the Rs 12 goes into cost, the note still carries its own gap, and
// 2100 clears through the real payment.
func TestStage1481StepBillDifferingFromTheInvoiceBooks(t *testing.T) {
	cases := []struct {
		name              string
		invoice, typed    float64
		bill              float64
		wantAdjust        int64
		wantDebit, wantVC bool
	}{
		{name: "with a debit note", invoice: 120, typed: 1416, bill: 1428, wantAdjust: 1200, wantDebit: true},
		{name: "with a credit note", invoice: 90, typed: 1062, bill: 1074, wantAdjust: 1200, wantVC: true},
		{name: "no note", invoice: 100, typed: 1180, bill: 1192, wantAdjust: 1017},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newMC1481(t)
			s := m.stepReceipt(10, c.invoice, 100, 18, c.typed)
			hand := m.bill(s.po, s.grn, c.bill, nil)
			v := m.postStep(s)
			if v.VendorBillID != hand {
				t.Fatalf("the receipt made its own bill %s, not %s", v.VendorBillID, hand)
			}
			if (v.DebitNoteID != "") != c.wantDebit || (v.VendorCreditNoteID != "") != c.wantVC {
				t.Fatalf("notes: debit %q credit %q", v.DebitNoteID, v.VendorCreditNoteID)
			}
			if a := m.gl(hand, "1200"); a != c.wantAdjust {
				t.Errorf("1200 %d, want %d", a, c.wantAdjust)
			}
			m.wantBalanced(hand)
			m.payStep(s)
			docs := []string{s.grn, hand, v.DebitNoteID, v.VendorCreditNoteID}
			if net := m.glAll("2100", docs...); net < -1 || net > 1 {
				t.Errorf("2100 is left at %d paise", net)
			}
			m.wantStockEqualsGL(s.sku, s.grn, hand)
			// A second match refused or moved nothing: paid once, booked once.
			if a := m.gl(hand, "1200"); a != c.wantAdjust {
				t.Errorf("1200 %d after paying", a)
			}
		})
	}
}

// --- A second bill on one GRN -----------------------------------------------

// Two bills that list their lines: each settles only its own units.
func TestStage1481SecondBillByLinesBooksItsOwnUnits(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	a := m.bill(po, grn, 660, map[string]interface{}{"bill_lines": billLinesJSON(map[string]interface{}{"sku": sku, "qty": 6, "rate": 110})})
	if res, err := MatchVendorInvoice(m.tenant, a, po, grn, 25); err != nil || !res.Matched {
		t.Fatalf("first: %v %+v", err, res)
	}
	if n := m.gl(a, "1200"); n != 6000 {
		t.Errorf("first bill 1200 %d, want 6000", n)
	}
	b := m.bill(po, grn, 480, map[string]interface{}{"bill_lines": billLinesJSON(map[string]interface{}{"sku": sku, "qty": 4, "rate": 120})})
	res, err := MatchVendorInvoice(m.tenant, b, po, grn, 25)
	if err != nil || !res.Matched {
		t.Fatalf("second: %v %+v", err, res)
	}
	if res.Details["cost_difference_skipped"] != nil {
		t.Errorf("the second bill was skipped: %v", res.Details)
	}
	if n := m.gl(b, "1200"); n != 8000 {
		t.Errorf("second bill 1200 %d, want 8000", n)
	}
	m.wantAvg(sku, 11400, "both bills")

	// A re-match of either moves nothing.
	for _, id := range []string{a, b} {
		if _, err := MatchVendorInvoice(m.tenant, id, po, grn, 25); err != nil {
			t.Fatal(err)
		}
	}
	if x, y := m.gl(a, "1200"), m.gl(b, "1200"); x != 6000 || y != 8000 {
		t.Errorf("a re-match moved it: %d, %d", x, y)
	}

	// A third bill for units nobody received holds, and books nothing.
	c := m.bill(po, grn, 100, map[string]interface{}{"bill_lines": billLinesJSON(map[string]interface{}{"sku": sku, "qty": 1, "rate": 100})})
	if res, err := MatchVendorInvoice(m.tenant, c, po, grn, 50); err != nil || res.Matched {
		t.Fatalf("third: %v %+v", err, res)
	}
	if n := m.gl(c, "1200"); n != 0 {
		t.Errorf("third bill 1200 %d", n)
	}

	m.mustPay(a)
	m.mustPay(b)
	m.wantCleared("2100", grn, a, b)
	m.wantStockEqualsGL(sku, grn, a, b)
}

// Two whole bills paid by override: the first looks short, the second makes it
// up; net nothing in cost, nothing left in 2100.
func TestStage1481WholeBillsSplitByOverrideNetToNothing(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	a := m.bill(po, grn, 600, nil)
	if res, err := MatchVendorInvoice(m.tenant, a, po, grn, 2); err != nil || res.Matched {
		t.Fatalf("first: %v %+v", err, res)
	}
	m.overridePay(a)
	if n := m.gl(a, "1200"); n != -40000 {
		t.Errorf("first bill 1200 %d, want -40000", n)
	}
	b := m.bill(po, grn, 400, nil)
	if res, err := MatchVendorInvoice(m.tenant, b, po, grn, 2); err != nil || res.Matched {
		t.Fatalf("second: %v %+v", err, res)
	}
	m.overridePay(b)
	if n := m.gl(b, "1200"); n != 40000 {
		t.Errorf("second bill 1200 %d, want 40000", n)
	}
	m.wantAvg(sku, 10000, "both bills")
	m.wantCleared("2100", grn, a, b)
	m.wantStockEqualsGL(sku, grn, a, b)
}

// A supplementary bill on a GRN already billed in full: all of it is a difference.
// A first bill from before this stage holds no record of its units: it counts as
// all of them, and it is not rewritten.
func TestStage1481SupplementaryBillOnAnOldFirstBill(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	a := m.bill(po, grn, 1000, nil)
	if res, err := MatchVendorInvoice(m.tenant, a, po, grn, 2); err != nil || !res.Matched {
		t.Fatalf("first: %v %+v", err, res)
	}
	m.mustPay(a)
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = data - 'bill_cost_units' WHERE id = $1", a); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.DB.QueryRow("SELECT data::text FROM "+m.schema+".documents WHERE id = $1", a).Scan(&before); err != nil {
		t.Fatal(err)
	}
	b := m.bill(po, grn, 100, nil)
	if res, err := MatchVendorInvoice(m.tenant, b, po, grn, 2); err != nil || res.Matched {
		t.Fatalf("second: %v %+v", err, res)
	}
	m.overridePay(b)
	if n := m.gl(b, "1200"); n != 10000 {
		t.Errorf("second bill 1200 %d, want 10000", n)
	}
	m.wantAvg(sku, 11000, "supplementary")
	m.wantCleared("2100", grn, a, b)
	m.wantStockEqualsGL(sku, grn, a, b)
	var after string
	if err := db.DB.QueryRow("SELECT data::text FROM "+m.schema+".documents WHERE id = $1", a).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("the first bill was rewritten:\n%s\n%s", before, after)
	}
}

// A second bill for two items splits by what is still to bill on each.
func TestStage1481SecondWholeBillAcrossTwoItems(t *testing.T) {
	m := newMC1481(t)
	x, y := m.item(nil), m.item(nil)
	lines := []mc1481Line{{sku: x, qty: 10, rate: 100}, {sku: y, qty: 10, rate: 300}}
	grn, po := m.grnDocs(m.loc, lines)
	if err := m.postGRN(m.loc, grn, lines); err != nil {
		t.Fatal(err)
	}
	// The first bill names 10 of x at its rate; y is still to bill.
	a := m.bill(po, grn, 1000, map[string]interface{}{"bill_lines": billLinesJSON(map[string]interface{}{"sku": x, "qty": 10, "rate": 100})})
	if res, err := MatchVendorInvoice(m.tenant, a, po, grn, 2); err != nil || !res.Matched {
		t.Fatalf("first: %v %+v", err, res)
	}
	// The second is a whole bill of Rs 3,200 for the rest: Rs 200 over on y alone.
	b := m.bill(po, grn, 3200, nil)
	if res, err := MatchVendorInvoice(m.tenant, b, po, grn, 2); err != nil || res.Matched {
		t.Fatalf("second: %v %+v", err, res)
	}
	m.overridePay(b)
	if n := m.gl(b, "1200"); n != 20000 {
		t.Errorf("second bill 1200 %d, want 20000", n)
	}
	m.wantAvg(x, 10000, "x is billed in full")
	m.wantAvg(y, 32000, "y takes the Rs 200")
	m.mustPay(a)
	m.wantCleared("2100", grn, a, b)
}

func TestStage1481BillCostHelperKeepsTheKeyShape(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	bill := m.bill(po, grn, 1500, nil)
	if _, err := MatchVendorInvoice(m.tenant, bill, po, grn, 2); err != nil {
		t.Fatal(err)
	}
	m.overridePay(bill)
	var keys int
	want := fmt.Sprintf("VendorInvoice:%s:BILL_DIFF:%%", bill)
	if err := db.DB.QueryRow("SELECT COUNT(DISTINCT idempotency_key) FROM "+m.schema+".gl_postings WHERE document_id = $1 AND idempotency_key LIKE $2", bill, want).Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if keys != 1 {
		t.Errorf("%d difference postings, want 1", keys)
	}
}

// --- A bill matched before its step GRN finished posting ---------------------

// Matched while the GRN was still being worked: nothing to settle yet, and it says
// why. The posting matches it again, at the tolerance it passed at, and books.
func TestStage1481StepBillMatchedBeforePostingSettlesAtPosting(t *testing.T) {
	m := newMC1481(t)
	s := m.stepReceipt(10, 100, 100, 18, 1180)
	hand := m.bill(s.po, s.grn, 1192, nil)
	res, err := MatchVendorInvoice(m.tenant, hand, "", "", 5)
	if err != nil || !res.Matched {
		t.Fatalf("match before posting: %v %+v", err, res)
	}
	details, _ := m.billData(hand)["match_details"].(map[string]interface{})
	if got := strField(details, "cost_difference_skipped"); got != billSkipStepNotPosted {
		t.Errorf("reason %q, want %q", got, billSkipStepNotPosted)
	}
	if a := m.gl(hand, "1200"); a != 0 {
		t.Errorf("booked %d before the stock was in", a)
	}
	v := m.postStep(s)
	if v.VendorBillID != hand {
		t.Fatalf("the receipt made its own bill %s, not %s", v.VendorBillID, hand)
	}
	if a := m.gl(hand, "1200"); a != 1017 {
		t.Errorf("1200 %d after posting, want 1017", a)
	}
	details, _ = m.billData(hand)["match_details"].(map[string]interface{})
	if got := strField(details, "cost_difference_skipped"); got != "" {
		t.Errorf("still skipped: %q", got)
	}
	m.wantStockEqualsGL(s.sku, s.grn, hand)
	m.payStep(s)
	if net := m.glAll("2100", s.grn, hand); net < -1 || net > 1 {
		t.Errorf("2100 is left at %d paise", net)
	}
}

// --- A cancelled bill --------------------------------------------------------

// 10 @ Rs 100, 6 sold, a bill of Rs 1,500 matched: Rs 200 in stock, Rs 300 to
// variance. Cancelling takes both back on the bill's next key, a retry finds
// nothing, and a bill for the right figure then clears 2100.
func TestStage1481CancelledBillTakesBackItsDifference(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	cart, _ := m.mustSell(sku, 6, nil)
	bill := m.bill(po, grn, 1500, nil)
	if res, err := MatchVendorInvoice(m.tenant, bill, "", "", 100); err != nil || !res.Matched {
		t.Fatalf("match: %v %+v", err, res)
	}
	if h, v := m.gl(bill, "1200"), m.gl(bill, accountCOGSVariance); h != 20000 || v != 30000 {
		t.Fatalf("booked 1200 %d, variance %d, want 20000 and 30000", h, v)
	}
	if err := ReverseVendorBillDifference(m.tenant, bill, "system"); err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if h, v, c := m.gl(bill, "1200"), m.gl(bill, accountCOGSVariance), m.gl(bill, "2100"); h != 0 || v != 0 || c != 0 {
		t.Errorf("after cancelling: 1200 %d, variance %d, 2100 %d", h, v, c)
	}
	m.wantAvg(sku, 10000, "cancelled")
	m.wantStockEqualsGL(sku, grn, cart, bill)
	details, _ := m.billData(bill)["match_details"].(map[string]interface{})
	if got := strField(details, "cost_difference_skipped"); got != "Bill cancelled" {
		t.Errorf("reason %q", got)
	}
	var keys int
	count := `SELECT COUNT(DISTINCT idempotency_key) FROM ` + m.schema + `.gl_postings WHERE document_id = $1 AND idempotency_key LIKE '%BILL_DIFF%'`
	if err := db.DB.QueryRow(count, bill).Scan(&keys); err != nil || keys != 2 {
		t.Errorf("difference postings %d (%v), want the booking and its reversal", keys, err)
	}
	// A retry books nothing.
	if err := ReverseVendorBillDifference(m.tenant, bill, "system"); err != nil {
		t.Fatalf("second reverse: %v", err)
	}
	if err := db.DB.QueryRow(count, bill).Scan(&keys); err != nil || keys != 2 {
		t.Errorf("a retry posted: %d (%v)", keys, err)
	}
	// Soft-deleted as the screen does; the replacement sees only itself.
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1", bill); err != nil {
		t.Fatal(err)
	}
	next := m.bill(po, grn, 1000, nil)
	if res, err := MatchVendorInvoice(m.tenant, next, "", "", 0); err != nil || !res.Matched {
		t.Fatalf("replacement: %v %+v", err, res)
	}
	if a := m.gl(next, "1200"); a != 0 {
		t.Errorf("the replacement booked %d", a)
	}
	m.mustPay(next)
	m.wantCleared("2100", grn, bill, next)
	// Deleted: nothing left to reverse.
	if err := ReverseVendorBillDifference(m.tenant, bill, "system"); err != nil {
		t.Errorf("a deleted bill: %v", err)
	}
}

// A bill with money or a decision on it does not go: the difference stands.
func TestStage1481CancelRefusedWithMoneyOrApprovalOnIt(t *testing.T) {
	m := newMC1481(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	bill := m.bill(po, grn, 1200, nil)
	if res, err := MatchVendorInvoice(m.tenant, bill, "", "", 100); err != nil || !res.Matched {
		t.Fatalf("match: %v %+v", err, res)
	}
	wantRefused := func(why string) {
		t.Helper()
		err := ReverseVendorBillDifference(m.tenant, bill, "system")
		if t1481Code(err) != "FIN-0910" {
			t.Errorf("%s: %v, want FIN-0910", why, err)
		}
		if a := m.gl(bill, "1200"); a != 20000 {
			t.Errorf("%s: 1200 %d, want 20000 standing", why, a)
		}
	}
	set := func(status, payments string) {
		t.Helper()
		if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET status = $1, data = data || $2::jsonb WHERE id = $3", status, payments, bill); err != nil {
			t.Fatal(err)
		}
	}
	set("Matched", `{"payments":[{"amount":100,"reference":"x"}]}`)
	wantRefused("part paid")
	set("Pending Approval", `{"payments":[]}`)
	wantRefused("awaiting a checker")
	set("Approved", `{}`)
	wantRefused("approved")
	set("Matched", `{}`)
	m.mustPay(bill)
	wantRefused("paid")
}
