package engines

import (
	"errors"
	"testing"

	"custom_erp/db"
)

// Stage 146.5.7: a dynamic product group may filter on attribute values with
// an operator that fits the attribute's type.

func s14657Setup(t *testing.T) string {
	schema, _, cleanup := pimTestFixture(t, "S14657")
	t.Cleanup(cleanup)
	for code, typ := range map[string]string{"S14657-COLOR": "Select", "S14657-WT": "Number", "S14657-LAUNCH": "Date", "S14657-NOTE": "Data"} {
		d := map[string]interface{}{"code": code, "label": code, "value_type": typ}
		if typ == "Select" {
			d["value_options"] = "Red,Blue,Green"
		}
		pimInsertDoc(t, schema, code, "ProductAttributeDef", "Active", d)
	}
	items := []struct{ id, color, wt, launch, note string }{
		{"S14657-I1", "Red", "10", "2026-01-10", "alpha one"},
		{"S14657-I2", "Blue", "25.5", "2026-03-01", "beta"},
		{"S14657-I3", "Green", "40", "2026-06-15", ""},
		{"S14657-I4", "", "", "", ""},
	}
	for _, it := range items {
		pimInsertDoc(t, schema, it.id, "Item", "Active", map[string]interface{}{"code": it.id, "name": "n " + it.id, "family": ""})
		for attr, v := range map[string]string{"S14657-COLOR": it.color, "S14657-WT": it.wt, "S14657-LAUNCH": it.launch, "S14657-NOTE": it.note} {
			if v != "" {
				pimInsertDoc(t, schema, it.id+"::"+attr, "ProductAttributeValue", "Active", map[string]interface{}{"item": it.id, "attribute": attr, "value": v})
			}
		}
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S14657%'")
	})
	return schema
}

func s14657Group(t *testing.T, schema, id string, conds []map[string]interface{}) {
	pimInsertDoc(t, schema, id, "PIMProductGroup", "Active", map[string]interface{}{
		"code": id, "name": id, "group_type": "Dynamic", "filter_conditions": conds})
}

func s14657Members(t *testing.T, group string) map[string]bool {
	t.Helper()
	res, err := ResolvePIMProductGroup("default", group)
	if err != nil {
		t.Fatalf("resolve %s: %v", group, err)
	}
	out := map[string]bool{}
	for _, m := range res.Members {
		if len(m.ItemCode) > 6 && m.ItemCode[:6] == "S14657" {
			out[m.ItemCode] = true
		}
	}
	return out
}

func TestStage14657ConditionsPickProductsByTypedOperator(t *testing.T) {
	schema := s14657Setup(t)
	cases := []struct {
		id    string
		conds []map[string]interface{}
		want  []string
	}{
		{"S14657-G1", []map[string]interface{}{{"attribute": "S14657-COLOR", "operator": "in", "value": "Red, Green"}}, []string{"S14657-I1", "S14657-I3"}},
		{"S14657-G2", []map[string]interface{}{{"attribute": "S14657-WT", "operator": "between", "value": "20..45"}}, []string{"S14657-I2", "S14657-I3"}},
		{"S14657-G3", []map[string]interface{}{{"attribute": "S14657-WT", "operator": "greater than", "value": "9"}, {"attribute": "S14657-COLOR", "operator": "not equals", "value": "Blue"}}, []string{"S14657-I1", "S14657-I3"}},
		{"S14657-G4", []map[string]interface{}{{"attribute": "S14657-LAUNCH", "operator": "after", "value": "2026-02-01"}}, []string{"S14657-I2", "S14657-I3"}},
		{"S14657-G5", []map[string]interface{}{{"attribute": "S14657-NOTE", "operator": "contains", "value": "ALPHA"}}, []string{"S14657-I1"}},
		{"S14657-G6", []map[string]interface{}{{"attribute": "S14657-COLOR", "operator": "is empty"}, {"attribute": "name", "operator": "starts with", "value": "n S14657"}}, []string{"S14657-I4"}},
		{"S14657-G7", []map[string]interface{}{{"attribute": "name", "operator": "equals", "value": "n S14657-I3"}}, []string{"S14657-I3"}},
	}
	for _, c := range cases {
		s14657Group(t, schema, c.id, c.conds)
		got := s14657Members(t, c.id)
		if len(got) != len(c.want) {
			t.Fatalf("%s: members %v, want %v", c.id, got, c.want)
		}
		for _, w := range c.want {
			if !got[w] {
				t.Fatalf("%s: members %v, want %v", c.id, got, c.want)
			}
		}
	}
}

func TestStage14657BadOperatorForTypeIsRefusedOnSave(t *testing.T) {
	s14657Setup(t)
	bad := func(attr, op, val string) string {
		err := ValidatePIMProductGroupDocument("default", map[string]interface{}{
			"group_type": "Dynamic", "filter_conditions": []map[string]interface{}{{"attribute": attr, "operator": op, "value": val}}})
		var ve *ValidationError
		if errors.As(err, &ve) {
			return ve.Code
		}
		if err != nil {
			return "other"
		}
		return ""
	}
	for _, c := range [][3]string{
		{"S14657-COLOR", "greater than", "1"},
		{"S14657-WT", "contains", "1"},
		{"S14657-LAUNCH", "starts with", "20"},
		{"S14657-WT", "greater than", "heavy"},
		{"S14657-WT", "between", "5"},
		{"S14657-LAUNCH", "after", "yesterday"},
		{"S14657-COLOR", "equals", "Purple"},
		{"S14657-NOTE", "equals", ""},
		{"S14657-NOTE", "wobbles", "x"},
		{"NO-SUCH-ATTR", "equals", "x"},
	} {
		if got := bad(c[0], c[1], c[2]); got != "PIM-0971" {
			t.Fatalf("%v: code %q, want PIM-0971", c, got)
		}
	}
	if got := bad("S14657-COLOR", "is empty", ""); got != "" {
		t.Fatalf("is empty needs no value, got %q", got)
	}
	if got := bad("S14657-WT", "between", "1..9"); got != "" {
		t.Fatalf("valid between refused: %q", got)
	}
}

func TestStage14657ConditionsAloneMakeADynamicGroupAndResultIsCapped(t *testing.T) {
	schema := s14657Setup(t)
	s14657Group(t, schema, "S14657-GCAP", []map[string]interface{}{{"attribute": "S14657-WT", "operator": "greater than", "value": "0"}})
	old := maxConditionGroupMembers
	maxConditionGroupMembers = 2
	defer func() { maxConditionGroupMembers = old }()
	res, err := ResolvePIMProductGroup("default", "S14657-GCAP")
	if err != nil {
		t.Fatal(err)
	}
	if res.MemberCount != 2 || !res.Truncated {
		t.Fatalf("count=%d truncated=%v, want 2 / true", res.MemberCount, res.Truncated)
	}
}
