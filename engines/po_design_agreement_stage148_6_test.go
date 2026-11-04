package engines

import (
	"encoding/json"
	"strings"
	"testing"
)

func askDesignRows(items string, vendor string, rows ...map[string]interface{}) map[string]interface{} {
	asked := make([]interface{}, len(rows))
	for i, r := range rows {
		asked[i] = r
	}
	return map[string]interface{}{"vendor": vendor, "items": items, "design_rows": asked}
}

// The grid's rows may have no item yet. They go by design and category, the two
// scopes a new item will have, and what they are given is what the save stamps.
func TestStage1486DesignRowTakesTheAgreementPrice(t *testing.T) {
	f := newAgreementFixture(t)
	defer f.cleanup()
	const (
		v    = "TEST14861-V"
		des  = "TEST14861-DES"
		cat  = "TEST14861-CAT"
		sku  = "TEST14861-I1"
		sku2 = "TEST14861-I2"
	)
	f.insert(v, "Vendor", "Active", map[string]interface{}{"code": v, "name": v})
	f.item(sku, des, cat, 5)
	f.agreement("TEST14861-A", v, "2026-01-01", "",
		map[string]interface{}{"sku": sku, "price": 120, "vendor_code": "VA-1"},
		map[string]interface{}{"design": des, "price": 100},
		map[string]interface{}{"category": cat, "price": 80, "min_qty": 1, "max_qty": 9},
		map[string]interface{}{"category": cat, "price": 75, "min_qty": 10})

	got := PriceDesignRows("default", askDesignRows("", v,
		map[string]interface{}{"sku": sku, "design": des, "category": cat, "qty": 1},       // an existing combination: its own row
		map[string]interface{}{"sku": "", "design": des, "category": cat, "qty": 1},        // a new combination of a loaded design
		map[string]interface{}{"sku": "", "design": "", "category": cat, "qty": 2},         // a new design: its category
		map[string]interface{}{"sku": "", "design": "", "category": cat, "qty": 10},        // the next break
		map[string]interface{}{"sku": "", "design": "", "category": "TEST-NONE", "qty": 1}, // nothing covers it
	))
	want := []float64{120, 100, 80, 75, 0}
	if len(got) != len(want) {
		t.Fatalf("answers %+v", got)
	}
	for i, rate := range want {
		if got[i].Rate != rate {
			t.Errorf("row %d: rate %v, want %v (%q)", i+1, got[i].Rate, rate, got[i].Note)
		}
	}
	if got[4].Note != "no agreement for this item" {
		t.Errorf("why: %q", got[4].Note)
	}

	// The same answer the save gives once the item exists.
	f.item(sku2, des, cat, 5)
	if byItem := PriceDesignRows("default", askDesignRows("", v, map[string]interface{}{"sku": sku2, "category": "TEST-NONE", "qty": 1})); len(byItem) != 1 || byItem[0].Rate != 100 {
		t.Errorf("an existing item goes by its own design: %+v", byItem)
	}
	raw, _ := json.Marshal([]map[string]interface{}{{"sku": sku2, "qty": 1, "rate": got[1].Rate, "agreement_rate": got[1].Rate}})
	payload := map[string]interface{}{"vendor": v, "items": string(raw)}
	if err := StampPOPricing("default", payload); err != nil {
		t.Fatal(err)
	}
	line := poLinesOf(t, payload)[0]
	if numFromInterface(line["rate"]) != 100 || numFromInterface(line["agreement_rate"]) != 100 {
		t.Errorf("saved line %v, want the design's 100 kept as the agreement's", line)
	}
}

// It says why, in the words a PO line uses.
func TestStage1486DesignRowSaysWhyItHasNoPrice(t *testing.T) {
	f := newAgreementFixture(t)
	defer f.cleanup()
	const (
		v, vNone = "TEST14862-V", "TEST14862-VN"
		exp      = "TEST14862-EXP"
		future   = "TEST14862-FUT"
		brk      = "TEST14862-BRK"
		disc     = "TEST14862-DSC"
	)
	f.insert(v, "Vendor", "Active", map[string]interface{}{"code": v, "name": v})
	f.insert(vNone, "Vendor", "Active", map[string]interface{}{"code": vNone, "name": vNone})
	f.agreement("TEST14862-A", v, "2026-01-01", "",
		map[string]interface{}{"category": exp, "price": 70, "valid_to": "2026-10-12"},
		map[string]interface{}{"category": future, "price": 70, "valid_from": "2099-03-01"},
		map[string]interface{}{"category": brk, "price": 70, "min_qty": 50},
		map[string]interface{}{"category": disc, "discount_pct": 5})

	got := PriceDesignRows("default", askDesignRows("", v,
		map[string]interface{}{"category": exp, "qty": 1},
		map[string]interface{}{"category": future, "qty": 1},
		map[string]interface{}{"category": brk, "qty": 1},
		map[string]interface{}{"category": disc, "qty": 1},
	))
	if len(got) != 4 {
		t.Fatalf("answers %+v", got)
	}
	for i, want := range []string{"no agreement: expired 12 Oct", "no agreement: starts 1 Mar", "no agreement: qty below the 50+ break", "agreement sets no price"} {
		if got[i].Rate != 0 || got[i].Note != want {
			t.Errorf("row %d: %+v, want no rate and %q", i+1, got[i], want)
		}
	}
	none := PriceDesignRows("default", askDesignRows("", vNone, map[string]interface{}{"category": exp, "qty": 1}))
	if len(none) != 1 || none[0].Rate != 0 || none[0].Note != "" {
		t.Errorf("a vendor with no agreement at all has nothing to explain: %+v", none)
	}
	nobody := PriceDesignRows("default", askDesignRows("", "", map[string]interface{}{"category": exp, "qty": 1}))
	if len(nobody) != 1 || nobody[0].Rate != 0 || nobody[0].Note != "" {
		t.Errorf("no vendor: %+v", nobody)
	}
}

// A break counted across the PO reads the rows being added and the lines already on
// it, as the save will.
func TestStage1486DesignRowsCountAcrossThePO(t *testing.T) {
	f := newAgreementFixture(t)
	defer f.cleanup()
	const (
		v   = "TEST14863-V"
		cat = "TEST14863-CAT"
		sku = "TEST14863-I1"
	)
	f.insert(v, "Vendor", "Active", map[string]interface{}{"code": v, "name": v})
	f.item(sku, "", cat, 5)
	f.agreement("TEST14863-A", v, "2026-01-01", "",
		map[string]interface{}{"category": cat, "price": 60, "min_qty": 10, "count_across_po": "Yes"})

	on := itemsJSON(map[string]interface{}{"sku": sku, "qty": 4, "rate": 90})
	row := func(qty int) map[string]interface{} { return map[string]interface{}{"category": cat, "qty": qty} }
	if got := PriceDesignRows("default", askDesignRows(on, v, row(3), row(3))); len(got) != 2 || got[0].Rate != 60 || got[1].Rate != 60 {
		t.Errorf("4 on the PO and 3 + 3 in the grid make 10: %+v", got)
	}
	if got := PriceDesignRows("default", askDesignRows(on, v, row(3))); len(got) != 1 || got[0].Rate != 0 || !strings.Contains(got[0].Note, "10+") {
		t.Errorf("7 is below the break: %+v", got)
	}
}
