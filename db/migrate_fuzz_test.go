package db

import "testing"

// FuzzCompareMigrationNames targets the exact comparator whose digit-vs-text
// ordering bug caused AUD-02 (Stage 50) - a fresh-install migration failure
// that manual review only caught after the fact. Native Go fuzzing (BLD-018)
// checks the two properties any total-order comparator must hold, on top of
// the panic-freedom fuzzing gets for free: reflexivity (a string always
// compares equal to itself) and antisymmetry (swapping the arguments negates
// the sign). A violation of either would mean sort.Slice over migration
// filenames could produce a different, order-dependent result depending on
// which element the sort happens to compare first - silently reintroducing
// AUD-02's failure mode in a new shape.
func FuzzCompareMigrationNames(f *testing.F) {
	for _, seed := range []struct{ a, b string }{
		{"migrations_stage26_4_pim_maturity.sql", "migrations_stage26_10_1_stock_ledger.sql"},
		{"migrations_stage47_7_6_audit_archive.sql", "migrations_stage47_7_audit_evidence.sql"},
		{"migration.sql", "migrations_phase3.sql"},
		{"", ""},
		{"a", "a1"},
		{"9999999999999999999999.sql", "1.sql"},
	} {
		f.Add(seed.a, seed.b)
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		self := compareMigrationNames(a, a)
		if self != 0 {
			t.Fatalf("compareMigrationNames(%q, %q) = %d, want 0 (reflexivity)", a, a, self)
		}
		ab := compareMigrationNames(a, b)
		ba := compareMigrationNames(b, a)
		if ab != -ba {
			t.Fatalf("compareMigrationNames(%q, %q) = %d but compareMigrationNames(%q, %q) = %d, want negation (antisymmetry)", a, b, ab, b, a, ba)
		}
	})
}
