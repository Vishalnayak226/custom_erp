package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stage 49.17.4: the scorecard reads each row from its evidence file and
// judges it against the stamp - an overdue risk review and a stale drill are
// flagged, and a missing input says so instead of failing the generator.
func TestSecurityScorecardJudgesEvidenceAgainstTheStamp(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/security/risk-register.md", "## Open risks\n\n### R-01 — a\n- **Review:** 2026-10-01.\n\n### R-02 — b\n- **Review:** 2027-01-01.\n\n## Closed\n\n### R-03 — c\n- **Review:** 2020-01-01.\n")
	write("docs/operations/restore-drill-log.md", "## 2026-08-07 — drill\n")
	write("docs/security/old.md", "---\nreview_by: 2026-09-01\n---\n")

	body := string(scorecardFiles(root, "2026-10-26")[scorecardPath])
	for _, want := range []string{
		"| Open risks | 2 open | attention: 1 review date(s) passed |",
		"| Restore drill | last logged 2026-08-07 | attention: last logged drill 80 days ago",
		"attention: past review_by - old.md",
		"| Attack surface | manifest not found | attention |",
		"| Detections |",
		"| User-reported security friction | locked accounts and PINs, password resets, lost authenticators, \"not helpful\" on the sign-in help (14 days) | live only |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scorecard misses %q:\n%s", want, body)
		}
	}
}

// 49.17.4: drill pass rate over time reads the dated restore-drill log and
// the register's drill rows; a failed run is flagged, a register row is
// "held" and never counted as a pass, and each row names its file.
func TestSecurityScorecardShowsDrillPassRateOverTime(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/operations/restore-drill-log.md", "# Log\n\n"+
		"- 2026-07-19T05:02:30Z (approx.) | backup=a | verifier=manual | result=PASS\n"+
		"- 2026-07-20T18:04:13Z | backup=b | verifier=auto | result=FAIL\n"+
		"- 2026-08-07T03:09:48Z | backup=c | rows documents=210/210 | result=PASS\n"+
		"## 2026-08-07 - heading is not a run\n")
	write("docs/security/assessment-register.md", "| Date | Kind | Viewpoint | By | Evidence | Expires |\n|---|---|---|---|---|---|\n"+
		"| 2026-10-26 | internal-persona | Detection drill: every rule fired | sessions | `TestDrill` | 2027-04-24 |\n"+
		"| 2026-09-01 | internal-persona | Deep audit | sessions | `x.md` | 2027-02-28 |\n")

	body := string(scorecardFiles(root, "2026-11-01")[scorecardPath])
	for _, want := range []string{
		"| Drill pass rate 2026-07 | restore drills: 1 of 2 passed | attention: 1 failed | `docs/operations/restore-drill-log.md` |",
		"| Drill pass rate 2026-08 | restore drills: 1 of 1 passed | all passed |",
		"| Drill pass rate (restore, all) | 2 of 3 passed (66%) | attention: 1 failed, latest 2026-07-20 |",
		"| Drill: Detection drill | held 2026-10-26; no pass/fail recorded | logged 6 days ago | `TestDrill` |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scorecard misses %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Deep audit") {
		t.Errorf("a non-drill register row was listed as a drill")
	}
}
