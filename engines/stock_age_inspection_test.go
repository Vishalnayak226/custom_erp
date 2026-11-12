package engines

import (
	"fmt"
	"testing"
	"time"

	"custom_erp/db"
)

func (f inspFix) backdate(t *testing.T, sku string, days int) {
	t.Helper()
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET created_at = now() - ($2 || ' days')::interval WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1`, f.schema), sku, fmt.Sprint(days)); err != nil {
		t.Fatal(err)
	}
}

func (f inspFix) ageDocs(t *testing.T, status string) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'StockAgeInspection' AND status = $1`, f.schema), status).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Off: nothing is filed. On: one per store per cohort, item rule overrides the
// category rule, no stock moves, and the outcomes do what they say.
func TestStage146311StockAge(t *testing.T) {
	f := inspTenant(t)
	f.insert("L1", "Location", "Active", map[string]interface{}{"code": "L1", "name": "Store 1", "status": "Active"})
	f.insert("R-CAT", "InspectionRule", "Active", map[string]interface{}{"rule_kind": "Stock age", "ref": "Plain", "threshold": 30, "status": "Active"})
	f.insert("R-ITEM", "InspectionRule", "Active", map[string]interface{}{"rule_kind": "Stock age (item)", "ref": "I-OPT", "threshold": 60, "status": "Active"})
	db.DB.Exec("DELETE FROM " + f.schema + ".documents WHERE id = 'IA-DAMAGE'") // a starter code since 163.1
	f.insert("IA-DAMAGE", "ReasonCode", "Active", map[string]interface{}{"code": "IA-DAMAGE", "category": ReasonInventoryAdjust, "status": "Active"})
	f.receive(t, "G-AGE", "V1", "L1", line("I-PLAIN", 10), line("I-OPT", 5))
	f.backdate(t, "I-PLAIN", 25)
	f.backdate(t, "I-OPT", 40)

	if _, err := RunStockAgeInspections(f.tenant); err == nil {
		t.Fatal("off: the check should refuse")
	}
	if err := SetSetting(f.tenant, SettingAgeInspection, "true", "test"); err != nil {
		t.Fatal(err)
	}
	n, err := RunStockAgeInspections(f.tenant)
	if err != nil || n != 1 {
		t.Fatalf("first run filed %d (%v), want 1: I-PLAIN is 25d old on a 30d rule, 7d ahead; I-OPT is on its own 60d rule at 40d, not yet", n, err)
	}
	if n, _ = RunStockAgeInspections(f.tenant); n != 0 {
		t.Errorf("second run filed %d, want 0 (one per store per cohort)", n)
	}
	if oh, av, hold := f.stock(t, "I-PLAIN", "L1"); oh != 10 || av != 10 || hold != 0 {
		t.Errorf("an inspection must not hold the stock: %d/%d/%d", oh, av, hold)
	}
	list, err := ListStockAgeInspections(f.tenant, "HR/Admin", "", StoreAccess{All: true})
	if err != nil || len(list) != 1 || list[0].Location != "L1" || list[0].DaysLeft != 5 {
		t.Fatalf("list = %+v (%v), want one at L1 due in 5 days", list, err)
	}
	if rep, err := stockAgeDueReport(f.tenant, nil); err != nil || len(rep) != 1 {
		t.Errorf("due report = %d rows (%v), want 1", len(rep), err)
	}

	// An item rule beats the category rule: I-OPT at 100 days is on cohort 1 of 60.
	f.backdate(t, "I-OPT", 100)
	if n, _ = RunStockAgeInspections(f.tenant); n != 1 {
		t.Errorf("item rule filed %d, want 1", n)
	}

	id := list[0].ID
	if _, err := ResolveStockAgeInspection(f.tenant, "u", "HR/Admin", StoreAccess{All: true}, StockAgeOutcomeRequest{ID: id, Outcome: "Re-polish"}); err == nil {
		t.Error("re-polish without a job-work order should refuse")
	}
	if _, err := ResolveStockAgeInspection(f.tenant, "u", "HR/Admin", StoreAccess{All: true}, StockAgeOutcomeRequest{ID: id, Outcome: "Scrap", Lines: []struct {
		SKU string `json:"sku"`
		Qty int    `json:"qty"`
	}{{SKU: "I-PLAIN", Qty: 2}}}); err == nil {
		t.Error("scrap without a reason should refuse")
	}
	res, err := ResolveStockAgeInspection(f.tenant, "u", "HR/Admin", StoreAccess{All: true}, StockAgeOutcomeRequest{ID: id, Outcome: "Scrap", ReasonCode: "IA-DAMAGE", Lines: []struct {
		SKU string `json:"sku"`
		Qty int    `json:"qty"`
	}{{SKU: "I-PLAIN", Qty: 2}}})
	if err != nil || res.AdjustmentID == "" {
		t.Fatalf("scrap: %+v %v", res, err)
	}
	if oh, _, _ := f.stock(t, "I-PLAIN", "L1"); oh != 8 {
		t.Errorf("scrap: on hand %d, want 8", oh)
	}
	if _, err := ResolveStockAgeInspection(f.tenant, "u", "HR/Admin", StoreAccess{All: true}, StockAgeOutcomeRequest{ID: id, Outcome: "Pass"}); err == nil {
		t.Error("a decided inspection must not be decided twice")
	}
	if got := f.ageDocs(t, ageDone); got != 1 {
		t.Errorf("done = %d, want 1", got)
	}
	if _, err := ResolveStockAgeInspection(f.tenant, "u", "Cashier", StoreAccess{All: true}, StockAgeOutcomeRequest{ID: "x", Outcome: "Pass"}); err == nil {
		t.Error("a cashier may not decide")
	}
	if err := validateInspectionRule(map[string]interface{}{"rule_kind": "Stock age", "threshold": 0}); err == nil {
		t.Error("a rule without days or category should be refused")
	}
}

// The receipt-date barcode wins over the last receipt, and the row says which was used;
// a Markdown outcome sets the flag the due report shows.
func TestStage146311AgeSourceAndMarkdown(t *testing.T) {
	f := inspTenant(t)
	f.insert("L1", "Location", "Active", map[string]interface{}{"code": "L1", "name": "Store 1", "status": "Active"})
	f.insert("R-CAT", "InspectionRule", "Active", map[string]interface{}{"rule_kind": "Stock age", "ref": "Plain", "threshold": 30, "status": "Active"})
	f.receive(t, "G-A1", "V1", "L1", line("I-PLAIN", 10))
	f.receive(t, "G-A2", "V1", "L1", line("I-OPT", 5))
	// Ledger says both arrived 5 days ago (not due); I-PLAIN's dated barcode says 28 days ago.
	f.backdate(t, "I-PLAIN", 5)
	f.backdate(t, "I-OPT", 5)
	f.insert("BC-1", "ItemBarcode", "Active", map[string]interface{}{"kind": "Dated", "item_code": "I-PLAIN", "received_on": time.Now().AddDate(0, 0, -28).Format("2006-01-02"), "grns": []interface{}{"G-A1"}})
	if err := SetSetting(f.tenant, SettingAgeInspection, "true", "test"); err != nil {
		t.Fatal(err)
	}
	n, err := RunStockAgeInspections(f.tenant)
	if err != nil || n != 1 {
		t.Fatalf("filed %d (%v), want 1 from the barcode date", n, err)
	}
	list, err := ListStockAgeInspections(f.tenant, "HR/Admin", "", StoreAccess{All: true})
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v (%v)", list, err)
	}
	if list[0].AgeSource != ageSourceBarcode || list[0].AgeDays < 27 || list[0].AgeDays > 29 {
		t.Errorf("age source %q days %d, want %q and about 28", list[0].AgeSource, list[0].AgeDays, ageSourceBarcode)
	}
	// An item with no barcode falls back to its last receipt.
	f.backdate(t, "I-OPT", 40)
	f.insert("R-OPT", "InspectionRule", "Active", map[string]interface{}{"rule_kind": "Stock age (item)", "ref": "I-OPT", "threshold": 30, "status": "Active"})
	if n, _ = RunStockAgeInspections(f.tenant); n != 1 {
		t.Fatalf("ledger fallback filed %d, want 1", n)
	}
	list, _ = ListStockAgeInspections(f.tenant, "HR/Admin", "", StoreAccess{All: true})
	sources := map[string]bool{}
	for _, v := range list {
		sources[v.AgeSource] = true
	}
	if !sources[ageSourceBarcode] || !sources[ageSourceLedger] {
		t.Errorf("sources = %v, want both %q and %q", sources, ageSourceBarcode, ageSourceLedger)
	}
	// Markdown: the flag is set and R9061 shows it.
	var id string
	for _, v := range list {
		if v.AgeSource == ageSourceBarcode {
			id = v.ID
		}
	}
	if _, err := ResolveStockAgeInspection(f.tenant, "u", "HR/Admin", StoreAccess{All: true}, StockAgeOutcomeRequest{ID: id, Outcome: "Markdown"}); err != nil {
		t.Fatal(err)
	}
	rows, err := stockAgeDueReport(f.tenant, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, r := range rows {
		if fmt.Sprint(r["markdown"]) == "Yes" {
			seen = true
		}
	}
	if !seen {
		t.Errorf("R9061 shows no Markdown row: %v", rows)
	}
}
