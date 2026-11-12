package engines

import (
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 47.17.5: every module in the catalog has at least one capability
// maturity entry, every entry is on a real module, and nothing is Certified
// past its expiry (or Certified at all without one).
func TestStage47175EveryModuleHasAMaturityEntry(t *testing.T) {
	db.InitDB(testConnStr())
	modules, err := ListModules()
	if err != nil {
		t.Fatalf("list modules: %v", err)
	}
	if len(modules) == 0 {
		t.Skip("public.modules is empty on this database")
	}
	keys := make([]string, 0, len(modules))
	for _, m := range modules {
		keys = append(keys, m.ModuleKey)
	}
	if problems := CapabilityMaturityProblems(CapabilityMaturityRegistry(), keys, time.Now()); len(problems) > 0 {
		t.Fatalf("capability maturity registry:\n  %s", strings.Join(problems, "\n  "))
	}
}

func TestStage47175CertifiedNeedsALiveExpiry(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	entries := []CapabilityMaturity{
		{Key: "a.expired", Module: "a", Title: "a", Level: MaturityCertified, Evidence: []string{"x"}, VerifiedOn: "2026-01-01", CertifiedUntil: "2026-10-01"},
		{Key: "a.noexpiry", Module: "a", Title: "a", Level: MaturityCertified, Evidence: []string{"x"}, VerifiedOn: "2026-01-01"},
		{Key: "a.live", Module: "a", Title: "a", Level: MaturityCertified, Evidence: []string{"x"}, VerifiedOn: "2026-01-01", CertifiedUntil: "2027-01-01"},
		{Key: "a.prodnoproof", Module: "a", Title: "a", Level: MaturityProduction},
		{Key: "a.badlevel", Module: "a", Title: "a", Level: "Golden"},
	}
	problems := strings.Join(CapabilityMaturityProblems(entries, []string{"a", "b"}, now), "\n")
	for _, want := range []string{
		"a.expired: certification expired 2026-10-01",
		"a.noexpiry: Certified needs certified_until",
		"a.prodnoproof: Production needs evidence",
		"a.badlevel: unknown level Golden",
		"module b has no capability maturity entry",
	} {
		if !strings.Contains(problems, want) {
			t.Errorf("problems miss %q:\n%s", want, problems)
		}
	}
	if strings.Contains(problems, "a.live") {
		t.Errorf("a live certification was reported:\n%s", problems)
	}
}

// The registry is honest by construction: nothing is Certified yet, and a
// Production entry rests on a dated live record.
func TestStage47175RegistryClaimsNoCertification(t *testing.T) {
	for _, e := range CapabilityMaturityRegistry() {
		if e.Level == MaturityCertified {
			t.Errorf("%s is Certified; no external validation is on record", e.Key)
		}
		if e.Level == MaturityProduction && !strings.Contains(strings.Join(e.Evidence, " "), "docs/") {
			t.Errorf("%s is Production without a document pointer in its evidence", e.Key)
		}
	}
}
