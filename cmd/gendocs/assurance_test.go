package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Stage 49.17.8: a lesson without a live regression test is a lesson that can
// quietly come back.
func TestStage49178EveryLessonHasALiveRegressionTest(t *testing.T) {
	root := filepath.Join("..", "..")
	ls := lessons(root)
	if len(ls) < 5 {
		t.Fatalf("read only %d lessons from docs/security/security-lessons.md", len(ls))
	}
	defined := map[string]bool{}
	fn := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	for _, dir := range []string{"engines", "internal", "cmd"} {
		filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, "_test.go") {
				b, _ := os.ReadFile(path)
				for _, m := range fn.FindAllSubmatch(b, -1) {
					defined[string(m[1])] = true
				}
			}
			return nil
		})
	}
	for _, l := range ls {
		switch {
		case l.test == "":
			t.Errorf("lesson %s (%s) names no regression test", l.date, l.wrong)
		case !defined[l.test]:
			t.Errorf("lesson %s names %s, which no longer exists - restore the guard or name its replacement", l.date, l.test)
		}
	}
}

// Stage 49.17.5: an activity is judged by its own evidence file's date.
func TestStage49175CadenceFlagsOverdueAndNeverDone(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("docs/security/threat_model.md", "---\nlast_verified: 2026-01-01\n---\n")
	write("docs/security/risk_register.md", "---\nlast_verified: 2026-10-01\n---\n")
	write("docs/security/assessment-register.md", "| 2026-10-20 | internal-review | monthly access review | owner | live | 2027-04-18 |\n")
	asOf := evidenceAsOf(root, "2026-10-26")
	days := func(d string) int {
		tm, err := time.Parse("2006-01-02", d)
		if err != nil {
			return -1
		}
		return int(asOf.Sub(tm).Hours() / 24)
	}
	got := map[string]string{}
	for _, r := range cadenceRows(root, days) {
		got[r.area] = r.state
	}
	for area, want := range map[string]string{
		"Cadence: threat model review":          "attention: overdue",
		"Cadence: risk register review":         "current",
		"Cadence: access and detection review":  "current",
		"Cadence: tabletop exercise":            "never logged",
		"Cadence: independent penetration test": "never logged",
	} {
		if !strings.Contains(got[area], want) {
			t.Errorf("%s = %q, want it to contain %q", area, got[area], want)
		}
	}
}

// Stage 49.17.8: the statement names the shipped version and says plainly
// that no external assessment exists while the register has none.
func TestStage49178AssuranceStatementIsHonest(t *testing.T) {
	root := filepath.Join("..", "..")
	body := string(assuranceFiles(root, "2026-10-26")[assurancePath])
	version, _ := os.ReadFile(filepath.Join(root, "internal", "server", "VERSION"))
	for _, want := range []string{"Version " + strings.TrimSpace(string(version)), "Security contact", "open risks", "No independent external assessment"} {
		if !strings.Contains(body, want) {
			t.Errorf("statement is missing %q", want)
		}
	}
}
