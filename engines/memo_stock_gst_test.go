package engines

import (
	"testing"

	"custom_erp/db"
)

// memoGSTFixture gives the test store a legal entity in Maharashtra (27) and a
// vendor with the given GSTIN ("" = unregistered).
func memoGSTFixture(m *mc1481, gstin string) (vendor, sku string) {
	ent := m.id("LE")
	m.doc(ent, "LegalEntity", "Active", map[string]interface{}{"code": ent, "name": ent, "gstin": "27AAPFU0939F1ZV", "state": "Maharashtra"})
	if _, err := db.DB.Exec(`UPDATE `+m.schema+`.documents SET data = data || $2::jsonb WHERE id = $1`, m.loc, `{"legal_entity":"`+ent+`"}`); err != nil {
		m.t.Fatal(err)
	}
	vendor = m.id("VEND")
	data := map[string]interface{}{"code": vendor, "name": vendor}
	if gstin != "" {
		data["gstin"] = gstin
	}
	m.doc(vendor, "Vendor", "Active", data)
	sku = m.item(map[string]interface{}{"tax_treatment": "Taxable", "gst_rate": 12.0})
	return
}

func memoGSTSale(t *testing.T, gstin string) (m *mc1481, bill string, cart string) {
	m = newMC1481(t)
	memoModule(t, m, true)
	vendor, sku := memoGSTFixture(m, gstin)
	m.receive(sku, 10, 100)
	memoReceive(t, m, vendor, sku, 5, 80)
	cart, _ = m.mustSell(sku, 5, nil)
	ids, _ := memoBills(t, m, cart)
	if len(ids) != 1 {
		t.Fatalf("memo bills %v, want one", ids)
	}
	return m, ids[0], cart
}

// 148.16d: the memo rate is exclusive of GST; a registered vendor's bill adds the
// item's rate on top and books it to input credit.
func TestMemoBillGSTRegisteredIntraState(t *testing.T) {
	m, bill, cart := memoGSTSale(t, "27AABCU9603R1ZM") // Maharashtra, as the store
	d := m.pieceData("VendorInvoice", "code", bill)
	if d["invoice_amount"] != 448.0 || d["gst_amount"] != 48.0 || d["interstate"] != false {
		t.Fatalf("bill %v, want 448 with 48 GST, intra-state", d)
	}
	for acct, want := range map[string]int64{"5100": 40000, "1500": 4800, "2100": -44800} {
		if got := m.gl(bill, acct); got != want {
			t.Errorf("account %s %d paise, want %d", acct, got, want)
		}
	}
	m.wantBalanced(bill)
	m.wantBalanced(cart)
	split, _ := d["gst_split"].(map[string]interface{})
	if split["cgst"] != 24.0 || split["sgst"] != 24.0 || split["igst"] != 0.0 {
		t.Errorf("split %v, want CGST 24 / SGST 24", split)
	}
	// A second sale finds no memo units left: no second bill, the first posts once.
	m.mustSell(m.skus[0], 1, nil)
	if ids, _ := memoBills(t, m, cart); len(ids) != 1 {
		t.Errorf("bills after more sales %v, want still one", ids)
	}
	if got := m.gl(bill, "1500"); got != 4800 {
		t.Errorf("input GST %d after later sales, want 4800 once", got)
	}
}

func TestMemoBillGSTRegisteredInterState(t *testing.T) {
	m, bill, _ := memoGSTSale(t, "29AABCU9603R1ZM") // Karnataka against a Maharashtra store
	d := m.pieceData("VendorInvoice", "code", bill)
	if d["invoice_amount"] != 448.0 || d["interstate"] != true {
		t.Fatalf("bill %v, want 448, inter-state", d)
	}
	split, _ := d["gst_split"].(map[string]interface{})
	if split["igst"] != 48.0 || split["cgst"] != 0.0 {
		t.Errorf("split %v, want IGST 48", split)
	}
	if got := m.gl(bill, "1500"); got != 4800 {
		t.Errorf("input GST %d, want 4800", got)
	}
	m.wantBalanced(bill)
}

func TestMemoBillGSTUnregisteredUnchanged(t *testing.T) {
	m, bill, _ := memoGSTSale(t, "")
	d := m.pieceData("VendorInvoice", "code", bill)
	if d["invoice_amount"] != 400.0 || d["gst_amount"] != nil {
		t.Fatalf("bill %v, want 400 and no GST", d)
	}
	if got := m.gl(bill, "1500"); got != 0 {
		t.Errorf("unregistered vendor got input GST %d", got)
	}
	if got := m.gl(bill, "5100"); got != 40000 {
		t.Errorf("5100 %d, want 40000", got)
	}
	m.wantBalanced(bill)
}
