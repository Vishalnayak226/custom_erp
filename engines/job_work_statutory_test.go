package engines

import "testing"

// CGST Act s.143: inputs return within 1 year, capital goods within 3; when
// goods go direct to the job worker the clock runs from the job worker's receipt.
func TestJobWorkStatutoryDue(t *testing.T) {
	cases := []struct {
		name, sent, recv string
		capital          bool
		want             string
	}{
		{"no receipt date counts from sending", "2026-01-10", "", false, "2027-01-10"},
		{"receipt date moves the due date", "2026-01-10", "2026-02-20", false, "2027-02-20"},
		{"capital goods get three years", "2026-01-10", "", true, "2029-01-10"},
		{"capital goods from receipt", "2026-01-10", "2026-02-20", true, "2029-02-20"},
		{"a bad receipt date falls back to sending", "2026-01-10", "soon", false, "2027-01-10"},
		{"nothing known gives nothing", "", "", false, ""},
	}
	for _, c := range cases {
		if got := jobWorkStatutoryDue(c.sent, c.recv, c.capital); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
