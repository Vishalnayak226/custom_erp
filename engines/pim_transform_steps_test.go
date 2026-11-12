package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 146.5.3: the new transform steps, each on its own, plus value maps kept
// per channel.
func TestStage14653TransformSteps(t *testing.T) {
	lookup := func(attr string) string {
		return map[string]string{"colour": "Red", "size": " M ", "empty": "", "model": "Shirt"}[attr]
	}
	tc := PIMTransformContext{Lookup: lookup}
	cases := []struct {
		name, value string
		step        PIMTransformStep
		want        string
		wantErr     bool
	}{
		{"constant", "anything", PIMTransformStep{Function: "constant", Operand1: "FIXED"}, "FIXED", false},
		{"constant fills blank", "", PIMTransformStep{Function: "constant", Operand1: "FIXED"}, "FIXED", false},
		{"join default space", "Blue", PIMTransformStep{Function: "join", Operand1: "colour,size"}, "Blue Red M", false},
		{"join separator skips blank", "", PIMTransformStep{Function: "join", Operand1: "empty,model,colour", Operand2: " - "}, "Shirt - Red", false},
		{"first_non_empty keeps filled", "Own", PIMTransformStep{Function: "first_non_empty", Operand1: "colour"}, "Own", false},
		{"first_non_empty falls to list", "", PIMTransformStep{Function: "first_non_empty", Operand1: "empty,model,colour"}, "Shirt", false},
		{"first_non_empty all blank", "", PIMTransformStep{Function: "first_non_empty", Operand1: "empty,nothing"}, "", false},
		{"pad left default", "7", PIMTransformStep{Function: "pad", Operand1: "3"}, "007", false},
		{"pad char", "7", PIMTransformStep{Function: "pad", Operand1: "4", Operand2: "x"}, "xxx7", false},
		{"pad right", "7", PIMTransformStep{Function: "pad", Operand1: "4", Operand2: "x:right"}, "7xxx", false},
		{"pad already wide", "12345", PIMTransformStep{Function: "pad", Operand1: "3"}, "12345", false},
		{"pad bad width", "7", PIMTransformStep{Function: "pad", Operand1: "9999"}, "", true},
		{"pad multi-char", "7", PIMTransformStep{Function: "pad", Operand1: "4", Operand2: "ab"}, "", true},
		{"regex_replace", "SKU-123-A", PIMTransformStep{Function: "regex_replace", Operand1: `[^0-9]+`, Operand2: ""}, "123", false},
		{"regex_replace group", "ab12", PIMTransformStep{Function: "regex_replace", Operand1: `([a-z]+)(\d+)`, Operand2: "${2}-${1}"}, "12-ab", false},
		{"regex_replace bad pattern", "x", PIMTransformStep{Function: "regex_replace", Operand1: `(`}, "", true},
		{"regex_replace too long", "x", PIMTransformStep{Function: "regex_replace", Operand1: strings.Repeat("a", 201)}, "", true},
		{"regex_extract match", "Order #4521 shipped", PIMTransformStep{Function: "regex_extract", Operand1: `\d+`}, "4521", false},
		{"regex_extract group", "Order #4521 shipped", PIMTransformStep{Function: "regex_extract", Operand1: `#(\d+)`}, "4521", false},
		{"regex_extract none", "no digits", PIMTransformStep{Function: "regex_extract", Operand1: `\d+`}, "", false},
		{"strip_html", "<p>Soft &amp; <b>warm</b></p>\n<p>fleece</p>", PIMTransformStep{Function: "strip_html"}, "Soft & warm fleece", false},
		{"split part", "red/blue/green", PIMTransformStep{Function: "split", Operand1: "/", Operand2: "2"}, "blue", false},
		{"split part out of range", "red/blue", PIMTransformStep{Function: "split", Operand1: "/", Operand2: "5"}, "", false},
		{"split bad part", "red/blue", PIMTransformStep{Function: "split", Operand1: "/", Operand2: "0"}, "", true},
	}
	for _, c := range cases {
		c.step.Sequence = 1
		got, err := runPIMTransformSteps("R", []PIMTransformStep{c.step}, c.value, tc)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected an error, got %q", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
		} else if got != c.want {
			t.Errorf("%s = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestStage14653RuleValidationCatchesBadSteps(t *testing.T) {
	bad := []map[string]interface{}{
		{"sequence": 1, "function": "regex_replace", "operand1": "("},
		{"sequence": 1, "function": "pad", "operand1": "abc"},
		{"sequence": 1, "function": "split", "operand1": "/", "operand2": "x"},
		{"sequence": 1, "function": "join"},
		{"sequence": 1, "function": "value_map"},
	}
	for _, step := range bad {
		err := ValidatePIMTransformRuleDocument("default", map[string]interface{}{"steps": []map[string]interface{}{step}})
		if err == nil {
			t.Errorf("step %v was accepted", step)
		}
	}
	ok := map[string]interface{}{"steps": []map[string]interface{}{
		{"sequence": 1, "function": "strip_html"},
		{"sequence": 2, "function": "value_map", "operand1": "colour"},
	}}
	if err := ValidatePIMTransformRuleDocument("default", ok); err != nil {
		t.Errorf("valid rule refused: %v", err)
	}
}

// Value maps are per channel: Myntra and Amazon name the same colour
// differently, and the same rule serves both.
func TestStage14653ValueMapsPerChannel(t *testing.T) {
	schema, itemID, cleanup := pimTestFixture(t, "S14653")
	defer cleanup()
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S14653%'")

	channels := []string{"S14653-MYN", "S14653-AMZ", "S14653-OTH"}
	for _, ch := range channels {
		pimInsertDoc(t, schema, ch, "Channel", "Active", map[string]interface{}{"code": ch, "name": ch, "default_locale": "en"})
	}
	vm := func(id, channel, ours, theirs, status string) {
		pimInsertDoc(t, schema, id, "PIMValueMap", status, map[string]interface{}{
			"channel": channel, "attribute": "colour", "our_value": ours, "their_value": theirs})
	}
	vm("S14653-V1", "S14653-MYN", "Navy", "Dark Blue", "Active")
	vm("S14653-V2", "S14653-AMZ", "Navy", "Midnight", "Active")
	vm("S14653-V3", "S14653-OTH", "Navy", "Ignored", "Inactive")
	pimInsertDoc(t, schema, "S14653-RULE", "PIMTransformRule", "Active", map[string]interface{}{
		"code": "S14653-RULE", "name": "map colour",
		"steps": []map[string]interface{}{{"sequence": 1, "function": "value_map", "operand1": "colour"}}})
	for i, ch := range channels {
		pimInsertDoc(t, schema, "S14653-M"+string(rune('1'+i)), "ChannelFieldMap", "Active", map[string]interface{}{
			"channel": ch, "source_field": "colour_src", "target_field": "color", "transform_rule": "S14653-RULE"})
	}
	// the product's own value comes from the item document
	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET data = data || '{"colour_src":"navy"}'::jsonb WHERE id = $1`, itemID); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"S14653-MYN": "Dark Blue", "S14653-AMZ": "Midnight", "S14653-OTH": "navy"}
	for ch, w := range want {
		p, err := BuildChannelPayload("default", itemID, ch)
		if err != nil {
			t.Fatalf("%s: %v", ch, err)
		}
		if got := p.Attributes["color"]; got != w {
			t.Errorf("%s color = %q, want %q (inactive map and unmapped values pass through)", ch, got, w)
		}
	}
	// an explicit channel in operand 2 wins over the exporting channel
	step := func(op2 string) []PIMTransformStep {
		return []PIMTransformStep{{Sequence: 1, Function: "value_map", Operand1: "colour", Operand2: op2}}
	}
	got, err := runPIMTransformSteps("R", step("S14653-AMZ"), "Navy", PIMTransformContext{TenantID: "default", Channel: "S14653-MYN"})
	if err != nil || got != "Midnight" {
		t.Errorf("explicit channel = %q, %v; want Midnight", got, err)
	}
	// a value with no map row passes through
	if got, _ := runPIMTransformSteps("R", step(""), "Teal", PIMTransformContext{TenantID: "default", Channel: "S14653-MYN"}); got != "Teal" {
		t.Errorf("unmapped value = %q, want it passed through", got)
	}
	// no channel known (an import) and none in operand 2: refused, not silent
	if _, err := runPIMTransformSteps("R", step(""), "Navy", PIMTransformContext{TenantID: "default"}); err == nil {
		t.Error("value_map without a channel should be refused")
	}
}
