package engines

import (
	"encoding/json"
	"sort"
	"testing"
)

// Stage 146.4.2: approved purchase proposals become one Draft PO per default
// vendor, priced from the vendor agreement and rounded up to its pack multiple.
// This is the second PO-creating path (the first is the PO form).
func TestStage14642ProposalsBecomeOnePOPerDefaultVendor(t *testing.T) {
	f := newAgreementFixture(t)
	defer f.cleanup()
	const (
		loc        = "TEST14642-LOC"
		va, vb     = "TEST14642-VA", "TEST14642-VB"
		sA, sB, sC = "TEST14642-A", "TEST14642-B", "TEST14642-C"
		pA, pB, pC = "TEST14642-PA", "TEST14642-PB", "TEST14642-PC"
		sNo, pNo   = "TEST14642-NO", "TEST14642-PNO"
	)
	f.insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": loc, "state": "Maharashtra"})
	for _, v := range []string{va, vb} {
		f.insert(v, "Vendor", "Active", map[string]interface{}{"code": v, "name": v, "state": "Maharashtra"})
	}
	mk := func(sku, vendor string, cost float64) {
		f.insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku, "hsn_code": "6204", "gst_rate": 5.0,
			"tax_treatment": "Taxable", "standard_cost": cost, "default_vendor": vendor})
	}
	mk(sA, va, 50)
	mk(sB, vb, 60)
	mk(sC, va, 70)
	mk(sNo, "", 10)
	f.agreement("TEST14642-AG", va, "2026-01-01", "",
		map[string]interface{}{"sku": sA, "price": 40, "pack_multiple": 12, "vendor_code": "VA-A"})
	prop := func(id, sku string, qty float64) {
		f.insert(id, "SupplyProposal", ProposalOpen, map[string]interface{}{"sku": sku, "location": loc, "suggested_qty": qty, "source": "Reorder"})
	}
	prop(pA, sA, 10)
	prop(pB, sB, 5)
	prop(pC, sC, 3)
	prop(pNo, sNo, 4)

	if _, err := ApproveSupplyProposals("default", "system", "HQ", ApproveProposalsInput{IDs: []string{pA, pNo}, Action: ProposalPurchase}); err == nil {
		t.Fatal("an item with no default vendor and no vendor chosen must be refused")
	}
	res, err := ApproveSupplyProposals("default", "system", "HQ", ApproveProposalsInput{IDs: []string{pA, pB, pC}, Action: ProposalPurchase})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	for _, id := range res.Documents {
		f.ids = append(f.ids, id)
	}
	if len(res.Documents) != 2 {
		t.Fatalf("want one PO per default vendor (2), got %v", res.Documents)
	}
	byVendor := map[string]map[string]POLineInput{}
	for _, id := range res.Documents {
		d, st, _ := fetchDocData("default", "PurchaseOrder", id)
		if st != "Draft" {
			t.Fatalf("%s status %s", id, st)
		}
		var ls []POLineInput
		_ = json.Unmarshal([]byte(str(d, "items")), &ls)
		m := map[string]POLineInput{}
		for _, l := range ls {
			m[l.SKU] = l
		}
		byVendor[str(d, "vendor")] = m
	}
	var vs []string
	for v := range byVendor {
		vs = append(vs, v)
	}
	sort.Strings(vs)
	if len(vs) != 2 || vs[0] != va || vs[1] != vb {
		t.Fatalf("vendors on the POs: %v", vs)
	}
	a := byVendor[va][sA]
	// agreement price 40 (not the item cost 50); 10 rounded up to the pack of 12
	if a.Rate != 40 || a.Qty != 12 {
		t.Fatalf("line A = %+v, want rate 40 qty 12", a)
	}
	if len(byVendor[va]) != 2 || byVendor[vb][sB].Rate != 60 || byVendor[vb][sB].Qty != 5 {
		t.Fatalf("POs = %+v", byVendor)
	}
}
