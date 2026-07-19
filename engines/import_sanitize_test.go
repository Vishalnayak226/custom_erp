package engines

import "testing"

func TestSanitizeCSVCell(t *testing.T) {
	for _, input := range []string{"=1+1", "+1+1", "-1", "@SUM(A1:A2)", "  =1+1"} {
		if got := sanitizeCSVCell(input); got != "'"+input {
			t.Errorf("sanitizeCSVCell(%q) = %q", input, got)
		}
	}
	for _, input := range []string{"Vendor Name", "'already escaped", "", "  normal"} {
		if got := sanitizeCSVCell(input); got != input {
			t.Errorf("safe value %q changed to %q", input, got)
		}
	}
}
