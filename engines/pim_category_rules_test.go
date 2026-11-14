package engines

import (
	"encoding/json"
	"strings"
	"testing"

	"custom_erp/db"
)

// 168.12 / 168.8: per-category attribute rules with nearest-wins inheritance.

func catRulesFixture(t *testing.T) *creditFixture {
	f := newCreditFixture(t)
	f.seed("CAT-ROOT", "Category", "Active", map[string]interface{}{"code": "CAT-ROOT", "name": "Jewellery"})
	f.seed("CAT-RING", "Category", "Active", map[string]interface{}{"code": "CAT-RING", "name": "Rings", "parent": "CAT-ROOT"})
	f.seed("CAT-BAND", "Category", "Active", map[string]interface{}{"code": "CAT-BAND", "name": "Bands", "parent": "CAT-RING"})
	for _, a := range []string{"METAL", "PURITY", "SIZE"} {
		f.seed(a, "ProductAttributeDef", "Active", map[string]interface{}{"code": a, "label": a + " label", "value_type": "Data"})
	}
	f.seed("R1", "CategoryAttributeRule", "Active", map[string]interface{}{"code": "R1", "category": "CAT-ROOT", "attribute": "METAL", "rule": "Required"})
	f.seed("R2", "CategoryAttributeRule", "Active", map[string]interface{}{"code": "R2", "category": "CAT-RING", "attribute": "SIZE", "rule": "Required"})
	f.seed("R3", "CategoryAttributeRule", "Active", map[string]interface{}{"code": "R3", "category": "CAT-BAND", "attribute": "METAL", "rule": "Optional"})
	f.seed("R4", "CategoryAttributeRule", "Active", map[string]interface{}{"code": "R4", "category": "CAT-BAND", "attribute": "PURITY", "rule": "Hidden"})
	return f
}

func ruleMap(t *testing.T, f *creditFixture, cat string) map[string]EffectiveCategoryRule {
	t.Helper()
	list, err := EffectiveCategoryRules(f.tenantID, cat)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]EffectiveCategoryRule{}
	for _, r := range list {
		m[r.Attribute] = r
	}
	return m
}

func countTenantDocs(t *testing.T, f *creditFixture, doctype string) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow("SELECT count(*) FROM "+f.schema+".documents WHERE doctype = $1 AND deleted_at IS NULL", doctype).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCategoryRulesNearestWinsAndInherited(t *testing.T) {
	f := catRulesFixture(t)
	band := ruleMap(t, f, "CAT-BAND")
	if band["METAL"].Rule != "Optional" || band["METAL"].Inherited {
		t.Fatalf("own Optional must beat parent Required: %+v", band["METAL"])
	}
	if r := band["SIZE"]; r.Rule != "Required" || !r.Inherited || r.From != "CAT-RING" {
		t.Fatalf("SIZE should come from Rings: %+v", r)
	}
	ring := ruleMap(t, f, "CAT-RING")
	if ring["METAL"].Rule != "Required" || !ring["METAL"].Inherited {
		t.Fatalf("ring METAL: %+v", ring["METAL"])
	}
	byName := ruleMap(t, f, "rings") // an item may hold the category's name
	if byName["SIZE"].Rule != "Required" || byName["SIZE"].Inherited {
		t.Fatalf("name match: %+v", byName["SIZE"])
	}
}

func TestCategoryRulesCycleDoesNotHang(t *testing.T) {
	f := catRulesFixture(t)
	f.seed("CAT-A", "Category", "Active", map[string]interface{}{"code": "CAT-A", "name": "A", "parent": "CAT-B"})
	f.seed("CAT-B", "Category", "Active", map[string]interface{}{"code": "CAT-B", "name": "B", "parent": "CAT-A"})
	f.seed("R9", "CategoryAttributeRule", "Active", map[string]interface{}{"code": "R9", "category": "CAT-B", "attribute": "SIZE", "rule": "Required"})
	if m := ruleMap(t, f, "CAT-A"); m["SIZE"].Rule != "Required" {
		t.Fatalf("cycle: %+v", m)
	}
}

func TestCategoryRulesCompletenessCountsRequiredAndHiddenDropsFamilyMandatory(t *testing.T) {
	f := catRulesFixture(t)
	f.seed("FAM1", "ProductFamily", "Active", map[string]interface{}{"code": "FAM1", "name": "Fam"})
	f.seed("FA-PUR", "ProductFamilyAttribute", "Active", map[string]interface{}{"family": "FAM1", "attribute": "PURITY", "mandatory": "Yes"})
	f.seed("IT-RING", "Item", "Active", map[string]interface{}{"code": "IT-RING", "name": "Ring", "category": "CAT-RING", "family": "FAM1"})
	f.seed("IT-BAND", "Item", "Active", map[string]interface{}{"code": "IT-BAND", "name": "Band", "category": "CAT-BAND", "family": "FAM1"})
	has := func(c *CompletenessResult, s string) bool {
		for _, m := range c.MissingFields {
			if strings.Contains(m, s) {
				return true
			}
		}
		return false
	}
	ring, err := CalculateCompleteness(f.tenantID, "IT-RING", "en", "")
	if err != nil {
		t.Fatal(err)
	}
	if !has(ring, "METAL") || !has(ring, "SIZE") || !has(ring, "PURITY") {
		t.Fatalf("ring should miss the inherited rules and the family attribute: %v", ring.MissingFields)
	}
	band, err := CalculateCompleteness(f.tenantID, "IT-BAND", "en", "")
	if err != nil {
		t.Fatal(err)
	}
	if has(band, "PURITY") || has(band, "METAL") || !has(band, "SIZE") {
		t.Fatalf("band: Hidden drops PURITY, Optional drops METAL, SIZE stays: %v", band.MissingFields)
	}
	list, err := ListWorkbenchSKUs(f.tenantID, "", []string{"IT-RING", "IT-BAND"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, e := range list {
		got[e.ItemCode] = e.MissingCount
	}
	if len(got) != 2 || got["IT-BAND"] >= got["IT-RING"] {
		t.Fatalf("workbook should score the band better: %v", got)
	}
}

func TestCategoryRulesSaveRefusalOnlyWithSetting(t *testing.T) {
	f := catRulesFixture(t)
	f.seed("IT-R1", "Item", "Active", map[string]interface{}{"code": "IT-R1", "name": "Ring", "category": "CAT-RING"})
	f.seed("IT-R1::METAL", "ProductAttributeValue", "Active", map[string]interface{}{"item": "IT-R1", "attribute": "METAL", "value": "Gold"})
	blank := map[string]interface{}{"item": "IT-R1", "attribute": "METAL", "value": ""}
	item := map[string]interface{}{"code": "IT-R1", "name": "Ring", "category": "CAT-RING"}

	if err := validateBlankedRequiredValue(f.tenantID, blank); err != nil {
		t.Fatalf("setting off must not refuse a blank: %v", err)
	}
	if err := ValidateItemCategoryRules(f.tenantID, item); err != nil {
		t.Fatalf("setting off must not refuse the item: %v", err)
	}
	if err := SetSetting(f.tenantID, SettingEnforceCategoryAttributeRules, "true", "test"); err != nil {
		t.Fatal(err)
	}
	err := validateBlankedRequiredValue(f.tenantID, blank)
	if ve, ok := err.(*ValidationError); !ok || ve.Code != "PIM-9300" {
		t.Fatalf("blank required: %v", err)
	}
	err = ValidateItemCategoryRules(f.tenantID, item) // SIZE is missing
	if ve, ok := err.(*ValidationError); !ok || ve.Code != "PIM-9301" || !strings.Contains(ve.Message, "SIZE") {
		t.Fatalf("existing item missing a required value: %v", err)
	}
	f.seed("IT-R1::SIZE", "ProductAttributeValue", "Active", map[string]interface{}{"item": "IT-R1", "attribute": "SIZE", "value": "7"})
	if err := ValidateItemCategoryRules(f.tenantID, item); err != nil {
		t.Fatalf("filled item: %v", err)
	}
	if err := ValidateItemCategoryRules(f.tenantID, map[string]interface{}{"code": "IT-NEW", "category": "CAT-RING"}); err != nil {
		t.Fatalf("a new item is never refused: %v", err)
	}
	if err := validateBlankedRequiredValue(f.tenantID, map[string]interface{}{"item": "IT-R1", "attribute": "PURITY", "value": ""}); err != nil {
		t.Fatalf("not required: %v", err)
	}
	if err := CheckAttributeValueDelete(f.tenantID, "IT-R1::METAL"); err == nil {
		t.Fatal("deleting a required value should be refused")
	}
}

func TestCategoryRulesHiddenKeepsStoredValue(t *testing.T) {
	f := catRulesFixture(t)
	f.seed("IT-B1", "Item", "Active", map[string]interface{}{"code": "IT-B1", "name": "Band", "category": "CAT-BAND"})
	f.seed("IT-B1::PURITY", "ProductAttributeValue", "Active", map[string]interface{}{"item": "IT-B1", "attribute": "PURITY", "value": "22k"})
	if r := ruleMap(t, f, "CAT-BAND")["PURITY"]; r.Rule != "Hidden" {
		t.Fatalf("PURITY should be Hidden: %+v", r)
	}
	if v := f.field("ProductAttributeValue", "IT-B1::PURITY", "value"); v != "22k" {
		t.Fatalf("hiding must keep the value, got %q", v)
	}
}

func TestCategoryRuleDocumentChecks(t *testing.T) {
	f := catRulesFixture(t)
	ok := map[string]interface{}{"id": "RX", "category": "CAT-RING", "attribute": "PURITY", "rule": "Required"}
	if err := ValidateCategoryAttributeRuleDocument(f.tenantID, ok); err != nil {
		t.Fatalf("valid: %v", err)
	}
	for code, p := range map[string]map[string]interface{}{
		"PIM-9302": {"id": "RX", "category": "CAT-RING", "attribute": "PURITY", "rule": "Maybe"},
		"PIM-9303": {"id": "RX", "category": "CAT-RING", "attribute": "PURITY", "rule": "Hidden", "default_value": "x"},
		"PIM-9304": {"id": "RX", "category": "CAT-RING", "attribute": "SIZE", "rule": "Optional"},
	} {
		err := ValidateCategoryAttributeRuleDocument(f.tenantID, p)
		if ve, isV := err.(*ValidationError); !isV || ve.Code != code {
			t.Fatalf("want %s got %v", code, err)
		}
	}
	if err := ValidateCategoryAttributeRuleDocument(f.tenantID, map[string]interface{}{"id": "R2", "category": "CAT-RING", "attribute": "SIZE", "rule": "Optional"}); err != nil {
		t.Fatalf("self edit: %v", err)
	}
}

func TestCategoryRulesLibraryImportPreviewApplyReapply(t *testing.T) {
	f := newCreditFixture(t)
	var lib categoryRuleLibrary
	if err := json.Unmarshal(categoryRuleLibraryJSON, &lib); err != nil || len(lib.Rules) == 0 {
		t.Fatalf("library: %v", err)
	}
	r0 := lib.Rules[0]
	f.seed(r0.CategoryID, "Category", "Active", map[string]interface{}{"code": r0.CategoryID, "name": r0.CategoryName})
	f.seed(r0.Attribute, "ProductAttributeDef", "Active", map[string]interface{}{"code": r0.Attribute, "label": r0.Attribute})

	pv, err := ImportCategoryRules(f.tenantID, "tester", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if pv.ToAdd == 0 || pv.Added != 0 || pv.NoCategory == 0 || pv.LeftOut == 0 {
		t.Fatalf("preview: %+v", pv)
	}
	if n := countTenantDocs(t, f, "CategoryAttributeRule"); n != 0 {
		t.Fatalf("preview wrote %d rules", n)
	}
	ap, err := ImportCategoryRules(f.tenantID, "tester", "", true)
	if err != nil || ap.Added != pv.ToAdd {
		t.Fatalf("apply: %+v %v", ap, err)
	}
	if n := countTenantDocs(t, f, "CategoryAttributeRule"); n != ap.Added {
		t.Fatalf("stored %d, added %d", n, ap.Added)
	}
	again, err := ImportCategoryRules(f.tenantID, "tester", "", true)
	if err != nil || again.Added != 0 || again.ToAdd != 0 || again.AlreadyThere != ap.Added {
		t.Fatalf("re-apply: %+v %v", again, err)
	}
}
