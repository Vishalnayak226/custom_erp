package engines

import (
	"encoding/json"
	"testing"
	"time"
)

// Stage 148.13: the vendor scorecard, worked out from existing documents.

func (p *pr14812) scPO(vendor, expected string, rate, qty float64) (sku, poID string) {
	sku, poID = p.newItem(), p.id("PO")
	items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": qty, "rate": rate}})
	p.doc(poID, "PurchaseOrder", "Approved", map[string]interface{}{"code": poID, "vendor_id": vendor, "vendor": vendor,
		"expected_delivery_date": expected, "gst_mode": GSTModeExclusive, "items": string(items)})
	return
}

func (p *pr14812) scGRN(poID, sku string, qty, accepted float64) {
	id := p.id("GRN")
	recv, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": qty, "accepted_qty": accepted}})
	p.doc(id, "GRN", "Received", map[string]interface{}{"code": id, "po_id": poID, "received_items": string(recv)})
}

func wantPct(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func TestStage14813ScorecardWorkedOutFromTheDocuments(t *testing.T) {
	p := newPR14812(t)
	v := "V14813-" + p.run
	today, _ := time.Parse("2006-01-02", BusinessToday())
	future := today.AddDate(0, 0, 5).Format("2006-01-02")

	// On time, 10 received, 8 accepted, billed 10% over the ordered rate.
	sku1, po1 := p.scPO(v, future, 100, 10)
	p.scGRN(po1, sku1, 10, 8)
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku1, "qty": 8, "rate": 110.0}})
	p.scBill(po1, v, "Matched", map[string]interface{}{"bill_lines": string(lines)})
	// Past its date and nothing received: counts against fill, not against on-time.
	_, _ = p.scPO(v, "2020-01-01", 100, 10)
	// Received long after its date: late, but filled.
	sku3, po3 := p.scPO(v, "2020-01-01", 100, 10)
	p.scGRN(po3, sku3, 10, 10)
	p.scBill(po3, v, "MismatchHold", nil)

	s, err := VendorScorecardFor(p.tenant, v, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Orders != 3 || s.Receipts != 2 || s.Bills != 2 {
		t.Fatalf("counts: %+v", s)
	}
	wantPct(t, "on time", s.OnTimePct, 50)
	wantPct(t, "fill", s.FillPct, 66.7)
	wantPct(t, "qc reject", s.QCRejectPct, 10) // 2 of 20 received
	wantPct(t, "price variance", s.PriceVariancePct, 10)
	wantPct(t, "bill mismatch", s.MismatchPct, 50)
	if s.BillMismatches != 1 {
		t.Fatalf("mismatches = %d", s.BillMismatches)
	}
}

func TestStage14813NoDataIsNotZero(t *testing.T) {
	p := newPR14812(t)
	// A vendor with nothing in the period: every measure is absent, not 0.
	s, err := VendorScorecardFor(p.tenant, "V14813-none-"+p.run, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.OnTimePct != nil || s.FillPct != nil || s.QCRejectPct != nil || s.PriceVariancePct != nil || s.MismatchPct != nil || s.Orders != 0 {
		t.Fatalf("empty vendor: %+v", s)
	}
	// An order with no receipt, not yet due: no on-time, fill, QC or price figure.
	v := "V14813-open-" + p.run
	today, _ := time.Parse("2006-01-02", BusinessToday())
	p.scPO(v, today.AddDate(0, 0, 9).Format("2006-01-02"), 100, 10)
	s, err = VendorScorecardFor(p.tenant, v, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Orders != 1 || s.OnTimePct != nil || s.FillPct != nil || s.QCRejectPct != nil || s.PriceVariancePct != nil {
		t.Fatalf("open order: %+v", s)
	}
	// The period decides which orders count.
	s, _ = VendorScorecardFor(p.tenant, v, "2001-01-01", "2001-12-31")
	if s.Orders != 0 {
		t.Fatalf("an old period still counted today's order: %+v", s)
	}
	// The report returns "No data", not 0%.
	def, ok := reportRegistry["vendor-scorecard"]
	if !ok {
		t.Fatal("vendor-scorecard is not registered")
	}
	rows, err := def.Run(p.tenant, map[string]string{"vendor": v})
	if err != nil || len(rows) != 1 || rows[0]["on_time"] != "No data" {
		t.Fatalf("report: %v %v", err, rows)
	}
}

func (p *pr14812) scBill(poID, vendor, status string, extra map[string]interface{}) {
	id := p.id("VI")
	data := map[string]interface{}{"id": id, "invoice_number": id, "vendor_id": vendor, "po_id": poID, "status": status}
	for k, v := range extra {
		data[k] = v
	}
	p.doc(id, "VendorInvoice", status, data)
}
