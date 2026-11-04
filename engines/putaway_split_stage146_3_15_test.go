package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 146.3.15: put-away split across bins, and a capacity warning.
// Split is behind wms.putaway_split (off); the warning % defaults to 0 (none).

func psSeed(t *testing.T, schema string, insert func(id, doctype, status, createdBy string, data map[string]interface{})) (loc, sku string) {
	t.Helper()
	loc, sku = "PS1", "PS1-SKU"
	insert("ITEM-"+sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Q"})
	insert("ITEM-PS1-OTHER", "Item", "Active", "system", map[string]interface{}{"code": "PS1-OTHER", "name": "O"})
	insert("PS-STRAT", "PutawayStrategy", "Active", "system", map[string]interface{}{"criteria": "capacity", "status": "Active"})
	bin := func(code string, capacity int, extra map[string]interface{}) {
		m := map[string]interface{}{"bin_code": code, "location": loc, "status": "Active", "capacity": capacity}
		for k, v := range extra {
			m[k] = v
		}
		insert("BIN-"+code, "Bin", "Active", "system", m)
	}
	bin("PS-A", 10, nil)                                                   // 7 held -> room 3
	bin("PS-B", 10, nil)                                                   // empty -> room 10
	bin("PS-S", 10, map[string]interface{}{"bin_mix_rule": "Single item"}) // holds another item
	bin("PS-E", 10, map[string]interface{}{"bin_mix_rule": "Single item"}) // empty single-item bin -> room 10
	put := func(b, s string, q int) {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', $4)", b, s, loc, q); err != nil {
			t.Fatal(err)
		}
	}
	put("PS-A", "PS1-OTHER", 7)
	put("PS-S", "PS1-OTHER", 4)
	return
}

func TestStage146315SplitFillsBestThenNext(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	loc, sku := psSeed(t, schema, insert)

	// Off: 12 pieces fit no single bin -> no suggestion, as today.
	plan, err := SuggestPutawayPlan(tenantID, sku, loc, 12, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.BinCode != "" || len(plan.Parts) != 0 {
		t.Fatalf("split off: want no suggestion for 12, got %+v", plan)
	}
	if err := SetSetting(tenantID, "wms.putaway_split", "true", "test"); err != nil {
		t.Fatal(err)
	}
	plan, err = SuggestPutawayPlan(tenantID, sku, loc, 12, "")
	if err != nil {
		t.Fatal(err)
	}
	// Best fit (least room first) = PS-A with 3, then the next bin; PS-S is never used.
	got := map[string]int{}
	total := 0
	for _, p := range plan.Parts {
		got[p.BinCode] += p.Qty
		total += p.Qty
	}
	if got["PS-S"] != 0 {
		t.Fatalf("a single-item bin holding another item was used: %+v", plan.Parts)
	}
	if total != 12 || got["PS-A"] != 3 || len(plan.Parts) < 2 || plan.Parts[0].BinCode != "PS-A" {
		t.Fatalf("parts = %+v; want 12 in all, 3 into PS-A first", plan.Parts)
	}
	// A quantity that fits one bin stays a single bin.
	plan, _ = SuggestPutawayPlan(tenantID, sku, loc, 2, "")
	if len(plan.Parts) != 1 || plan.Parts[0].Qty != 2 || plan.BinCode == "" {
		t.Fatalf("2 pieces: want one part, got %+v", plan)
	}
	// More than every bin can take: the parts cover what fits and say what is left.
	plan, _ = SuggestPutawayPlan(tenantID, sku, loc, 50, "")
	sum := 0
	for _, p := range plan.Parts {
		sum += p.Qty
	}
	if sum != 23 || !strings.Contains(plan.Reason, "27") {
		t.Fatalf("50 pieces: parts %+v reason %q; want 23 placed and 27 left over named", plan.Parts, plan.Reason)
	}
}

func TestStage146315CapacityWarning(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	loc, sku := psSeed(t, schema, insert)

	plan, err := SuggestPutawayPlan(tenantID, sku, loc, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Warning != "" {
		t.Fatalf("warning %% is 0 but got %q", plan.Warning)
	}
	if err := SetSetting(tenantID, "wms.putaway_fill_warn_pct", "80", "test"); err != nil {
		t.Fatal(err)
	}
	// PS-A holds 7 of 10: 2 more is 90%.
	if w := PutawayFillWarning(tenantID, "PS-A", sku, 2); !strings.Contains(w, "PS-A") || !strings.Contains(w, "90%") {
		t.Fatalf("warning = %q; want PS-A at 90%%", w)
	}
	if w := PutawayFillWarning(tenantID, "PS-B", sku, 2); w != "" {
		t.Fatalf("PS-B would be 20%% full but warned: %q", w)
	}
	// The suggestion carries the warning of the bin it picks (PS-A: least room that fits).
	plan, err = SuggestPutawayPlan(tenantID, sku, loc, 2, "")
	if err != nil || plan.BinCode != "PS-A" || !strings.Contains(plan.Warning, "90%") {
		t.Fatalf("plan = %+v %v; want PS-A with a 90%% warning", plan, err)
	}
}
