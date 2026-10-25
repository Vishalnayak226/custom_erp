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
	write("docs/security/risk_register.md", "## Open risks\n\n### R-01 — a\n- **Review:** 2026-10-01.\n\n### R-02 — b\n- **Review:** 2027-01-01.\n\n## Closed\n\n### R-03 — c\n- **Review:** 2020-01-01.\n")
	write("docs/operations/restore_drill_log.md", "## 2026-08-07 — drill\n")
	write("docs/security/old.md", "---\nreview_by: 2026-09-01\n---\n")

	body := string(scorecardFiles(root, "2026-10-26")[scorecardPath])
	for _, want := range []string{
		"| Open risks | 2 open | attention: 1 review date(s) passed |",
		"| Restore drill | last logged 2026-08-07 | attention: last logged drill 80 days ago",
		"attention: past review_by - old.md",
		"| Attack surface | manifest not found | attention |",
		"| Detections |",
		"| User-reported security friction | - | not measured yet |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scorecard misses %q:\n%s", want, body)
		}
	}
}
