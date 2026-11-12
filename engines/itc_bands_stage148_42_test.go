package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"
)

// Stage 148.42: GST credit eligibility and value bands.

func itc42On(t *testing.T, on bool) {
	t.Helper()
	val := "false"
	if on {
		val = "true"
	}
	if err := SetSetting("default", SettingITC, val, "t14842"); err != nil {
		t.Fatalf("setting: %v", err)
	}
}

// itc42Setup turns the setting on and restores it after.
func itc42Setup(t *testing.T) *mc1481 {
	m := newMC1481(t)
	itc42On(t, true)
	t.Cleanup(func() { itc42On(t, false) })
	return m
}

// itc42Item makes a taxable 18 percent item under its own HSN row.
func (m *mc1481) itc42Item(hsnRow map[string]interface{}) (sku, hsn string) {
	m.t.Helper()
	hsn = fmt.Sprintf("%08d", time.Now().UnixNano()%100000000)
	row := map[string]interface{}{"code": HSNCatalogID(hsn), "hsn": hsn, "status": "Active"}
	for k, v := range hsnRow {
		row[k] = v
	}
	m.doc(HSNCatalogID(hsn), "HSNCode", "Active", row)
	sku = m.item(map[string]interface{}{"hsn_code": hsn, "tax_treatment": "Taxable", "gst_rate": 18.0})
	return sku, hsn
}

func itc42PO(sku string, price float64, itc string) map[string]interface{} {
	line := map[string]interface{}{"sku": sku, "qty": 1, "rate": price}
	if itc != "" {
		line["itc"] = itc
	}
	raw, _ := json.Marshal([]interface{}{line})
	return map[string]interface{}{"items": string(raw), "gst_mode": GSTModeExclusive}
}

// inputCredit is the GST report's input tax credit over all time.
func (m *mc1481) inputCredit() float64 {
	m.t.Helper()
	s, err := GetGSTReturnSummary(m.tenant, "2000-01-01", "2100-01-01")
	if err != nil {
		m.t.Fatalf("gst report: %v", err)
	}
	return s.InputTaxCredit
}

func (m *mc1481) patchDoc(id, patch string) {
	m.t.Helper()
	if _, err := db.DB.Exec("UPDATE "+m.schema+".documents SET data = data || $2::jsonb WHERE id = $1", id, patch); err != nil {
		m.t.Fatal(err)
	}
}

func TestStage14842ValueBandPicksTheRateByPrice(t *testing.T) {
	m := itc42Setup(t)
	sku, _ := m.itc42Item(map[string]interface{}{"value_bands": "1000:5,2500:12,:18"})
	for _, c := range []struct{ price, tax float64 }{{900, 45}, {1000, 50}, {2000, 240}, {2500, 300}, {5000, 900}} {
		b, err := ComputePurchaseOrderGST(m.tenant, itc42PO(sku, c.price, ""))
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(b.TotalTax-c.tax) > 0.01 {
			t.Errorf("price %v: tax %v, want %v", c.price, b.TotalTax, c.tax)
		}
	}
	pv, err := PreviewPurchaseOrder(m.tenant, itc42PO(sku, 900, ""))
	if err != nil || len(pv.Lines) != 1 || pv.Lines[0].GSTRate != 5 {
		t.Errorf("preview: %v %+v", err, pv)
	}
}

func TestStage14842NoBandOrSettingOffKeepsTheRowRate(t *testing.T) {
	m := itc42Setup(t)
	plain, _ := m.itc42Item(nil)
	b, err := ComputePurchaseOrderGST(m.tenant, itc42PO(plain, 900, ""))
	if err != nil || math.Abs(b.TotalTax-162) > 0.01 {
		t.Errorf("no band: tax %v err %v, want 162", b.TotalTax, err)
	}
	banded, _ := m.itc42Item(map[string]interface{}{"value_bands": "1000:5,:18"})
	itc42On(t, false)
	b, err = ComputePurchaseOrderGST(m.tenant, itc42PO(banded, 900, "Blocked"))
	if err != nil || math.Abs(b.TotalTax-162) > 0.01 || b.BlockedTax != 0 {
		t.Errorf("setting off: tax %v blocked %v err %v, want 162 and 0", b.TotalTax, b.BlockedTax, err)
	}
}

func TestStage14842BadBandsAreRefused(t *testing.T) {
	for _, bad := range []string{"abc", "1000", "2500:12,1000:5", ":5,1000:12", "1000:150", "1000:-1"} {
		if err := PrepareHSNCode(true, map[string]interface{}{"hsn": "6109", "value_bands": bad}); err == nil {
			t.Errorf("bands %q were accepted", bad)
		}
	}
	if err := PrepareHSNCode(true, map[string]interface{}{"hsn": "6109", "value_bands": "1000:5,2500:12,:18", "itc": "Blocked"}); err != nil {
		t.Errorf("good bands refused: %v", err)
	}
	if err := PrepareHSNCode(true, map[string]interface{}{"hsn": "6109", "itc": "Maybe"}); err == nil {
		t.Error("a credit value outside Eligible/Blocked was accepted")
	}
}

func TestStage14842BlockedComesFromLineThenHSNThenVendor(t *testing.T) {
	m := itc42Setup(t)
	plain, _ := m.itc42Item(nil)
	hsnBlocked, _ := m.itc42Item(map[string]interface{}{"itc": "Blocked"})
	blocked := func(sku, line string, vendor map[string]interface{}) float64 {
		p := itc42PO(sku, 1000, line)
		if vendor != nil {
			vid := m.id("V")
			m.doc(vid, "Vendor", "Active", vendor)
			p["vendor"] = vid
		}
		b, err := ComputePurchaseOrderGST(m.tenant, p)
		if err != nil {
			t.Fatal(err)
		}
		return b.BlockedTax
	}
	if got := blocked(plain, "", nil); got != 0 {
		t.Errorf("default: blocked %v, want 0", got)
	}
	if got := blocked(plain, "Blocked", nil); got != 180 {
		t.Errorf("line Blocked: %v, want 180", got)
	}
	if got := blocked(hsnBlocked, "", nil); got != 180 {
		t.Errorf("HSN Blocked: %v, want 180", got)
	}
	if got := blocked(hsnBlocked, "Eligible", nil); got != 0 {
		t.Errorf("line Eligible over HSN Blocked: %v, want 0", got)
	}
	if got := blocked(plain, "", map[string]interface{}{"code": "x", "itc": "Blocked"}); got != 180 {
		t.Errorf("vendor Blocked: %v, want 180", got)
	}
	p := itc42PO(plain, 1000, "Blocked")
	if err := ApplyPurchaseOrderTotals(m.tenant, p); err != nil {
		t.Fatal(err)
	}
	if v, _ := p["itc_blocked_tax"].(float64); v != 180 {
		t.Errorf("PO itc_blocked_tax %v, want 180", p["itc_blocked_tax"])
	}
}

// 10 @ Rs 100 received, a bill of Rs 1,180 with Rs 90 of its Rs 180 GST blocked.
func TestStage14842BlockedGSTStaysOutOfInputCreditInTheJournalAndReport(t *testing.T) {
	m := itc42Setup(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	m.patchDoc(po, `{"grand_total":1180,"gst_breakdown":{"total_tax":180,"cgst":90,"sgst":90,"igst":0},"itc_blocked_tax":90}`)
	bill := m.bill(po, grn, 1180, nil)
	res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 20)
	if err != nil || !res.Matched {
		t.Fatalf("match: %v %+v", err, res)
	}
	d := m.billData(bill)
	if numFromInterface(d["gst_amount"]) != 90 || numFromInterface(d["itc_blocked_amount"]) != 90 || numFromInterface(d["taxable_amount"]) != 1090 {
		t.Fatalf("bill gst %v blocked %v taxable %v, want 90 / 90 / 1090", d["gst_amount"], d["itc_blocked_amount"], d["taxable_amount"])
	}
	before := m.inputCredit()
	m.mustPay(bill)
	if got := m.inputCredit() - before; math.Abs(got-90) > 0.001 {
		t.Errorf("GST report input credit moved by %v, want 90", got)
	}
	if c, a, s, cash := m.gl(bill, "1500"), m.gl(bill, "1200"), m.gl(bill, "2100"), m.gl(bill, "1100"); c != 9000 || a != 9000 || s != 100000 || cash != -118000 {
		t.Errorf("1500 %d, 1200 %d, 2100 %d, 1100 %d, want 9000 / 9000 / 100000 / -118000", c, a, s, cash)
	}
	m.wantBalanced(bill)
}

func TestStage14842NothingBlockedPostsAllTheTaxAsCredit(t *testing.T) {
	m := itc42Setup(t)
	sku := m.item(nil)
	grn, po := m.receive(sku, 10, 100)
	m.patchDoc(po, `{"grand_total":1180,"gst_breakdown":{"total_tax":180,"cgst":90,"sgst":90,"igst":0}}`)
	bill := m.bill(po, grn, 1180, nil)
	if res, err := MatchVendorInvoice(m.tenant, bill, po, grn, 20); err != nil || !res.Matched {
		t.Fatalf("match: %v", err)
	}
	m.mustPay(bill)
	if c, a := m.gl(bill, "1500"), m.gl(bill, "1200"); c != 18000 || a != 0 {
		t.Errorf("1500 %d, 1200 %d, want 18000 and 0", c, a)
	}
}
