package engines

import (
	"custom_erp/db"
	"testing"
)

// Review fix: a person sees and decides only the stock age inspections of the
// stores they work at, and a scrap takes only items the inspection covers.
func TestStage146311StockAgeStoreScopeAndScrap(t *testing.T) {
	f := inspTenant(t)
	for _, l := range []string{"L1", "L2"} {
		f.insert(l, "Location", "Active", map[string]interface{}{"code": l, "name": "Store " + l, "status": "Active"})
	}
	f.insert("R-CAT", "InspectionRule", "Active", map[string]interface{}{"rule_kind": "Stock age", "ref": "Plain", "threshold": 30, "status": "Active"})
	db.DB.Exec("DELETE FROM " + f.schema + ".documents WHERE id = 'IA-DAMAGE'") // a starter code since 163.1
	f.insert("IA-DAMAGE", "ReasonCode", "Active", map[string]interface{}{"code": "IA-DAMAGE", "category": ReasonInventoryAdjust, "status": "Active"})
	f.receive(t, "G-S1", "V1", "L1", line("I-PLAIN", 10))
	f.receive(t, "G-S2", "V1", "L2", line("I-PLAIN", 6), line("I-OPT", 4))
	f.backdate(t, "I-PLAIN", 25)
	f.backdate(t, "I-OPT", 25)
	if err := SetSetting(f.tenant, SettingAgeInspection, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if n, err := RunStockAgeInspections(f.tenant); err != nil || n != 2 {
		t.Fatalf("filed %d (%v), want one per store", n, err)
	}
	l1 := StoreAccess{Home: "L1", Stores: map[string]bool{"L1": true}}
	all, _ := ListStockAgeInspections(f.tenant, "HR/Admin", "", StoreAccess{All: true})
	mine, err := ListStockAgeInspections(f.tenant, "HR/Admin", "", l1)
	if err != nil || len(all) != 2 || len(mine) != 1 || mine[0].Location != "L1" {
		t.Fatalf("all=%d mine=%+v (%v): a store user must see only L1", len(all), mine, err)
	}
	none, _ := ListStockAgeInspections(f.tenant, "HR/Admin", "", StoreAccess{})
	if len(none) != 0 {
		t.Fatalf("a user with no stores saw %d", len(none))
	}
	var other, own string
	for _, v := range all {
		if v.Location == "L2" {
			other = v.ID
		} else {
			own = v.ID
		}
	}
	type lines = []struct {
		SKU string `json:"sku"`
		Qty int    `json:"qty"`
	}
	scrap := func(id string, ls lines) error {
		_, err := ResolveStockAgeInspection(f.tenant, "u", "HR/Admin", l1, StockAgeOutcomeRequest{ID: id, Outcome: "Scrap", ReasonCode: "IA-DAMAGE", Lines: ls})
		return err
	}
	if err := scrap(other, lines{{SKU: "I-PLAIN", Qty: 1}}); err == nil {
		t.Fatal("an L1 user decided L2's inspection")
	}
	if oh, _, _ := f.stock(t, "I-PLAIN", "L2"); oh != 6 {
		t.Fatalf("L2 stock moved to %d", oh)
	}
	if err := scrap(own, lines{{SKU: "I-OPT", Qty: 1}}); err == nil {
		t.Fatal("an item the rule does not cover was scrapped")
	}
	if err := scrap(own, lines{{SKU: "NO-SUCH-ITEM", Qty: 1}}); err == nil {
		t.Fatal("an unknown item was scrapped")
	}
	if err := scrap(own, lines{{SKU: "I-PLAIN", Qty: 6}, {SKU: "I-PLAIN", Qty: 5}}); err == nil {
		t.Fatal("more than the store holds was scrapped")
	}
	if oh, _, _ := f.stock(t, "I-PLAIN", "L1"); oh != 10 {
		t.Fatalf("a refused scrap moved L1 stock to %d", oh)
	}
	if err := scrap(own, lines{{SKU: "I-PLAIN", Qty: 3}}); err != nil {
		t.Fatalf("a covered scrap within stock: %v", err)
	}
	if oh, _, _ := f.stock(t, "I-PLAIN", "L1"); oh != 7 {
		t.Fatalf("L1 on hand %d, want 7", oh)
	}
}
