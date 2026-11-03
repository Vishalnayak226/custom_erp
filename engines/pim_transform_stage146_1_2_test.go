package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 146.1.2: a transform rule must run on a blank value, so
// default_if_empty fills the gap on an export, while the other transforms
// keep producing a blank (never junk) for blank input.
func TestStage14612RulesRunOnBlankValues(t *testing.T) {
	schema, itemID, cleanup := pimTestFixture(t, "S14612")
	defer cleanup()

	pimInsertDoc(t, schema, "S14612-CH", "Channel", "Active", map[string]interface{}{"code": "S14612-CH", "name": "S14612", "default_locale": "en"})
	rules := map[string][]map[string]interface{}{
		"S14612-DEF":   {{"sequence": 1, "function": "default_if_empty", "operand1": "NA"}},
		"S14612-PRE":   {{"sequence": 1, "function": "prefix", "operand1": "X-"}},
		"S14612-CHAIN": {{"sequence": 1, "function": "default_if_empty", "operand1": "red"}, {"sequence": 2, "function": "uppercase"}},
	}
	for id, steps := range rules {
		pimInsertDoc(t, schema, id, "PIMTransformRule", "Active", map[string]interface{}{"code": id, "name": id, "steps": steps})
	}
	maps := []struct{ id, source, target, rule string }{
		{"S14612-M1", "colour_x", "color_a", "S14612-DEF"},
		{"S14612-M2", "colour_y", "color_b", "S14612-PRE"},
		{"S14612-M3", "colour_z", "color_c", "S14612-CHAIN"},
	}
	for _, m := range maps {
		pimInsertDoc(t, schema, m.id, "ChannelFieldMap", "Active", map[string]interface{}{
			"channel": "S14612-CH", "source_field": m.source, "target_field": m.target, "transform_rule": m.rule})
	}
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S14612%'")

	payload, err := BuildChannelPayload("default", itemID, "S14612-CH")
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	if got := payload.Attributes["color_a"]; got != "NA" {
		t.Errorf("default_if_empty on a blank attribute = %q, want NA", got)
	}
	if got, ok := payload.Attributes["color_b"]; ok {
		t.Errorf("prefix on a blank attribute produced junk %q, want it left out", got)
	}
	if got := payload.Attributes["color_c"]; got != "RED" {
		t.Errorf("default then uppercase = %q, want RED", got)
	}
}

// Every existing transform's behaviour on blank input, by the one seam both
// the export and import paths use.
func TestStage14612EveryTransformOnBlank(t *testing.T) {
	cases := []struct {
		fn, op1, op2, want string
	}{
		{"trim", "", "", ""},
		{"uppercase", "", "", ""},
		{"lowercase", "", "", ""},
		{"prefix", "X-", "", ""},
		{"suffix", "-X", "", ""},
		{"truncate", "3", "", ""},
		{"default_if_empty", "NA", "", "NA"},
		{"find_replace_literal", "a", "b", ""},
		{"number_format", "2", "", ""},
		{"date_format", "2006-01-02", "02/01/2006", ""},
	}
	for _, tc := range cases {
		got, err := runPIMTransformSteps("R", []PIMTransformStep{{Sequence: 1, Function: tc.fn, Operand1: tc.op1, Operand2: tc.op2}}, "  ", PIMTransformContext{})
		if err != nil {
			t.Errorf("%s on blank: %v", tc.fn, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s on blank = %q, want %q", tc.fn, got, tc.want)
		}
	}
}
