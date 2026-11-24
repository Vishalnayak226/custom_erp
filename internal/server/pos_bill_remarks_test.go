package server

import (
	"strings"
	"testing"
)

// Ask 260.3: a bill's remarks are printed on an ESC/POS receipt, where a
// control byte is a printer command. Checkout cleans them before anything
// stores or holds the sale.
func TestCleanBillRemarks(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"", "", true},
		{"  gift wrap  ", "gift wrap", true},
		{"gift\twrap\r\nplease", "gift wrap please", true},
		{"cut \x1b@ paper \x1dV now", "cut @ paper V now", true},
		{"del\x7fete c1\u0085x", "del ete c1 x", true},
		{"नमस्ते Sunday", "नमस्ते Sunday", true},
		{strings.Repeat("a", billRemarksMax), strings.Repeat("a", billRemarksMax), true},
		{strings.Repeat("é", billRemarksMax), strings.Repeat("é", billRemarksMax), true}, // runes, not bytes
	}
	for _, c := range cases {
		got, ok := cleanBillRemarks(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("cleanBillRemarks(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
	if _, ok := cleanBillRemarks(strings.Repeat("a", billRemarksMax+1)); ok {
		t.Errorf("a %d-character remark was accepted; the most is %d", billRemarksMax+1, billRemarksMax)
	}
	// Spaces squeezed out do not count against the limit.
	if got, ok := cleanBillRemarks(strings.Repeat("ab   ", 60)); !ok || len(got) != 60*3-1 {
		t.Errorf("squeezed remark = %d chars, ok %v; want %d, true", len(got), ok, 60*3-1)
	}
}
