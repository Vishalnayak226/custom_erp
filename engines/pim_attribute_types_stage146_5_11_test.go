package engines

import "testing"

// Stage 146.5.11: attribute types (Yes / No, Multi-select, Long text, URL) and a
// check of every value against its definition at ValidateDocument.

func s146511Setup(t *testing.T) (schema string) {
	t.Helper()
	schema, _, cleanup := pimTestFixture(t, "S146511")
	t.Cleanup(func() {
		SetSetting("default", SettingCheckLegacyAttributeValues, "false", "test")
		cleanup()
	})
	defs := map[string]map[string]interface{}{
		"S146511-YN":  {"value_type": "Yes / No"},
		"S146511-MS":  {"value_type": "Multi-select", "value_options": "Red,Green,Blue"},
		"S146511-LT":  {"value_type": "Long text"},
		"S146511-URL": {"value_type": "URL"},
		"S146511-NUM": {"value_type": "Number"},
		"S146511-SEL": {"value_type": "Select", "value_options": "A,B"},
	}
	for id, d := range defs {
		d["code"], d["label"], d["status"] = id, id, "Active"
		pimInsertDoc(t, schema, id, "ProductAttributeDef", "Active", d)
	}
	return schema
}

func s146511Val(attr, v string) map[string]interface{} {
	id := "S146511-ITEM::" + attr
	return map[string]interface{}{"id": id, "code": id, "item": "S146511-ITEM", "attribute": attr, "value": v, "status": "Active"}
}

func TestStage146511NewTypesAreCheckedAtValidateDocument(t *testing.T) {
	s146511Setup(t)
	bad := map[string]string{
		"S146511-YN": "maybe", "S146511-MS": "Red,Pink", "S146511-URL": "not a url",
	}
	for attr, v := range bad {
		err := ValidateDocument("default", "ProductAttributeValue", s146511Val(attr, v))
		ve, ok := err.(*ValidationError)
		if !ok || ve.Code != "PIM-0980" {
			t.Errorf("%s=%q: want PIM-0980, got %v", attr, v, err)
		}
	}
	p := s146511Val("S146511-YN", "y")
	if err := ValidateDocument("default", "ProductAttributeValue", p); err != nil || p["value"] != "Yes" {
		t.Errorf("yes/no: err=%v value=%v", err, p["value"])
	}
	p = s146511Val("S146511-MS", "blue, red")
	if err := ValidateDocument("default", "ProductAttributeValue", p); err != nil || p["value"] != "Blue,Red" {
		t.Errorf("multi: err=%v value=%v", err, p["value"])
	}
	for _, c := range [][2]string{{"S146511-URL", "https://example.com/a?b=1"}, {"S146511-LT", "line one\nline two"}} {
		if err := ValidateDocument("default", "ProductAttributeValue", s146511Val(c[0], c[1])); err != nil {
			t.Errorf("%s: %v", c[0], err)
		}
	}
}

func TestStage146511OldTypesKeepSavingAsToday(t *testing.T) {
	schema := s146511Setup(t)
	for _, c := range [][2]string{{"S146511-NUM", "abc"}, {"S146511-SEL", "zzz"}} {
		if err := ValidateDocument("default", "ProductAttributeValue", s146511Val(c[0], c[1])); err != nil {
			t.Errorf("setting off, %s=%q refused: %v", c[0], c[1], err)
		}
	}
	if err := SetSetting("default", SettingCheckLegacyAttributeValues, "true", "test"); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{{"S146511-NUM", "abc"}, {"S146511-SEL", "zzz"}} {
		if err := ValidateDocument("default", "ProductAttributeValue", s146511Val(c[0], c[1])); err == nil {
			t.Errorf("setting on, %s=%q passed", c[0], c[1])
		}
	}
	// a stored value that would now fail still saves unchanged
	pimInsertDoc(t, schema, "S146511-ITEM::S146511-SEL::fr::", "ProductAttributeValue", "Active",
		map[string]interface{}{"item": "S146511-ITEM", "attribute": "S146511-SEL", "value": "old", "locale": "fr"})
	p := s146511Val("S146511-SEL", "old")
	p["id"], p["locale"] = "S146511-ITEM::S146511-SEL::fr::", "fr"
	if err := ValidateDocument("default", "ProductAttributeValue", p); err != nil {
		t.Errorf("unchanged stored value blocked: %v", err)
	}
	p["value"] = "other"
	if err := ValidateDocument("default", "ProductAttributeValue", p); err == nil {
		t.Errorf("changed value passed")
	}
}

func TestStage146511DefinitionAcceptsNewTypesAndBulkSetIsChecked(t *testing.T) {
	s146511Setup(t)
	d := map[string]interface{}{"code": "S146511-NEW", "label": "n", "value_type": "Yes / No", "status": "Active"}
	if err := ValidateDocument("default", "ProductAttributeDef", d); err != nil {
		t.Fatalf("definition with new type refused: %v", err)
	}
	_, err := SetDesignAttributeValues("default", "system", DesignValueRequest{Designs: []string{"S146511-ITEM"}, Attribute: "S146511-YN", Value: "perhaps"})
	if ve, ok := err.(*ValidationError); !ok || ve.Code != "PIM-0980" {
		t.Fatalf("bulk set: want PIM-0980, got %v", err)
	}
}
