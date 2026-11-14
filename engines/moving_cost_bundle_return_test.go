package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 148.1d: a bundle comes back as its components at the cost the sale booked.

// bundleReturnFixture: kit = 2 x a (Rs 100) + 1 x b (Rs 50), Rs 250 a kit.
func bundleReturnFixture(t *testing.T, stock float64) (m *mc1481, a, b, kit, g1, g2 string) {
	m = newMC1481(t)
	a, b = m.item(nil), m.item(nil)
	kit = m.item(map[string]interface{}{"item_type": "Virtual Kit"})
	comps, _ := json.Marshal([]map[string]interface{}{{"sku": a, "quantity": 2}, {"sku": b, "quantity": 1}})
	m.doc(m.id("PB"), "ProductBundle", "Active", map[string]interface{}{"bundle_sku": kit, "name": "Gift set", "fulfillment_mode": "Virtual", "pricing_mode": "Fixed Price", "fixed_price": 1000.0, "components": string(comps)})
	g1, _ = m.receive(a, stock*2, 100)
	g2, _ = m.receive(b, stock, 50)
	return
}

func TestBundleReturnFullComesBackAtBookedCost(t *testing.T) {
	m, a, b, kit, g1, g2 := bundleReturnFixture(t, 4)
	cart, _ := m.mustSell(kit, 2, nil)
	if got := m.gl(cart, "5100"); got != 50000 {
		t.Fatalf("COGS %d, want 50000", got)
	}
	ret := m.custReturn(cart, kit, "Sellable", ReturnItemInput{Qty: 2})
	if c, s := m.gl(ret, "5100"), m.gl(ret, "1200"); c != -50000 || s != 50000 {
		t.Errorf("return posted 5100 %d, 1200 %d: want -50000 / 50000", c, s)
	}
	if m.onHand(a) != 8 || m.onHand(b) != 4 {
		t.Errorf("components on hand a=%v b=%v, want 8 and 4", m.onHand(a), m.onHand(b))
	}
	if m.onHand(kit) != 0 {
		t.Errorf("the kit's own code holds %v, want 0", m.onHand(kit))
	}
	m.wantBalanced(ret)
	m.wantValueEqualsGL([]string{a, b, kit}, g1, g2, cart, ret)
	// Replay: a second QC on the same return is refused and posts nothing more.
	if _, _, err := ApplyReturnQC(m.tenant, ret, map[string]string{kit: "Sellable"}, "t1481-q"); err == nil {
		t.Errorf("a second QC was accepted")
	}
	if c := m.gl(ret, "5100"); c != -50000 {
		t.Errorf("after replay 5100 %d, want -50000", c)
	}
}

func TestBundleReturnPartialIsProRataByQty(t *testing.T) {
	m, a, b, kit, g1, g2 := bundleReturnFixture(t, 4)
	cart, _ := m.mustSell(kit, 4, nil)
	r1 := m.custReturn(cart, kit, "Sellable", ReturnItemInput{Qty: 1})
	if c, s := m.gl(r1, "5100"), m.gl(r1, "1200"); c != -25000 || s != 25000 {
		t.Errorf("first return 5100 %d, 1200 %d, want -25000 / 25000", c, s)
	}
	if m.onHand(a) != 2 || m.onHand(b) != 1 {
		t.Errorf("after one kit back a=%v b=%v, want 2 and 1", m.onHand(a), m.onHand(b))
	}
	r2 := m.custReturn(cart, kit, "Sellable", ReturnItemInput{Qty: 3})
	if c, s := m.gl(r2, "5100"), m.gl(r2, "1200"); c != -75000 || s != 75000 {
		t.Errorf("second return 5100 %d, 1200 %d, want -75000 / 75000", c, s)
	}
	m.wantBalanced(r1)
	m.wantBalanced(r2)
	m.wantValueEqualsGL([]string{a, b, kit}, g1, g2, cart, r1, r2)
}

// A bundle sold before parts were recorded comes back as it always did: nothing.
func TestBundleReturnWithoutRecordedPartsStaysAsBefore(t *testing.T) {
	m, a, b, kit, g1, g2 := bundleReturnFixture(t, 4)
	cart, _ := m.mustSell(kit, 1, nil)
	if _, err := db.DB.Exec(`UPDATE `+m.schema+`.documents SET data = data - 'sale_costs' WHERE id = $1`, cart); err != nil {
		t.Fatal(err)
	}
	ret := m.custReturn(cart, kit, "Sellable", ReturnItemInput{})
	if c, s := m.gl(ret, "5100"), m.gl(ret, "1200"); c != 0 || s != 0 {
		t.Errorf("return posted 5100 %d, 1200 %d: want nothing", c, s)
	}
	if m.onHand(a) != 6 || m.onHand(b) != 3 {
		t.Errorf("components changed: a=%v b=%v", m.onHand(a), m.onHand(b))
	}
	_ = g1
	_ = g2
}
