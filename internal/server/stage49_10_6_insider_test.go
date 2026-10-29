package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 49.10.6 - insider and red-team scenarios. The map from each scenario
// to the tests that prove it is docs/security/insider-scenarios.md; these are
// the scenarios that had no test, and the check that keeps the map honest.

var insiderScenarios = []string{
	"Cashier payroll access", "Admin cross-tenant action", "DBA audit tamper",
	"Support impersonation", "Malicious import or attachment", "Price, refund and threshold fraud",
	"Owner crossover", "Connector SSRF and secret theft", "Stolen backup",
	"Compromised CI artifact", "Destructive operator",
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// Every scenario the item names has a section; every section names a test
// or says what is not covered; every test named exists, untagged.
func TestStage49106InsiderScenarioMapIsHonest(t *testing.T) {
	root := repoRoot(t)
	doc, err := os.ReadFile(filepath.Join(root, "docs", "security", "insider-scenarios.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc = []byte(strings.ReplaceAll(string(doc), "\r\n", "\n")) // a Windows checkout writes CRLF
	sections := regexp.MustCompile(`(?m)^## \d+\. (.+)$`).FindAllStringSubmatchIndex(string(doc), -1)
	if len(sections) != len(insiderScenarios) {
		t.Fatalf("%d scenario sections, want %d", len(sections), len(insiderScenarios))
	}

	// Every func Test... in the repository's test files, and whether its file
	// is kept out of the ordinary run by a build tag.
	tests := map[string]bool{} // name -> tagged
	funcRe := regexp.MustCompile(`(?m)^func (Test\w+)\(`)
	for _, dir := range []string{"engines", "internal", "db", "cmd"} {
		filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			tagged := strings.HasPrefix(string(src), "//go:build")
			for _, m := range funcRe.FindAllStringSubmatch(string(src), -1) {
				tests[m[1]] = tagged
			}
			return nil
		})
	}
	if len(tests) < 1000 {
		t.Fatalf("found only %d tests - the walk is wrong", len(tests))
	}

	cited := regexp.MustCompile("`(Test\\w+)`")
	for i, s := range sections {
		title := string(doc[s[2]:s[3]])
		if title != insiderScenarios[i] {
			t.Errorf("section %d is %q, want %q", i+1, title, insiderScenarios[i])
		}
		end := len(doc)
		if i+1 < len(sections) {
			end = sections[i+1][0]
		}
		body := string(doc[s[1]:end])
		names := cited.FindAllStringSubmatch(body, -1)
		if len(names) == 0 && !strings.Contains(body, "**Not covered:**") {
			t.Errorf("%s: names no test and says nothing is uncovered", title)
		}
		if !strings.Contains(body, "**Not covered:**") {
			t.Errorf("%s: has no **Not covered:** line (write \"none for the scenario as written\" when that is true)", title)
		}
		for _, n := range names {
			tagged, ok := tests[n[1]]
			switch {
			case !ok:
				t.Errorf("%s: cites %s, which does not exist", title, n[1])
			case tagged:
				t.Errorf("%s: cites %s, which a build tag keeps out of the ordinary run", title, n[1])
			}
		}
	}
}

// Support impersonation: opening a client as its support user leaves the
// client's own audit log naming the operator who did it, at issue and at
// sign-in.
func TestStage49106SupportSessionLeavesEvidence(t *testing.T) {
	db.InitDB(testConnStr())
	c := provisionHarnessClient(t, "insider_"+time.Now().Format("150405"))
	operator := "operator-qa-49106"
	code, err := engines.IssueSupportHandoff(c.tenant, operator)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, _, _, _, _, err := engines.RedeemSupportHandoff(c.tenant, code); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	count := func(status, like string) int {
		var n int
		if err := db.DB.QueryRow(`SELECT count(*) FROM `+c.schema+`.audit_logs WHERE action = 'LOGIN' AND status = $1 AND details LIKE $2`,
			status, like).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("OPERATOR_OPEN_ISSUED", "%"+operator+"%") != 1 {
		t.Error("issuing the hand-off left no audit row naming the operator in the client's log")
	}
	if count("OPERATOR_SESSION", "%hand-off%") != 1 {
		t.Error("the support sign-in left no audit row in the client's log")
	}
	if _, _, _, _, _, err := engines.RedeemSupportHandoff(c.tenant, code); err == nil {
		t.Error("a hand-off code worked twice")
	}
}

// Stolen backup: what leaves the box is only ever the encrypted dump.
func TestStage49106BackupLeavesTheBoxEncrypted(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "backup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(src)
	for _, must := range []string{
		`set -euo pipefail`,                      // a failed encryption stops the run
		`: "${BACKUP_ENCRYPTION_KEY:?`,           // no key, no backup
		`openssl enc -aes-256-cbc -pbkdf2 -salt`, // encrypted at rest
	} {
		if !strings.Contains(script, must) {
			t.Errorf("deploy/backup.sh no longer contains %q", must)
		}
	}
	enc := strings.Index(script, `openssl enc -aes-256-cbc`)
	rm := strings.Index(script, `rm -f "$dump"`)
	if enc < 0 || rm < enc {
		t.Error("the plain dump is not removed right after it is encrypted")
	}
	// Every off-box copy of the database is the encrypted file or its
	// checksum. "$dir/$rel" is the media store (product images and
	// attachments), copied as files - the known gap insider-scenarios.md
	// §9 records; a new plain copy of anything else fails here.
	for _, m := range regexp.MustCompile(`offsite_put "([^"]+)"`).FindAllStringSubmatch(script, -1) {
		switch m[1] {
		case "$enc", "$enc.sha256", "$dir/$rel":
		default:
			t.Errorf("offsite_put copies %q off the box - only the encrypted dump may leave", m[1])
		}
	}
}
