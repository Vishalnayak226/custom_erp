package engines

import "testing"

// Ask 215.18: a B2C inter-state invoice goes to B2CL above Rs 1 lakh from
// 1 August 2024 and above Rs 2.5 lakh before it.
func TestGSTR1B2CLLimitByDate(t *testing.T) {
	cases := []struct {
		day  string
		want float64
	}{
		{"31-07-2024", 250000},
		{"01-08-2024", 100000},
		{"2024-07-31", 250000},
		{"2024-08-01T10:00:00Z", 100000},
		{"15-11-2026", 100000},
		{"", 100000},
	}
	for _, c := range cases {
		if got := gstr1B2CLLimit(c.day); got != c.want {
			t.Errorf("gstr1B2CLLimit(%q) = %v, want %v", c.day, got, c.want)
		}
	}
	// Rs 1.5 lakh inter-state, no GSTIN: B2CS in July 2024, B2CL from August.
	d := gstr1Doc{interstate: true, val: RupeesToPaise(150000), date: "31-07-2024"}
	if s := d.section(); s != "b2cs" {
		t.Errorf("July 2024 Rs 1.5 lakh: section %q, want b2cs", s)
	}
	d.date = "01-08-2024"
	if s := d.section(); s != "b2cl" {
		t.Errorf("August 2024 Rs 1.5 lakh: section %q, want b2cl", s)
	}
	// Exactly Rs 1 lakh is not above the limit; intra-state never is B2CL.
	d.val = RupeesToPaise(100000)
	if s := d.section(); s != "b2cs" {
		t.Errorf("Rs 1 lakh exactly: section %q, want b2cs", s)
	}
	d.val, d.interstate = RupeesToPaise(500000), false
	if s := d.section(); s != "b2cs" {
		t.Errorf("intra-state Rs 5 lakh: section %q, want b2cs", s)
	}
}
