package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Stage 49.10.8: finding discipline. Every security finding is one row of
// the finding register in docs/security/risk_register.md, and a row is never
// closed on a code change alone: a Closed row names the fix, a regression
// test that still exists in the tree and a dated, passing retest. Severity is
// never quietly relabelled - a changed rating keeps the old one and says why.

var (
	findingRow   = regexp.MustCompile(`(?m)^\| (F-\d+) \|(.*)\|\s*$`)
	findingTier  = regexp.MustCompile(`\b(Critical|High|Medium|Low)\b`)
	findingDate  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	findingTests = regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")
)

// findingColumns is the register's header, after ID.
var findingColumns = []string{"Found", "Finding", "Severity", "Affected", "Evidence", "Owner / SLA", "Fix", "Regression test", "Retest", "Disclosure", "Status"}

type finding struct {
	id    string
	cells map[string]string
}

// findingRegister reads the "## Finding register" section's rows.
func findingRegister(raw string) []finding {
	start := strings.Index(raw, "\n## Finding register")
	if start < 0 {
		return nil
	}
	section := raw[start+1:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	var out []finding
	for _, m := range findingRow.FindAllStringSubmatch(section, -1) {
		c := cells(m[2])
		f := finding{id: m[1], cells: map[string]string{}}
		for i, name := range findingColumns {
			if i < len(c) {
				f.cells[name] = c[i]
			}
		}
		if len(c) != len(findingColumns) {
			f.cells["_shape"] = "wrong"
		}
		out = append(out, f)
	}
	return out
}

// findingProblems is the discipline itself, separate from the file so the
// negative cases can be tested.
func findingProblems(rows []finding, defined map[string]bool) []string {
	var problems []string
	seen := map[string]bool{}
	blank := func(v string) bool {
		v = strings.TrimSpace(strings.ToLower(v))
		return v == "" || v == "-" || v == "tbd" || v == "n/a"
	}
	for _, f := range rows {
		c := f.cells
		if seen[f.id] {
			problems = append(problems, f.id+": duplicate id")
		}
		seen[f.id] = true
		if c["_shape"] != "" {
			problems = append(problems, f.id+": wrong number of columns")
			continue
		}
		for _, col := range []string{"Found", "Finding", "Affected", "Evidence", "Owner / SLA", "Disclosure"} {
			if blank(c[col]) {
				problems = append(problems, f.id+": "+col+" is empty")
			}
		}
		tiers := findingTier.FindAllString(c["Severity"], -1)
		switch {
		case len(tiers) == 0:
			problems = append(problems, f.id+": severity is not Critical / High / Medium / Low")
		case len(tiers) > 1 && !(strings.Contains(c["Severity"], "→") && strings.Contains(c["Severity"], "(")):
			// A changed rating is written "High → Medium (why)".
			problems = append(problems, f.id+": severity names two ratings without the change and its reason")
		}
		switch c["Status"] {
		case "Open":
		case "Closed":
			if blank(c["Fix"]) {
				problems = append(problems, f.id+": closed with no fix")
			}
			tests := findingTests.FindAllStringSubmatch(c["Regression test"], -1)
			if len(tests) == 0 {
				problems = append(problems, f.id+": closed with no regression test - a code change alone does not close a finding")
			}
			for _, tm := range tests {
				if !defined[tm[1]] {
					problems = append(problems, f.id+": names "+tm[1]+", which does not exist - restore the guard or name its replacement")
				}
			}
			retest := findingDate.FindString(c["Retest"])
			if retest == "" || !strings.Contains(strings.ToLower(c["Retest"]), "pass") {
				problems = append(problems, f.id+": closed with no dated, passing retest")
			} else if found := findingDate.FindString(c["Found"]); found != "" && retest < found {
				problems = append(problems, f.id+": retest "+retest+" is before the finding "+found)
			}
		default:
			problems = append(problems, f.id+": status "+c["Status"]+" is not Open or Closed")
		}
	}
	return problems
}

func definedTestNames(root string) map[string]bool {
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
	return defined
}

func TestStage49108FindingRegisterNeverClosesOnCodeAlone(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, ok := readRepo(root, "docs/security/risk_register.md")
	if !ok {
		t.Fatal("docs/security/risk_register.md is missing")
	}
	rows := findingRegister(strings.ReplaceAll(raw, "\r\n", "\n"))
	if len(rows) < 10 {
		t.Fatalf("read only %d finding rows from the risk register's finding register", len(rows))
	}
	if problems := findingProblems(rows, definedTestNames(root)); len(problems) > 0 {
		t.Fatalf("finding register (49.10.8):\n- %s", strings.Join(problems, "\n- "))
	}
}

// TestStage49108FindingDisciplineRefusesShortcuts: each way to close a
// finding without evidence, or to bury it, is refused.
func TestStage49108FindingDisciplineRefusesShortcuts(t *testing.T) {
	good := func() finding {
		return finding{id: "F-90", cells: map[string]string{
			"Found": "2026-10-25", "Finding": "x", "Severity": "High", "Affected": "every release before 0.15.0",
			"Evidence": "test fails on 0.14.0", "Owner / SLA": "security-owner; next release", "Fix": "claim the token",
			"Regression test": "`TestKnown`", "Retest": "2026-10-27 pass", "Disclosure": "pending", "Status": "Closed",
		}}
	}
	defined := map[string]bool{"TestKnown": true}
	if p := findingProblems([]finding{good()}, defined); len(p) > 0 {
		t.Fatalf("a complete closed finding was refused: %q", p)
	}
	cases := []struct {
		name, col, value, want string
	}{
		{"closed on code alone", "Regression test", "", "no regression test"},
		{"guard deleted", "Regression test", "`TestGone`", "does not exist"},
		{"no retest", "Retest", "", "no dated, passing retest"},
		{"retest failed", "Retest", "2026-10-27 fail", "no dated, passing retest"},
		{"retest before the finding", "Retest", "2026-10-01 pass", "is before the finding"},
		{"relabelled silently", "Severity", "High Low", "without the change and its reason"},
		{"not a tier", "Severity", "Minor", "is not Critical"},
		{"no affected releases", "Affected", "-", "Affected is empty"},
		{"no disclosure decision", "Disclosure", "", "Disclosure is empty"},
		{"status hidden", "Status", "Wontfix", "is not Open or Closed"},
	}
	for _, c := range cases {
		f := good()
		f.cells[c.col] = c.value
		if p := strings.Join(findingProblems([]finding{f}, defined), "\n"); !strings.Contains(p, c.want) {
			t.Errorf("%s: problems %q, want one containing %q", c.name, p, c.want)
		}
	}
	relabel := good()
	relabel.cells["Severity"] = "High → Medium (only reachable with a leaked reset link)"
	if p := findingProblems([]finding{relabel}, defined); len(p) > 0 {
		t.Errorf("a recorded, reasoned change of rating was refused: %q", p)
	}
}
