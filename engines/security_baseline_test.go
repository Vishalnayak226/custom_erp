package engines

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Stage 49.1.3 tests. All of these are deliberately database-free: they call
// configurationFindings (the env/filesystem half) rather than
// ProductionSecurityBaseline, so they behave identically on a machine with no
// Postgres and cannot be perturbed by the shared-database fixture debris the
// rest of this package's tests live with.

// clearBaselineEnv gives a test a known-empty environment for every variable
// the validator reads, so one test's setting cannot leak into another's
// expectations and so the developer's own shell (which legitimately has
// DATABASE_URL and friends set) does not change the result.
func clearBaselineEnv(t *testing.T) {
	t.Helper()
	for _, name := range securityCriticalEnvVars {
		t.Setenv(name, "")
	}
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		name := kv[:eq]
		if strings.HasPrefix(name, "JWT_SECRET_") || strings.HasPrefix(name, "CHANNEL_CREDENTIAL_KEY_") {
			t.Setenv(name, "")
		}
	}
}

func findingIDs(findings []BaselineFinding) []string {
	ids := make([]string, 0, len(findings))
	for _, f := range findings {
		ids = append(ids, f.ID)
	}
	sort.Strings(ids)
	return ids
}

func hasFinding(findings []BaselineFinding, id string) bool {
	for _, f := range findings {
		if f.ID == id {
			return true
		}
	}
	return false
}

func severityOf(t *testing.T, findings []BaselineFinding, id string) string {
	t.Helper()
	for _, f := range findings {
		if f.ID == id {
			return f.Severity
		}
	}
	t.Fatalf("expected finding %s, got %v", id, findingIDs(findings))
	return ""
}

// A production configuration that satisfies every check must produce no
// findings at all. This is the test that would fail if a future check were
// written so strictly that no real deployment could ever pass it.
func TestFullyConfiguredProductionBaselineIsClean(t *testing.T) {
	clearBaselineEnv(t)
	t.Setenv("ENV", "production")
	t.Setenv("JWT_SECRET", "6f1c0a9d4e7b23815ca6de90f3b47a2158ec6d0b9a4f7231")
	t.Setenv("CHANNEL_CREDENTIAL_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://erp.example.com,https://pos.example.com")
	t.Setenv("TRUST_PROXY", "1")
	t.Setenv("TRUSTED_PROXY_CIDRS", "127.0.0.1/32")
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("DATABASE_URL", "postgres://erp@db.internal:5432/erp?sslmode=verify-full")
	t.Setenv("SHOPIFY_WEBHOOK_SECRET", "not-a-real-secret-value-for-tests")
	t.Setenv("JWT_EXPIRY_HOURS", "12")

	if findings := configurationFindings(); len(findings) > 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding on a fully configured production baseline: %s", f)
		}
	}
}

// The mirror image: an unconfigured production boot must be refused, and the
// specific reasons must be the ones a runbook can act on.
func TestUnconfiguredProductionBaselineBlocks(t *testing.T) {
	clearBaselineEnv(t)
	t.Setenv("ENV", "production")

	findings := configurationFindings()
	for _, want := range []string{
		"SB-002", // no explicit signing key
		"SB-007", // no explicit connector credential key
		"SB-014", // bound to every interface, no proxy trust
		"SB-016", // DATABASE_URL unset
		"SB-019", // Shopify webhook secret unset
	} {
		if !hasFinding(findings, want) {
			t.Errorf("expected finding %s on an unconfigured production baseline; got %v", want, findingIDs(findings))
		}
	}
	if got := severityOf(t, findings, "SB-002"); got != BaselineBlock {
		t.Errorf("SB-002 must block in production, got %q", got)
	}
	// SB-007 deliberately only warns: a deployment with no sales-channel
	// connector encrypts nothing with that key, and refusing to start would be
	// a control operators are entitled to resent. SB-022 is the blocking form
	// and fires only when channel_credentials actually holds rows.
	if got := severityOf(t, findings, "SB-007"); got != BaselineWarn {
		t.Errorf("SB-007 must only warn - SB-022 is the blocking form; got %q", got)
	}
}

// The identical configuration outside production reports the same findings at
// warn severity and never stops the process - the property that keeps a
// developer from deleting the check.
func TestSameGapsOnlyWarnOutsideProduction(t *testing.T) {
	clearBaselineEnv(t)
	t.Setenv("ENV", "")

	findings := configurationFindings()
	for _, f := range findings {
		if f.Severity == BaselineBlock {
			t.Errorf("a development boot must not produce a blocking finding, got %s", f)
		}
	}
	if err := EnforceProductionSecurityBaseline(); err != nil {
		t.Errorf("EnforceProductionSecurityBaseline must never fail outside production, got: %v", err)
	}
}

func TestWildcardCORSOriginBlocksEvenOutsideProduction(t *testing.T) {
	clearBaselineEnv(t)
	t.Setenv("ENV", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://erp.example.com,*")

	findings := configurationFindings()
	if got := severityOf(t, findings, "SB-008"); got != BaselineBlock {
		t.Errorf("a wildcard CORS origin is never acceptable in any environment; severity was %q", got)
	}
}

func TestSpoofableProxyTrustIsDetected(t *testing.T) {
	clearBaselineEnv(t)
	t.Setenv("ENV", "production")
	t.Setenv("TRUST_PROXY", "true")

	if got := severityOf(t, configurationFindings(), "SB-012"); got != BaselineBlock {
		t.Errorf("TRUST_PROXY on with no TRUSTED_PROXY_CIDRS must block in production, got %q", got)
	}
}

func TestUnencryptedRemoteDatabaseLinkBlocks(t *testing.T) {
	clearBaselineEnv(t)
	t.Setenv("ENV", "production")

	cases := []struct {
		name string
		conn string
		want bool
	}{
		{"remote sslmode=disable", "postgres://erp@10.0.0.9:5432/erp?sslmode=disable", true},
		{"remote sslmode missing", "postgres://erp@10.0.0.9:5432/erp", true},
		{"remote key/value form", "host=10.0.0.9 port=5432 dbname=erp sslmode=disable", true},
		{"remote verify-full", "postgres://erp@10.0.0.9:5432/erp?sslmode=verify-full", false},
		{"loopback sslmode=disable", "postgres://erp@127.0.0.1:5432/erp?sslmode=disable", false},
		{"localhost sslmode=disable", "postgres://erp@localhost:5432/erp?sslmode=disable", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tc.conn)
			if got := hasFinding(configurationFindings(), "SB-015"); got != tc.want {
				t.Errorf("SB-015 present = %v, want %v for %s", got, tc.want, tc.conn)
			}
		})
	}
}

func TestPlaceholderSigningKeysAreRejected(t *testing.T) {
	rejected := []string{
		"changeme", "CHANGEME", "secret", "test", "dev-secret-key",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "00000000000000000000000000000000",
		// deploy/erp.env.example ships exactly this value.
		"CHANGE_ME_openssl_rand_hex_48",
	}
	for _, v := range rejected {
		if !isLowEntropyPlaceholder(v) {
			t.Errorf("%q should be recognised as a placeholder signing key", v)
		}
	}
	accepted := []string{
		"6f1c0a9d4e7b23815ca6de90f3b47a2158ec6d0b9a4f7231",
		"contest-manifest-attestation-9f2b7c1e4a680d35", // contains "test" but is not one
	}
	for _, v := range accepted {
		if isLowEntropyPlaceholder(v) {
			t.Errorf("%q is a real key shape and must not be flagged as a placeholder", v)
		}
	}
}

// Stage 49.6.5: a CHANNEL_CREDENTIAL_KEY_<n> rotation keyring is just as valid
// a configuration as the bare var, so SB-007 (unconfigured warn) and SB-022
// (blocking once something is actually stored) must both recognise it -
// mirroring how JWT_SECRET_<n> already suppresses SB-002.
func TestChannelCredentialKeyringSuppressesSB007(t *testing.T) {
	clearBaselineEnv(t)
	t.Setenv("ENV", "production")
	t.Setenv("CHANNEL_CREDENTIAL_KEY_1", "0123456789abcdef0123456789abcdef")

	if hasFinding(configurationFindings(), "SB-007") {
		t.Error("SB-007 should not fire once a CHANNEL_CREDENTIAL_KEY_<n> rotation keyring is configured")
	}
}

func TestChannelCredentialKeyPlaceholderBlocks(t *testing.T) {
	clearBaselineEnv(t)
	t.Setenv("ENV", "production")
	t.Setenv("CHANNEL_CREDENTIAL_KEY", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	if got := severityOf(t, configurationFindings(), "SB-023"); got != BaselineBlock {
		t.Errorf("SB-023 must block a placeholder connector credential key in production, got %q", got)
	}
}

// 49.1.3's own wording: "Errors disclose no secret." Every finding string is
// logged at startup and lands in whatever collects that log, so no finding may
// echo the value it is complaining about.
func TestBaselineFindingsNeverEchoConfiguredValues(t *testing.T) {
	clearBaselineEnv(t)
	const canary = "canary-value-that-must-never-be-logged-7f3a"
	t.Setenv("ENV", "production")
	t.Setenv("JWT_SECRET", canary)
	t.Setenv("CHANNEL_CREDENTIAL_KEY", canary)
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://"+canary+".example.com,*")
	t.Setenv("TRUSTED_PROXY_CIDRS", canary)
	t.Setenv("DATABASE_URL", "postgres://erp:"+canary+"@10.0.0.9:5432/erp?sslmode=disable")
	t.Setenv("JWT_EXPIRY_HOURS", canary)

	findings := configurationFindings()
	if len(findings) == 0 {
		t.Fatal("expected findings for a deliberately bad configuration")
	}
	for _, f := range findings {
		if strings.Contains(f.String(), canary) {
			t.Errorf("finding %s leaks a configured value into its message: %s", f.ID, f.String())
		}
	}
}

// Every environment variable the codebase reads to make a security decision
// must have a check here. Without this test the validator quietly stops being
// complete the first time someone adds a variable and forgets - which is
// exactly how the pre-49.1.3 state (two checks, a dozen variables) came about.
func TestEverySecurityCriticalEnvVarIsChecked(t *testing.T) {
	src, err := os.ReadFile("security_baseline.go")
	if err != nil {
		t.Fatalf("cannot read security_baseline.go: %v", err)
	}
	// Everything after the securityCriticalEnvVars declaration block, so the
	// list itself does not count as its own coverage.
	body := string(src)
	if _, after, found := strings.Cut(body, "func SecurityCriticalEnvVars()"); found {
		body = after
	}
	for _, name := range securityCriticalEnvVars {
		if !strings.Contains(body, `"`+name+`"`) {
			t.Errorf("%s is listed as security-critical but no check in configurationFindings reads it", name)
		}
	}
}

// The reverse direction: a variable that some other file reads inside a
// security decision, but that is missing from securityCriticalEnvVars, is an
// unvalidated knob. This asserts the known set rather than scanning the tree,
// because "reads a variable" and "makes a security decision with it" are not
// the same thing and only a human can tell them apart - but the list being
// asserted at all means adding one is a deliberate, reviewed edit.
func TestSecurityCriticalEnvVarListIsSortedAndComplete(t *testing.T) {
	if !sort.StringsAreSorted(securityCriticalEnvVars) {
		t.Error("securityCriticalEnvVars must stay sorted so additions are readable in a diff")
	}
	want := map[string]bool{
		"CHANNEL_CREDENTIAL_KEY": true, "CORS_ALLOWED_ORIGINS": true, "DATABASE_URL": true,
		"ENV": true, "ERP_DISABLE_EXTERNAL_SIDE_EFFECTS": true, "ERP_ENABLE_EXTERNAL_SIDE_EFFECTS": true,
		"HOST": true, "JWT_EXPIRY_HOURS": true, "JWT_SECRET": true, "SHOPIFY_WEBHOOK_SECRET": true,
		"TRUSTED_PROXY_CIDRS": true, "TRUST_PROXY": true,
	}
	for _, name := range securityCriticalEnvVars {
		if !want[name] {
			t.Errorf("%s was added to securityCriticalEnvVars without updating this test - confirm it has a real check, then add it here", name)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("%s was removed from securityCriticalEnvVars - if the variable is gone from the codebase, remove it here too", name)
	}
}

// db/migration.sql seeds four accounts with literal bcrypt hashes. 24.27
// guarded one of them; 49.1.3 guards all four. This keeps the guarded set
// equal to the seeded set, so a fifth bootstrap account cannot be added to the
// migration without also being guarded.
func TestSeededCredentialHashesCoverMigrationSQL(t *testing.T) {
	sql, err := os.ReadFile("../db/migration.sql")
	if err != nil {
		t.Fatalf("cannot read db/migration.sql: %v", err)
	}
	bcryptLiteral := regexp.MustCompile(`'(\$2[aby]\$\d{2}\$[./A-Za-z0-9]{53})'`)
	seededInSQL := map[string]bool{}
	for _, m := range bcryptLiteral.FindAllStringSubmatch(string(sql), -1) {
		seededInSQL[m[1]] = true
	}
	if len(seededInSQL) == 0 {
		t.Fatal("found no bcrypt hash literals in db/migration.sql - the extraction pattern itself is broken, not the guard")
	}
	guarded := map[string]bool{}
	for _, h := range seededCredentialHashes {
		guarded[h] = true
	}
	for hash := range seededInSQL {
		if !guarded[hash] {
			t.Errorf("db/migration.sql seeds a password hash ending %q that seededCredentialHashes does not guard - add the account to that map so a production boot refuses it", hash[len(hash)-8:])
		}
	}
	for hash := range guarded {
		if !seededInSQL[hash] {
			t.Errorf("seededCredentialHashes guards a hash ending %q that db/migration.sql no longer seeds - remove it", hash[len(hash)-8:])
		}
	}
}

func TestValidSchemaIdentRejectsInjection(t *testing.T) {
	for _, ok := range []string{"tenant_default", "tenant_minn", "t1"} {
		if !validSchemaIdent(ok) {
			t.Errorf("%q is a legitimate schema name and must be accepted", ok)
		}
	}
	for _, bad := range []string{"", "Tenant_Default", "tenant-default", `tenant"; DROP SCHEMA public; --`, "public.users", "1tenant", strings.Repeat("a", 64)} {
		if validSchemaIdent(bad) {
			t.Errorf("%q must be refused as a schema identifier", bad)
		}
	}
}
