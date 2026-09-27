package engines

import (
	"strings"
	"testing"
)

// FuzzNormalizePhone targets the one cleaning entry point every phone field
// in the app goes through (identifiers/parsers bucket, BLD-018). It never
// returns an error by design - an unusable number comes back Valid=false
// with a Reason instead - so the properties worth fuzzing are panic-freedom
// (arbitrary Unicode/lengths must never crash country-code/trunk-prefix
// slicing) and the Valid/Reason contract every caller relies on: whenever
// there was actually something to judge (raw isn't blank), exactly one of
// "accepted" or "explained" is true. A blank/whitespace-only raw is its own
// third, deliberate state - neither valid nor explained, an optional field
// left empty rather than a rejected value - found by this fuzzer's first
// run and folded into the invariant rather than papered over.
func FuzzNormalizePhone(f *testing.F) {
	for _, seed := range []struct{ raw, iso2 string }{
		{"9876543210", "IN"},
		{"+919876543210", "IN"},
		{"09876543210", "IN"},
		{"", "IN"},
		{"+", ""},
		{"00919876543210", "US"},
		{"++++", "XX"},
		{"9876543210919876543210919876543210", "IN"},
	} {
		f.Add(seed.raw, seed.iso2)
	}
	f.Fuzz(func(t *testing.T, raw, iso2 string) {
		out := NormalizePhone(raw, iso2)
		if strings.TrimSpace(raw) == "" {
			if out.Valid || out.Reason != "" {
				t.Fatalf("NormalizePhone(%q, %q) = %+v: blank input must stay Valid=false with no Reason (nothing to judge)", raw, iso2, out)
			}
			return
		}
		if out.Valid == (out.Reason != "") {
			t.Fatalf("NormalizePhone(%q, %q) = %+v: Valid and Reason must be exact opposites for non-blank input, got Valid=%v Reason=%q", raw, iso2, out, out.Valid, out.Reason)
		}
	})
}
