package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 148.12: a matched bill sets the item's purchase rate (Item.standard_cost).

type pr14812 struct {
	t              *testing.T
	tenant, schema string
	run            string
	seq            int
	docs, skus     []string
}

func newPR14812(t *testing.T) *pr14812 {
	t.Helper()
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	p := &pr14812{t: t, tenant: "default", schema: schema, run: fmt.Sprintf("%x", time.Now().UnixNano())}
	t.Cleanup(func() {
		for _, id := range p.docs {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		for _, s := range p.skus {
			db.DB.Exec("DELETE FROM "+schema+".item_purchase_rate_history WHERE sku = $1", s)
		}
		SetSetting("default", SettingPriceJumpPercent, "0", "t")
	})
	SetSetting("default", SettingPriceJumpPercent, "0", "t")
	return p
}

func (p *pr14812) id(prefix string) string {
	p.seq++
	return fmt.Sprintf("T14812-%s-%s-%d", prefix, p.run, p.seq)
}

func (p *pr14812) doc(id, doctype, status string, data map[string]interface{}) {
	p.t.Helper()
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+p.schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'system')", id, doctype, raw, status); err != nil {
		p.t.Fatalf("insert %s: %v", id, err)
	}
	p.docs = append(p.docs, id)
}

func (p *pr14812) newItem() string {
	sku := p.id("I")
	p.doc(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku, "hsn_code": "7113", "standard_cost": 100.0, "mrp": 250.0})
	p.skus = append(p.skus, sku)
	return sku
}

// order makes an item (cost 100, MRP 250), a PO at poRate and a GRN for 10.
func (p *pr14812) order(poRate float64) (sku, poID, grnID string) {
	sku, poID, grnID = p.newItem(), p.id("PO"), p.id("GRN")
	items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 10, "rate": poRate}})
	recv, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 10, "accepted_qty": 10}})
	total := 10 * poRate
	p.doc(poID, "PurchaseOrder", "Approved", map[string]interface{}{"code": poID, "gst_mode": GSTModeExclusive, "total_amount": total, "grand_total": total, "items": string(items)})
	p.doc(grnID, "GRN", "Received", map[string]interface{}{"code": grnID, "po_id": poID, "received_items": string(recv)})
	return
}

func (p *pr14812) bill(poID, grnID string, amount float64, extra map[string]interface{}) string {
	id := p.id("VI")
	data := map[string]interface{}{"id": id, "invoice_number": id, "vendor_id": "V-" + p.run, "po_id": poID, "grn_id": grnID, "invoice_amount": amount, "financial_year": "T14812", "status": "Draft"}
	for k, v := range extra {
		data[k] = v
	}
	p.doc(id, "VendorInvoice", "Draft", data)
	return id
}

func (p *pr14812) rate(sku string) float64 {
	d, _, err := fetchDocData(p.tenant, "Item", sku)
	if err != nil {
		p.t.Fatalf("item %s: %v", sku, err)
	}
	return numFromInterface(d["standard_cost"])
}

func (p *pr14812) mrp(sku string) float64 {
	d, _, _ := fetchDocData(p.tenant, "Item", sku)
	return numFromInterface(d["mrp"])
}

func (p *pr14812) history(sku string) []PurchaseRateChange {
	h, err := ItemPurchaseRateHistory(p.tenant, sku, 50)
	if err != nil {
		p.t.Fatalf("history: %v", err)
	}
	return h
}

func (p *pr14812) patch(id string, v map[string]interface{}) {
	raw, _ := json.Marshal(v)
	if _, err := db.DB.Exec("UPDATE "+p.schema+".documents SET data = data || $1::jsonb WHERE id = $2", string(raw), id); err != nil {
		p.t.Fatal(err)
	}
}

func TestStage14812MatchedBillSetsRateAndKeepsMRP(t *testing.T) {
	p := newPR14812(t)
	sku, po, grn := p.order(120)
	bill := p.bill(po, grn, 1200, nil)
	m, err := MatchVendorInvoiceAs(p.tenant, "buyer1", bill, "", "", 0)
	if err != nil || !m.Matched {
		t.Fatalf("match: %v %+v", err, m)
	}
	if p.rate(sku) != 120 {
		t.Fatalf("purchase rate = %v, want 120", p.rate(sku))
	}
	if p.mrp(sku) != 250 {
		t.Fatalf("a printed MRP moved to %v", p.mrp(sku))
	}
	h := p.history(sku)
	if len(h) != 1 || h[0].OldRate != 100 || h[0].NewRate != 120 || h[0].ChangedBy != "buyer1" || h[0].SourceID != bill || h[0].SourceDoctype != "VendorInvoice" {
		t.Fatalf("history = %+v", h)
	}
	// Matching the same bill again changes nothing and adds no history.
	if _, err := MatchVendorInvoiceAs(p.tenant, "buyer1", bill, "", "", 0); err != nil {
		t.Fatalf("re-match: %v", err)
	}
	if len(p.history(sku)) != 1 {
		t.Fatalf("a re-match wrote history again")
	}
	// A newer bill's rate is not undone by matching the older bill again.
	po2, grn2 := p.id("PO"), p.id("GRN")
	items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 10, "rate": 140.0}})
	recv, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 10, "accepted_qty": 10}})
	p.doc(po2, "PurchaseOrder", "Approved", map[string]interface{}{"code": po2, "gst_mode": GSTModeExclusive, "total_amount": 1400.0, "grand_total": 1400.0, "items": string(items)})
	p.doc(grn2, "GRN", "Received", map[string]interface{}{"code": grn2, "po_id": po2, "received_items": string(recv)})
	bill2 := p.bill(po2, grn2, 1400, nil)
	if m, err := MatchVendorInvoiceAs(p.tenant, "buyer1", bill2, "", "", 0); err != nil || !m.Matched {
		t.Fatalf("second bill: %v %+v", err, m)
	}
	if _, err := MatchVendorInvoiceAs(p.tenant, "buyer1", bill, "", "", 0); err != nil {
		t.Fatalf("older re-match: %v", err)
	}
	if p.rate(sku) != 140 || len(p.history(sku)) != 2 {
		t.Fatalf("rate %v history %d after the newer bill", p.rate(sku), len(p.history(sku)))
	}
}

func TestStage14812HeldBillDoesNotSetTheRateUntilReleased(t *testing.T) {
	p := newPR14812(t)
	sku, po, grn := p.order(120)
	bill := p.bill(po, grn, 5000, nil) // far above the goods: held
	m, err := MatchVendorInvoiceAs(p.tenant, "buyer1", bill, "", "", 0)
	if err != nil || m.Matched {
		t.Fatalf("want held, got %v %+v", err, m)
	}
	if p.rate(sku) != 100 || len(p.history(sku)) != 0 {
		t.Fatalf("a held bill set the rate")
	}
	p.patch(bill, map[string]interface{}{"invoice_amount": 1200.0})
	m, err = MatchVendorInvoiceAs(p.tenant, "buyer2", bill, "", "", 0)
	if err != nil || !m.Matched {
		t.Fatalf("released bill: %v %+v", err, m)
	}
	h := p.history(sku)
	if p.rate(sku) != 120 || len(h) != 1 || h[0].ChangedBy != "buyer2" {
		t.Fatalf("after release: %v %+v", p.rate(sku), h)
	}
}

func TestStage14812JumpAsksForAReason(t *testing.T) {
	p := newPR14812(t)
	if err := SetSetting(p.tenant, SettingPriceJumpPercent, "10", "t"); err != nil {
		t.Fatal(err)
	}
	sku, po, grn := p.order(130)
	bill := p.bill(po, grn, 1300, nil)
	m, err := MatchVendorInvoiceAs(p.tenant, "buyer1", bill, "", "", 0)
	if err != nil || m.Matched || m.Status != "MismatchHold" || !strings.Contains(strings.Join(m.Reasons, " "), "enter a reason") {
		t.Fatalf("a 30 percent jump must hold the bill asking for a reason: %v %+v", err, m)
	}
	if p.rate(sku) != 100 {
		t.Fatalf("held jump moved the rate")
	}
	p.patch(bill, map[string]interface{}{"price_jump_reason": "New season price"})
	m, err = MatchVendorInvoiceAs(p.tenant, "buyer1", bill, "", "", 0)
	if err != nil || !m.Matched {
		t.Fatalf("with a reason: %v %+v", err, m)
	}
	h := p.history(sku)
	if p.rate(sku) != 130 || len(h) != 1 || h[0].Reason != "New season price" {
		t.Fatalf("after reason: %v %+v", p.rate(sku), h)
	}
	// A change inside the limit is not asked about.
	sku2, po2, grn2 := p.order(105)
	if m, err := MatchVendorInvoiceAs(p.tenant, "buyer1", p.bill(po2, grn2, 1050, nil), "", "", 0); err != nil || !m.Matched {
		t.Fatalf("5 percent change: %v %+v", err, m)
	}
	if p.rate(sku2) != 105 {
		t.Fatalf("5 percent change not applied")
	}
}

func TestStage14812DefaultNeverAsks(t *testing.T) {
	p := newPR14812(t)
	sku, po, grn := p.order(300) // 200 percent up
	if m, err := MatchVendorInvoiceAs(p.tenant, "buyer1", p.bill(po, grn, 3000, nil), "", "", 0); err != nil || !m.Matched {
		t.Fatalf("with the setting at 0 a big jump must go through: %v %+v", err, m)
	}
	if p.rate(sku) != 300 {
		t.Fatalf("rate = %v", p.rate(sku))
	}
}

func TestStage14812BillWithItsOwnLinesSetsTheBillRate(t *testing.T) {
	p := newPR14812(t)
	m := newMC1481(t) // a posted receipt, which a line-by-line bill is checked against
	sku := m.item(map[string]interface{}{"standard_cost": 100.0, "mrp": 250.0})
	p.skus = append(p.skus, sku)
	grn, po := m.receive(sku, 10, 100)
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 10, "rate": 105.0}})
	bill := m.bill(po, grn, 1050, map[string]interface{}{"bill_lines": string(lines)})
	res, err := MatchVendorInvoiceAs(p.tenant, "buyer1", bill, "", "", 5)
	if err != nil || !res.Matched {
		t.Fatalf("lines bill: %v %+v", err, res)
	}
	if p.rate(sku) != 105 || p.mrp(sku) != 250 {
		t.Fatalf("rate = %v mrp = %v", p.rate(sku), p.mrp(sku))
	}
}

// The step-flow path (the receipt screens and the bulk purchase upload both end
// here): the bill made from the Receive figures is matched at the vendor's
// invoice rate.
func TestStage14812StepFlowBillSetsTheRate(t *testing.T) {
	p := newPR14812(t)
	preserveSeries(t, p.schema, "VendorInvoice")
	sku, po, _ := p.order(100)
	grnID := p.id("SGRN")
	recv, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 10, "invoice_rate": 110.0}})
	grn := map[string]interface{}{"code": grnID, "flow": "steps", "po_id": po, "vendor": "V-" + p.run, "location": "HO",
		"vendor_invoice_no": "INV-" + p.run, "vendor_invoice_date": "2026-10-18", "vendor_invoice_amount": 1100.0, "received_items": string(recv)}
	p.doc(grnID, "GRN", "Pending", grn)
	t.Cleanup(func() {
		db.DB.Exec("DELETE FROM "+p.schema+".gl_postings WHERE document_id IN (SELECT id FROM "+p.schema+".documents WHERE doctype = 'VendorInvoice' AND data->>'grn_id' = $1)", grnID)
		db.DB.Exec("DELETE FROM "+p.schema+".documents WHERE doctype = 'VendorInvoice' AND data->>'grn_id' = $1", grnID)
	})
	billID, _, err := ensureStepGRNVendorBill(p.tenant, "manager1", grnID, grn)
	if err != nil {
		t.Fatalf("ensureStepGRNVendorBill: %v", err)
	}
	if _, st, _ := fetchDocData(p.tenant, "VendorInvoice", billID); st != "Matched" {
		t.Fatalf("bill %s is %s", billID, st)
	}
	h := p.history(sku)
	if p.rate(sku) != 110 || len(h) != 1 || h[0].ChangedBy != "manager1" || h[0].SourceID != billID {
		t.Fatalf("rate %v history %+v", p.rate(sku), h)
	}
	if p.mrp(sku) != 250 {
		t.Fatalf("MRP moved to %v", p.mrp(sku))
	}
}

// The screen names a bill by its own number; a row whose bill is gone keeps the id.
func TestStage14812HistoryNamesTheBillByItsNumber(t *testing.T) {
	p := newPR14812(t)
	sku, bill := "SKU-REF-"+p.run, "BILL-ID-"+p.run
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'VendorInvoice', '{"invoice_number":"VEN/77"}', 'Matched', 'admin')`, p.schema), bill); err != nil {
		t.Fatalf("seed bill: %v", err)
	}
	for _, source := range []string{"GONE-" + p.run, bill} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.item_purchase_rate_history
			(sku, old_rate_paise, new_rate_paise, changed_by, source_doctype, source_id, vendor, reason)
			VALUES ($1, 100, 200, 'admin', 'VendorInvoice', $2, '', '')`, p.schema), sku, source); err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}
	h := p.history(sku)
	if len(h) != 2 || h[0].SourceRef != "VEN/77" || h[0].SourceID != bill || h[1].SourceRef != "GONE-"+p.run {
		t.Fatalf("history = %+v", h)
	}
}
