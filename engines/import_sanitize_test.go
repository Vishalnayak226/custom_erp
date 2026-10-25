package engines

import "testing"

func TestSanitizeCSVCell(t *testing.T) {
	for _, input := range []string{"=1+1", "+1+1", "-1+2", "@SUM(A1:A2)", "  =1+1"} {
		if got := sanitizeCSVCell(input); got != "'"+input {
			t.Errorf("sanitizeCSVCell(%q) = %q", input, got)
		}
	}
	// Stage 49.8.2: a plain number - a negative quantity included - is never a
	// formula and stays a number (it used to import as the text "'-1").
	for _, input := range []string{"Vendor Name", "'already escaped", "", "  normal", "-1", "-12.50", "+3"} {
		if got := sanitizeCSVCell(input); got != input {
			t.Errorf("safe value %q changed to %q", input, got)
		}
	}
}
