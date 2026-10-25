package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"custom_erp/engines"
)

// Stage 49.17.4: docs/qa/generated/security-scorecard.md - the security
// scorecard as evidence, not one magic number. Each row is read from the file
// that holds its evidence, judged against the generation date, and names that
// file. What the repository cannot hold (live access recertification, audit
// growth, user-reported friction) is said to be live or not measured, never
// filled with a guess. Regenerated with the other references (gate G7).

const scorecardPath = "docs/qa/generated/security-scorecard.md"

var (
	riskHeading   = regexp.MustCompile(`(?m)^### (R-\d+) `)
	riskReview    = regexp.MustCompile(`(?m)^- \*\*Review:\*\* (\d{4}-\d{2}-\d{2})`)
	drillHeading  = regexp.MustCompile(`(?m)^## (\d{4}-\d{2}-\d{2})`)
	reviewByLine  = regexp.MustCompile(`(?m)^review_by: (\d{4}-\d{2}-\d{2})`)
	testFuncCount = regexp.MustCompile(`(?m)^func Test`)
	ledgerDate    = regexp.MustCompile(`(?m)^## \d+\..*?\((\d{4}-\d{2}-\d{2})`)
)

type scoreRow struct{ area, measure, state, evidence string }

func scorecardFiles(root, stamp string) map[string][]byte {
	read := func(rel string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		return string(b), err == nil
	}
	// Ages are judged as of the newest dated ledger section, not the fixed
	// generation stamp: deterministic for a source revision, and it moves
	// with the work, so a drill logged months ago reads as stale.
	asOf := stamp
	if ledger, ok := read("docs/project_ledger.md"); ok {
		for _, m := range ledgerDate.FindAllStringSubmatch(ledger, -1) {
			if m[1] > asOf {
				asOf = m[1]
			}
		}
	}
	now, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		now = time.Now()
	}
	// days is how long ago date was; -1 when it cannot be read. A date after
	// asOf counts as 0 - recent, not stale.
	days := func(date string) int {
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			return -1
		}
		return max(0, int(now.Sub(t).Hours()/24))
	}
	var rows []scoreRow
	add := func(area, measure, state, evidence string) {
		rows = append(rows, scoreRow{area, measure, state, evidence})
	}

	// Attack surface and dependency provenance, from the generated manifest.
	if raw, ok := read("docs/security/attack_surface.json"); ok {
		var m struct {
			Routes []struct {
				Auth string `json:"auth"`
			} `json:"routes"`
			EnvironmentFlags []struct {
				SecurityCritical bool `json:"security_critical"`
			} `json:"environment_flags"`
			Dependencies []struct {
				Direct bool `json:"direct"`
			} `json:"dependencies"`
		}
		if json.Unmarshal([]byte(raw), &m) == nil {
			byAuth := map[string]int{}
			for _, r := range m.Routes {
				byAuth[r.Auth]++
			}
			keys := make([]string, 0, len(byAuth))
			for k := range byAuth {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, fmt.Sprintf("%s %d", k, byAuth[k]))
			}
			add("Attack surface", fmt.Sprintf("%d routes: %s", len(m.Routes), strings.Join(parts, ", ")),
				"classified (every route has a capability; tests fail otherwise)", "`docs/security/attack_surface.json`")
			critical, direct := 0, 0
			for _, f := range m.EnvironmentFlags {
				if f.SecurityCritical {
					critical++
				}
			}
			for _, d := range m.Dependencies {
				if d.Direct {
					direct++
				}
			}
			add("Configuration", fmt.Sprintf("%d environment flags, %d security-critical", len(m.EnvironmentFlags), critical), "listed", "`docs/security/attack_surface.json`")
			add("Dependencies", fmt.Sprintf("%d modules, %d direct", len(m.Dependencies), direct), "pinned by go.sum", "`docs/security/attack_surface.json`")
		}
	} else {
		add("Attack surface", "manifest not found", "attention", "`docs/security/attack_surface.json`")
	}
	if raw, ok := read("docs/security/dependency-ledger.json"); ok {
		var l struct {
			Updated string `json:"updated"`
		}
		_ = json.Unmarshal([]byte(raw), &l)
		state := "current"
		if d := days(l.Updated); d < 0 || d > 90 {
			state = "review due (over 90 days)"
		}
		add("Dependency provenance", "ledger updated "+l.Updated, state, "`docs/security/dependency-ledger.json`")
	}

	// Unresolved risk age.
	if raw, ok := read("docs/security/risk_register.md"); ok {
		open := raw
		if i := strings.Index(open, "\n## Closed"); i >= 0 {
			open = open[:i]
		}
		risks := len(riskHeading.FindAllString(open, -1))
		overdue := 0
		for _, m := range riskReview.FindAllStringSubmatch(open, -1) {
			if days(m[1]) > 0 {
				overdue++
			}
		}
		state := "no review overdue"
		if overdue > 0 {
			state = fmt.Sprintf("attention: %d review date(s) passed", overdue)
		}
		add("Open risks", fmt.Sprintf("%d open", risks), state, "`docs/security/risk_register.md`")
	}

	// Detection coverage: the rule catalog and its drill.
	rules := engines.SecurityDetectionRules()
	urgent := 0
	for _, r := range rules {
		if r.Severity == engines.DetectionUrgent {
			urgent++
		}
	}
	add("Detections", fmt.Sprintf("%d rules (%d page, %d queue for review)", len(rules), urgent, len(rules)-urgent),
		"every rule fires in the drill test", "`engines/security_detection.go`, `TestStage49118DetectionDrillCoversEveryRule`")

	// Tenant and authorization negative tests.
	negative := 0
	for _, pattern := range []string{"internal/server/stage47_a*_test.go", "internal/server/stage49_3*_test.go", "internal/server/stage49_4_7*_test.go", "engines/stage47_a*_test.go"} {
		matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		for _, f := range matches {
			b, _ := os.ReadFile(f)
			negative += len(testFuncCount.FindAll(b, -1))
		}
	}
	add("Tenant and access negative tests", fmt.Sprintf("%d test functions", negative), "run in every release (G10)", "`*redteam*_test.go`, `stage49_3*_test.go`, `stage49_4_7*_test.go`")

	// Restore drill.
	if raw, ok := read("docs/operations/restore_drill_log.md"); ok {
		latest := ""
		for _, m := range drillHeading.FindAllStringSubmatch(raw, -1) {
			if m[1] > latest {
				latest = m[1]
			}
		}
		state := "current"
		if d := days(latest); d < 0 || d > 35 {
			state = fmt.Sprintf("attention: last logged drill %d days ago (monthly expected); `deploy/restore_drill.sh` now also checks ledgers and audit evidence", d)
		}
		add("Restore drill", "last logged "+latest, state, "`docs/operations/restore_drill_log.md`")
	}

	// Security documents past their review date.
	if matches, _ := filepath.Glob(filepath.Join(root, "docs", "security", "*.md")); len(matches) > 0 {
		expired := []string{}
		for _, f := range matches {
			b, _ := os.ReadFile(f)
			if m := reviewByLine.FindSubmatch(b); m != nil && days(string(m[1])) > 0 {
				expired = append(expired, filepath.Base(f))
			}
		}
		sort.Strings(expired)
		state := "all within review date"
		if len(expired) > 0 {
			state = "attention: past review_by - " + strings.Join(expired, ", ")
		}
		add("Security documents", fmt.Sprintf("%d documents", len(matches)), state, "`docs/security/*.md` front matter")
	}

	add("Access recertification", "admins without 2FA, API keys to review", "live only", "Activity Log → Security")
	add("Audit and data growth", "audit rows, lost audit writes, verifier failures", "live only", "Activity Log → Security")
	add("User-reported security friction", "-", "not measured yet", "-")

	var b strings.Builder
	b.WriteString(generatedHeader("Security Scorecard", stamp,
		"the security evidence files named in each row", "`pwsh docs/update-docs.ps1 -Group Content`"))
	fmt.Fprintf(&b, "Evidence, not one number (Stage 49.17.4). Judged as of %s, the newest dated ledger section; *attention* rows need an owner's look.\n\n", asOf)
	b.WriteString("| Area | Measure | State | Evidence |\n|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", r.area, r.measure, r.state, r.evidence)
	}
	return map[string][]byte{scorecardPath: []byte(b.String())}
}
