package engines

import (
	"errors"
	"testing"
)

// Stage 146.5.5: a design (the parent Item) carries attribute values its
// variants inherit; a grid shows them and a bulk set writes many at once.

const s14655Setting = "pim.design_values_fallback"

type s14655Fixture struct {
	schema, prefix      string
	designA, designB    string
	varA1, varA2, varB1 string
	cleanup             func()
}

func s14655Setup(t *testing.T) *s14655Fixture {
	t.Helper()
	schema, _, cleanup := pimTestFixture(t, "S14655")
	f := &s14655Fixture{schema: schema, prefix: "S14655"}
	f.designA, f.designB = "S14655-DA", "S14655-DB"
	f.varA1, f.varA2, f.varB1 = "S14655-A1", "S14655-A2", "S14655-B1"
	for _, d := range []string{f.designA, f.designB} {
		pimInsertDoc(t, schema, d, "Item", "Active", map[string]interface{}{"code": d, "name": "Design " + d})
	}
	for v, p := range map[string]string{f.varA1: f.designA, f.varA2: f.designA, f.varB1: f.designB} {
		pimInsertDoc(t, schema, v, "Item", "Active", map[string]interface{}{"code": v, "name": "Var " + v, "parent_product_code": p})
	}
	pimInsertDoc(t, schema, "S14655-ATTR", "ProductAttributeDef", "Active", map[string]interface{}{"code": "S14655-ATTR", "label": "Material"})
	pimInsertDoc(t, schema, "S14655-GRP", "PIMProductGroup", "Active", map[string]interface{}{
		"code": "S14655-GRP", "name": "g", "group_type": "Static",
		"members": []map[string]interface{}{{"item_code": f.varA1}, {"item_code": f.varA2}, {"item_code": f.varB1}}})
	f.cleanup = func() {
		SetSetting("default", s14655Setting, "false", "test")
		cleanup()
	}
	t.Cleanup(f.cleanup)
	return f
}

func TestStage14655FallbackIsOffByDefault(t *testing.T) {
	f := s14655Setup(t)
	if _, err := SetDesignAttributeValues("default", "system", DesignValueRequest{Designs: []string{f.designA}, Attribute: "S14655-ATTR", Value: "Gold"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, _ := ResolveAttributeValue("default", f.varA1, "S14655-ATTR", "", ""); v != "" {
		t.Fatalf("setting off: variant resolved %q, want blank (as today)", v)
	}
	if v, _ := ResolveAttributeValue("default", f.designA, "S14655-ATTR", "", ""); v != "Gold" {
		t.Fatalf("design itself = %q, want Gold", v)
	}
}

func TestStage14655VariantInheritsDesignValue(t *testing.T) {
	f := s14655Setup(t)
	if err := SetSetting("default", s14655Setting, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetDesignAttributeValues("default", "system", DesignValueRequest{Designs: []string{f.designA}, Attribute: "S14655-ATTR", Value: "Gold"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, _ := ResolveAttributeValue("default", f.varA1, "S14655-ATTR", "", ""); v != "Gold" {
		t.Fatalf("variant = %q, want Gold", v)
	}
	if v, _ := ResolveAttributeValue("default", f.varB1, "S14655-ATTR", "", ""); v != "" {
		t.Fatalf("variant of another design = %q, want blank", v)
	}
	// the variant's own value wins
	pimInsertDoc(t, f.schema, "S14655-A2::S14655-ATTR", "ProductAttributeValue", "Active",
		map[string]interface{}{"item": f.varA2, "attribute": "S14655-ATTR", "value": "Silver"})
	if v, _ := ResolveAttributeValue("default", f.varA2, "S14655-ATTR", "", ""); v != "Silver" {
		t.Fatalf("own value = %q, want Silver", v)
	}
	// a blank own value falls through to the design
	pimInsertDoc(t, f.schema, "S14655-A1::S14655-ATTR", "ProductAttributeValue", "Active",
		map[string]interface{}{"item": f.varA1, "attribute": "S14655-ATTR", "value": ""})
	if v, _ := ResolveAttributeValue("default", f.varA1, "S14655-ATTR", "", ""); v != "Gold" {
		t.Fatalf("blank own = %q, want Gold", v)
	}
	if ok, _ := attributeValueFilled("default", f.varA1, "S14655-ATTR", "", ""); !ok {
		t.Fatal("completeness should count the inherited value")
	}
}

func TestStage14655BulkSetOverAGroupAndClear(t *testing.T) {
	f := s14655Setup(t)
	res, err := SetDesignAttributeValues("default", "system", DesignValueRequest{Group: "S14655-GRP", Attribute: "S14655-ATTR", Value: "Brass"})
	if err != nil {
		t.Fatalf("bulk: %v", err)
	}
	if res.Designs != 2 || res.Written != 2 {
		t.Fatalf("designs/written = %d/%d, want 2/2 (three variants, two designs)", res.Designs, res.Written)
	}
	again, err := SetDesignAttributeValues("default", "system", DesignValueRequest{Group: "S14655-GRP", Attribute: "S14655-ATTR", Value: "Brass"})
	if err != nil || again.Written != 0 || again.Unchanged != 2 {
		t.Fatalf("repeat = %+v, %v; want nothing rewritten", again, err)
	}
	grid, err := DesignValueGrid("default", "S14655-GRP", []string{"S14655-ATTR"})
	if err != nil || len(grid) != 2 {
		t.Fatalf("grid = %d rows, %v; want 2", len(grid), err)
	}
	for _, r := range grid {
		if r.Values["S14655-ATTR"] != "Brass" || r.Variants < 1 {
			t.Fatalf("grid row %+v", r)
		}
	}
	cleared, err := SetDesignAttributeValues("default", "system", DesignValueRequest{Group: "S14655-GRP", Attribute: "S14655-ATTR", Value: ""})
	if err != nil || cleared.Written != 2 {
		t.Fatalf("clear = %+v, %v", cleared, err)
	}
	if v, _ := ResolveAttributeValue("default", f.designA, "S14655-ATTR", "", ""); v != "" {
		t.Fatalf("after clear = %q", v)
	}
}

func TestStage14655RefusesUnknownAttributeDesignAndTooMany(t *testing.T) {
	f := s14655Setup(t)
	code := func(err error) string {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return ve.Code
		}
		return ""
	}
	if _, err := SetDesignAttributeValues("default", "system", DesignValueRequest{Designs: []string{f.designA}, Attribute: "NOPE", Value: "x"}); code(err) != "PIM-0960" {
		t.Fatalf("unknown attribute: %v", err)
	}
	if _, err := SetDesignAttributeValues("default", "system", DesignValueRequest{Designs: []string{"NOPE-DESIGN"}, Attribute: "S14655-ATTR", Value: "x"}); code(err) != "PIM-0961" {
		t.Fatalf("unknown design: %v", err)
	}
	if _, err := SetDesignAttributeValues("default", "system", DesignValueRequest{Attribute: "S14655-ATTR", Value: "x"}); code(err) != "PIM-0959" {
		t.Fatalf("nothing chosen: %v", err)
	}
	if err := SetSetting("default", "pim.max_bulk_edit_documents", "1", "test"); err != nil {
		t.Fatal(err)
	}
	defer SetSetting("default", "pim.max_bulk_edit_documents", "100", "test")
	if _, err := SetDesignAttributeValues("default", "system", DesignValueRequest{Designs: []string{f.designA, f.designB}, Attribute: "S14655-ATTR", Value: "x"}); code(err) != "PIM-0962" {
		t.Fatalf("over the cap: %v", err)
	}
	// nothing was half-written
	if v, _ := ResolveAttributeValue("default", f.designA, "S14655-ATTR", "", ""); v != "" {
		t.Fatalf("refused call wrote %q", v)
	}
}
