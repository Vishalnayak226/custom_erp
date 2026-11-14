package engines

import (
	"testing"
)

// 170.16: a SCAR from a vendor non-conformance, its steps in order, and the
// scorecard's defects per million and open SCARs.
func TestSupplierSCARAndScorecard(t *testing.T) {
	p := newPR14812(t)
	v := "V170-" + p.run
	p.doc(v, "Vendor", "Active", map[string]interface{}{"code": v, "name": "Vendor " + v})
	sku, po := p.scPO(v, "2099-01-01", 100, 20)
	p.scGRN(po, sku, 20, 18) // 2 of 20 rejected = 100000 per million
	ncr := p.id("NCR")
	p.doc(ncr, "NonConformanceReport", "Draft", map[string]interface{}{"code": ncr, "description": "Cracked housing", "item": sku, "batch_no": "L9", "status": "Draft"})
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		if err == nil {
			return ""
		}
		return err.Error()
	}
	if _, err := OpenSCAR(p.tenant, SCARInput{Vendor: v, NCRID: ncr, DueDate: "tomorrow"}, "system"); code(err) != "QUALIT-1763" {
		t.Fatalf("bad date: %v", err)
	}
	id, err := OpenSCAR(p.tenant, SCARInput{Vendor: v, NCRID: ncr, DueDate: "2099-02-01"}, "system")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	p.docs = append(p.docs, id)
	d, _, _ := fetchDocData(p.tenant, scarDoctype, id)
	if d["issue"] != "Cracked housing" || d["batch_no"] != "L9" || d["status"] != ScarOpen {
		t.Fatalf("inherited from the NCR: %v", d)
	}
	if _, err := OpenSCAR(p.tenant, SCARInput{Vendor: v, NCRID: ncr, DueDate: "2099-02-01"}, "system"); code(err) != "QUALIT-1764" {
		t.Fatalf("second open SCAR on one NCR: %v", err)
	}

	s, err := VendorScorecardFor(p.tenant, v, "", "")
	if err != nil {
		t.Fatal(err)
	}
	wantPct(t, "defects per million", s.DefectsPerMillion, 100000)
	if s.OpenSCARs != 1 {
		t.Fatalf("open SCARs = %d", s.OpenSCARs)
	}

	// Steps run in order; the last closes the SCAR and it leaves the count.
	if err := AdvanceSCAR(p.tenant, id, "action", "Re-tool", "system"); code(err) != "QUALIT-1765" {
		t.Fatalf("action before root cause: %v", err)
	}
	if err := AdvanceSCAR(p.tenant, id, "root_cause", " ", "system"); code(err) != "QUALIT-1765" {
		t.Fatalf("blank text: %v", err)
	}
	for _, st := range [][2]string{{"root_cause", "Worn die"}, {"action", "Die replaced"}, {"verification", "Three lots clean"}} {
		if err := AdvanceSCAR(p.tenant, id, st[0], st[1], "system"); err != nil {
			t.Fatalf("%s: %v", st[0], err)
		}
	}
	if _, status, _ := fetchDocData(p.tenant, scarDoctype, id); status != ScarClosed {
		t.Fatalf("status %s", status)
	}
	if s, _ = VendorScorecardFor(p.tenant, v, "", ""); s.OpenSCARs != 0 {
		t.Fatalf("open SCARs after close = %d", s.OpenSCARs)
	}
	id2, err := OpenSCAR(p.tenant, SCARInput{Vendor: v, NCRID: ncr, DueDate: "2099-02-01"}, "system")
	if err != nil {
		t.Fatalf("a closed SCAR frees the NCR for a new one: %v", err)
	}
	p.docs = append(p.docs, id2)
}
