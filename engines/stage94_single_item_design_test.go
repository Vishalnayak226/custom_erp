package engines

import "testing"

// TestStage94SingleItemDesignNumbered: a design that is one item (every
// attribute has one value) has nothing to build a Combination ID from, so
// PrepareMasterNumber must not skip it - it takes the ordinary Item number.
func TestStage94SingleItemDesignNumbered(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]interface{}
		want    bool
	}{
		{"single item, no values", map[string]interface{}{"family": "D00001"}, false},
		{"empty option string", map[string]interface{}{"family": "D00001", "variant_option_values": " ; "}, false},
		{"blank attribute field", map[string]interface{}{"family": "D00001", "color": "  "}, false},
		{"typed option values", map[string]interface{}{"family": "D00001", "variant_option_values": "color:Red;size:M"}, true},
		{"attribute field", map[string]interface{}{"family": "D00001", "polish": "Temple"}, true},
	}
	for _, c := range cases {
		if got := itemHasVariantValues(c.payload); got != c.want {
			t.Errorf("%s: itemHasVariantValues = %v, want %v", c.name, got, c.want)
		}
	}
}
