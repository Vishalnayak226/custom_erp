package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 146.3.7: put-away rules by item, design or category. The most
// specific rule wins (item > design > category > location default) and the
// suggestion uses the rule's bin or zone. No rules = exactly today.

func prSeed(t *testing.T, insert func(id, doctype, status, createdBy string, data map[string]interface{})) (loc, sku string) {
	t.Helper()
	loc, sku = "PR1", "PR1-SKU"
	insert("ITEM-"+sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Q", "category": "Garments", "family": "FAM-PR1"})
	insert("ITEM-PR1-OTHER", "Item", "Active", "system", map[string]interface{}{"code": "PR1-OTHER", "name": "O", "category": "Toys"})
	for _, b := range []string{"PR-ITM", "PR-DES", "PR-CAT", "PR-LOC", "PR-FULL"} {
		m := map[string]interface{}{"bin_code": b, "location": loc, "status": "Active"}
		if b == "PR-FULL" {
			m["capacity"] = 1
		}
		insert("BIN-"+b, "Bin", "Active", "system", m)
	}
	insert("ZONE-PRZ", "Zone", "Active", "system", map[string]interface{}{"code": "PRZ"})
	insert("BIN-PR-Z1", "Bin", "Active", "system", map[string]interface{}{"bin_code": "PR-Z1", "location": loc, "zone": "PRZ", "status": "Active"})
	return
}

func prRule(insert func(id, doctype, status, createdBy string, data map[string]interface{}), id string, m map[string]interface{}) {
	m["status"] = "Active"
	insert(id, "PutawayRule", "Active", "system", m)
}

func TestStage14637MostSpecificRuleWins(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	loc, sku := prSeed(t, insert)

	// No rules: exactly today (no home bin, no strategy -> no suggestion).
	if bin, _, err := SuggestPutawayBin(tenantID, sku, loc, 2, ""); err != nil || bin != "" {
		t.Fatalf("no rules: want no suggestion, got %q %v", bin, err)
	}

	prRule(insert, "PR-R-LOC", map[string]interface{}{"location_code": loc, "bin_code": "PR-LOC"})
	prRule(insert, "PR-R-CAT", map[string]interface{}{"category": "Garments", "bin_code": "PR-CAT"})
	prRule(insert, "PR-R-DES", map[string]interface{}{"design": "FAM-PR1", "bin_code": "PR-DES"})
	prRule(insert, "PR-R-ITM", map[string]interface{}{"item": sku, "bin_code": "PR-ITM"})

	steps := []struct{ drop, want string }{
		{"", "PR-ITM"},
		{"PR-R-ITM", "PR-DES"},
		{"PR-R-DES", "PR-CAT"},
		{"PR-R-CAT", "PR-LOC"},
	}
	for _, s := range steps {
		if s.drop != "" {
			if _, err := db.DB.Exec("UPDATE "+schema+".documents SET status = 'Inactive', data = data || '{\"status\":\"Inactive\"}' WHERE id = $1", s.drop); err != nil {
				t.Fatal(err)
			}
		}
		bin, reason, err := SuggestPutawayBin(tenantID, sku, loc, 2, "")
		if err != nil || bin != s.want || !strings.Contains(reason, "put-away rule") {
			t.Fatalf("after dropping %q: got %q (%s) %v; want %s via a put-away rule", s.drop, bin, reason, err, s.want)
		}
	}
	// An item of another category falls to the location default only.
	if bin, _, _ := SuggestPutawayBin(tenantID, "PR1-OTHER", loc, 2, ""); bin != "PR-LOC" {
		t.Fatalf("other category: got %q; want the location default PR-LOC", bin)
	}
}

func TestStage14637RuleZoneAndFallThrough(t *testing.T) {
	tenantID, _, insert := stage140Tenant(t)
	loc, sku := prSeed(t, insert)
	// The item rule points at a bin with no room; the category rule's zone is next.
	if _, err := db.DB.Exec("INSERT INTO "+insert2schema(t, tenantID)+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ('PR-FULL', 'PR1-OTHER', $1, 'Good', 1)", loc); err != nil {
		t.Fatal(err)
	}
	prRule(insert, "PR-R-ITM", map[string]interface{}{"item": sku, "bin_code": "PR-FULL"})
	prRule(insert, "PR-R-CAT", map[string]interface{}{"category": "Garments", "zone": "PRZ"})
	bin, reason, err := SuggestPutawayBin(tenantID, sku, loc, 2, "")
	if err != nil || bin != "PR-Z1" || !strings.Contains(reason, "PRZ") {
		t.Fatalf("got %q (%s) %v; want PR-Z1 from the category rule's zone PRZ", bin, reason, err)
	}
}

func TestStage14637RuleValidation(t *testing.T) {
	two := map[string]interface{}{"item": "A", "category": "C", "bin_code": "B1", "status": "Active"}
	if err := validatePutawayRuleMasterRules("t", "", two); err == nil {
		t.Fatal("a rule naming both an item and a category was accepted")
	}
	none := map[string]interface{}{"item": "A", "status": "Active"}
	if err := validatePutawayRuleMasterRules("t", "", none); err == nil {
		t.Fatal("a rule naming neither a zone nor a bin was accepted")
	}
}

func insert2schema(t *testing.T, tenantID string) string {
	t.Helper()
	s, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
