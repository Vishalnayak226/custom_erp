package securityscan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Stage 49.6.6's reviewed committed-secret inventory: every hit ScanSecrets
// finds in a tracked file, with the reason it is not a live credential. Same
// rules as reviewedBypassFindings - the count is part of the entry, and a
// reason that does not survive being read out loud means the value must go
// (and, if it was ever real, be rotated - deleting it from the tree does not
// take it out of git history).
var reviewedSecretFindings = map[string]map[string]struct {
	Count  int
	Reason string
}{
	"internal/server/stage49_8_5_credentials_test.go": {
		"high-entropy-assignment": {1, `A synthetic connector secret the 49.8.5 test saves and then
			proves no HTTP answer ever reads back. It authenticates to nothing; the test needs a value
			that looks like a real secret, or "never read back" would prove nothing.`},
	},
}

var (
	memoSecretsOnce sync.Once
	memoSecrets     []SecretFinding
	memoSecretFiles []string
	memoSecretsErr  error
)

func cachedSecrets(t *testing.T) ([]SecretFinding, []string) {
	t.Helper()
	memoSecretsOnce.Do(func() {
		memoSecretFiles, memoSecretsErr = TrackedFiles(repoRoot)
		if memoSecretsErr == nil {
			memoSecrets, memoSecretsErr = ScanSecrets(repoRoot, memoSecretFiles)
		}
	})
	if memoSecretsErr != nil {
		t.Fatalf("secret scan (needs git on PATH - the scan reads git's own file list): %v", memoSecretsErr)
	}
	return memoSecrets, memoSecretFiles
}

func TestNoUnreviewedCommittedSecret(t *testing.T) {
	findings, files := cachedSecrets(t)
	if len(files) < 500 {
		t.Fatalf("git lists only %d tracked files - the scan is not reading this repository", len(files))
	}
	counts := CountSecretsByFileAndKind(findings)

	var problems []string
	for file, kinds := range counts {
		for kind, n := range kinds {
			entry, ok := reviewedSecretFindings[file][kind]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("  NEW      %s in %s (%d hit(s)) - remove it and ROTATE it if it was ever real (it stays in git history), or add it to reviewedSecretFindings with the reason it is not a credential", kind, file, n))
			case n > entry.Count:
				problems = append(problems, fmt.Sprintf("  INCREASE %s in %s: %d hit(s), %d reviewed", kind, file, n, entry.Count))
			case n < entry.Count:
				problems = append(problems, fmt.Sprintf("  STALE    %s in %s: %d hit(s), %d reviewed - lower the count", kind, file, n, entry.Count))
			}
		}
	}
	for file, kinds := range reviewedSecretFindings {
		for kind := range kinds {
			if counts[file][kind] == 0 {
				problems = append(problems, fmt.Sprintf("  GONE     %s in %s is reviewed but no longer found - remove the entry", kind, file))
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		for _, f := range findings {
			t.Logf("found: %s %s:%d  %s", f.Kind, f.File, f.Line, f.Excerpt)
		}
		t.Fatalf("the committed-secret inventory (49.6.6) does not match the tree:\n%s", strings.Join(problems, "\n"))
	}
}

// A scanner that matches nothing passes the inventory test above. This plants
// one of every shape in a scratch tree and asserts each is found, that none
// of the planted values reaches an excerpt, and that the look-alikes a real
// repository is full of (placeholders, env references, paths, the AWS
// documentation's example key, setting names) are not findings. The planted
// values are assembled at run time so this file is not itself a finding.
func TestSecretScannerDetectsPlantedShapes(t *testing.T) {
	j := func(parts ...string) string { return strings.Join(parts, "") }
	planted := map[string]string{
		"private-key":                j("-----BEGIN ", "RSA PRIVATE KEY-----"),
		"aws-access-key":             j("AK", "IA", "Q7W3E5R1T9Y2U4I6"),
		"github-token":               j("gh", "p_", "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8"),
		"slack-token":                j("xo", "xb-", "123456789012-", "AbCdEfGhIjKlMnOp"),
		"stripe-key":                 j("sk", "_live_", "a1B2c3D4e5F6g7H8i9J0k1L2"),
		"google-api-key":             j("AI", "za", "Sy9a8B7c6D5e4F3g2H1i0JkLmNoPqRsTuVw"),
		"llm-api-key":                j("sk-", "ant-", "api03-a1B2c3D4e5F6g7H8i9J0"),
		"jwt":                        j("ey", "JhbGciOiJIUzI1NiJ9", ".ey", "JzdWIiOiIxMjM0NTY3ODkwIn0", ".", "dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"),
		"connection-string-password": j("postgres", "://erp:", "Qx7", "vR2mK9@db.internal:5432/erp"),
		"high-entropy-assignment":    j(`client_sec`, `ret = "`, "q8F2kL0vX7mN3bR9tY6wZ1cH5jP4sD0a", `"`),
	}
	dir := t.TempDir()
	var files []string
	for kind, line := range planted {
		name := kind + ".txt"
		if err := os.WriteFile(filepath.Join(dir, name), []byte("before\n"+line+"\nafter\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, name)
	}
	clean := strings.Join([]string{
		j("DATABASE_URL=postgres", "://erp:", "CHANGE_ME_STRONG_DB_PASSWORD@localhost:5432/custom_erp"),
		j("TEST_DATABASE_URL=postgres", "://postgres:", "postgres@localhost:5490/custom_erp"),
		j("url := \"postgres", "://\" + user + \":\" + ${DB_PASSWORD} + \"@host\""),
		j("AWS_ACCESS_KEY_ID=AK", "IA", "IOSFODNN7EXAMPLE"),
		`"tokenUrl": "/api/public/v1/oauth/token"`,
		`const settingKey = "platform.integration_secret_retention_days_value"`,
		`password = "correct horse battery staple"`,
		`JWT_SECRET='local-audit-synthetic-secret-20260916-not-for-deployment'`,
		`if task-1234567890123456789012345678901234567890 { }`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "clean.txt"), []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}
	files = append(files, "clean.txt", "missing-file.txt")

	findings, err := ScanSecrets(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range findings {
		if f.File == "clean.txt" {
			t.Errorf("a look-alike was reported as a secret: %s line %d: %s", f.Kind, f.Line, f.Excerpt)
			continue
		}
		if f.File != f.Kind+".txt" || f.Line != 2 {
			t.Errorf("unexpected finding %s in %s:%d", f.Kind, f.File, f.Line)
		}
		got[f.Kind]++
		if strings.Contains(f.Excerpt, planted[f.Kind]) {
			t.Errorf("%s excerpt carries the secret it found: %s", f.Kind, f.Excerpt)
		}
		if !strings.Contains(f.Excerpt, "<redacted "+f.Kind+">") {
			t.Errorf("%s excerpt is not marked redacted: %s", f.Kind, f.Excerpt)
		}
	}
	for kind := range planted {
		if got[kind] == 0 {
			t.Errorf("the %s pattern found nothing in a file that plants one - it has stopped working", kind)
		}
	}
}

// Two secrets on one line: the excerpt of either finding carries neither.
func TestSecretExcerptRedactsEveryMatchOnTheLine(t *testing.T) {
	a := strings.Join([]string{"AK", "IA", "Q7W3E5R1T9Y2U4I6"}, "")
	b := strings.Join([]string{"gh", "p_", "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8"}, "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "two.env"), []byte("KEYS="+a+","+b+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := ScanSecrets(dir, []string{"two.env"})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("want both secrets found, got %d", len(findings))
	}
	for _, f := range findings {
		if strings.Contains(f.Excerpt, a) || strings.Contains(f.Excerpt, b) {
			t.Errorf("%s excerpt leaks a secret from the same line: %s", f.Kind, f.Excerpt)
		}
	}
}
