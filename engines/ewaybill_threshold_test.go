package engines

import "testing"

// Rule 138(1) CGST Rules: an e-way bill is needed when the consignment value
// EXCEEDS the threshold, so exactly 50,000 needs none.
func TestEWBValueNeedsBillIsExceeding(t *testing.T) {
	cases := []struct {
		value float64
		want  bool
	}{{49999.99, false}, {50000, false}, {50000.01, true}, {50001, true}}
	for _, c := range cases {
		if got := ewbValueNeedsBill(c.value, 50000); got != c.want {
			t.Errorf("value %.2f: got %v want %v", c.value, got, c.want)
		}
	}
}
