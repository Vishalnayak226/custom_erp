package engines

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Stage 74.U.1 / 74.U.2: docs/operations/uat-run-sheet.md carries the BRD's
// section 10 acceptance cases. This pins the list: every UAT-* and E2E-* id
// is a row with all nine columns and a status stated against the code, and
// dropping or renaming a case turns this red.
func TestUATRunSheetCarriesTheBRDCases(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "operations", "uat-run-sheet.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	start := strings.Index(doc, "## 8. BRD acceptance cases")
	rehearsal := strings.Index(doc, "### 8.12 ")
	journeys := strings.Index(doc, "## 9. End-to-end journeys")
	if start < 0 || rehearsal < start || journeys < rehearsal {
		t.Fatal("the run sheet lost its section 8 / 8.12 / section 9 headings")
	}
	cases := doc[start:rehearsal]
	e2e := doc[journeys:]

	// family prefix -> how many cases the BRD lists for it.
	families := []struct {
		prefix string
		count  int
	}{{"DC", 6}, {"ADV", 7}, {"MAT", 5}, {"ADJ", 6}, {"JW", 6}, {"SC", 6},
		{"GST", 10}, {"PRT", 5}, {"RSN", 5}, {"POGRN", 6}, {"PF", 6}}
	status := regexp.MustCompile(`^(Ready|Partial|Waits on)\b`)

	rowOf := func(part, id string) []string {
		for _, line := range strings.Split(part, "\n") {
			if strings.HasPrefix(line, "| "+id+" |") {
				cells := strings.Split(strings.Trim(line, "| "), " | ")
				return cells
			}
		}
		return nil
	}
	total := 0
	for _, f := range families {
		for n := 1; n <= f.count; n++ {
			id := fmt.Sprintf("UAT-%s-%02d", f.prefix, n)
			cells := rowOf(cases, id)
			total++
			if cells == nil {
				t.Errorf("%s is missing from the case tables", id)
				continue
			}
			if len(cells) < 8 { // the trailing blank Result cell is trimmed away
				t.Errorf("%s has %d columns, want id, step, expected, reconciliation, print, denied role, replay, status, result", id, len(cells))
				continue
			}
			if !status.MatchString(cells[7]) {
				t.Errorf("%s has no Ready / Partial / Waits on status: %q", id, cells[7])
			}
			if strings.TrimSpace(cells[1]) == "" || strings.TrimSpace(cells[2]) == "" {
				t.Errorf("%s lost its BRD step or expected result", id)
			}
		}
	}
	if total != 68 {
		t.Fatalf("the BRD lists 68 cases, this test counted %d", total)
	}

	for n := 1; n <= 16; n++ {
		id := fmt.Sprintf("E2E-%02d", n)
		cells := rowOf(e2e, id)
		if cells == nil {
			t.Errorf("%s is missing from the journey table", id)
			continue
		}
		if len(cells) != 6 {
			t.Errorf("%s has %d columns, want id, start state, steps, end state, reconciliation, status", id, len(cells))
			continue
		}
		if !status.MatchString(cells[5]) {
			t.Errorf("%s has no Ready / Partial / Waits on status: %q", id, cells[5])
		}
	}

	if !strings.Contains(doc, "A case passes only with all seven pieces of evidence") {
		t.Error("the pass rule is no longer stated once at the top of section 8")
	}
	if !strings.Contains(doc, "### 9.1 What \"the eleven command APIs\" are") {
		t.Error("section 9.1 (the eleven command APIs) is missing")
	}
}
