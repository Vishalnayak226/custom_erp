package engines

import "testing"

// A Number field or number line column holds a number and nothing else:
// Sscanf("%f") used to stop at the first non-digit, so "12abc" passed.
func TestIsPlainNumber(t *testing.T) {
	for _, s := range []string{"0", "12", "-3", "12.50", " 7 ", "1e+06"} {
		if !isPlainNumber(s) {
			t.Errorf("%q: want a number", s)
		}
	}
	for _, s := range []string{"", "abc", "12abc", "1,000", "2O0", "NaN", "Inf", "--1"} {
		if isPlainNumber(s) {
			t.Errorf("%q: want refused", s)
		}
	}
}

func TestJSONTableNumberColumnRefusesText(t *testing.T) {
	f := FieldMeta{Label: "Items", Options: `[{"key":"sku","type":"link","required":true},{"key":"rate","label":"Purchase Price","type":"number","required":true},{"key":"mrp","label":"MRP","type":"number"}]`}
	ok := []string{
		`[{"sku":"A","rate":20,"mrp":100}]`,
		`[{"sku":"A","rate":"20"}]`,
		`[{"sku":"A","rate":20,"mrp":""}]`,
	}
	for _, v := range ok {
		if err := validateJSONTableValue(f, v); err != nil {
			t.Errorf("%s: want accepted, got %v", v, err)
		}
	}
	bad := []string{
		`[{"sku":"A","rate":"abc"}]`,
		`[{"sku":"A","rate":20,"mrp":"1x0"}]`,
	}
	for _, v := range bad {
		if err := validateJSONTableValue(f, v); err == nil {
			t.Errorf("%s: want refused", v)
		}
	}
}
