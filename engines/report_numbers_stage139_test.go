package engines

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// startupReportIDs is every report registered when the package starts, set by
// TestMain (pim_dam_test.go) before any test registers a throwaway report.
var startupReportIDs []string

// Stage 139.2: every report has a permanent number - R, the area digit, three
// digits (docs/specs/reports/report_numbers.tsv, generated into reportNumbers
// by cmd/gencatalog). The same number for every client, never reused.
func TestStage139EveryReportHasAPermanentNumber(t *testing.T) {
	if len(startupReportIDs) == 0 {
		t.Fatal("startupReportIDs is empty - TestMain (pim_dam_test.go) must take it before m.Run")
	}
	format := regexp.MustCompile(`^R[1-9]\d{3}$`)
	owner := map[string]string{}
	for id, n := range reportNumbers {
		if !format.MatchString(n) {
			t.Errorf("%s: number %q is not R + area digit + 3 digits", id, n)
		}
		if other, dup := owner[n]; dup {
			t.Errorf("%s is given to both %s and %s", n, other, id)
		}
		owner[n] = id
	}

	var missing []string
	product := map[string]bool{}
	for _, id := range startupReportIDs {
		product[id] = true
	}
	for _, d := range ListReportDefinitions() {
		if !product[d.ID] {
			continue // a test's throwaway report
		}
		if reportNumbers[d.ID] == "" {
			missing = append(missing, fmt.Sprintf("%s (%s)", d.ID, d.Category))
		} else if d.Number != reportNumbers[d.ID] {
			t.Errorf("%s: the catalog serves number %q, want %q", d.ID, d.Number, reportNumbers[d.ID])
		}
	}
	for _, s := range catalogSpecs {
		if reportNumbers[s.ID] == "" {
			missing = append(missing, fmt.Sprintf("%s (%s)", s.ID, s.Module))
		}
	}
	if len(missing) > 0 {
		next := map[byte]int{}
		for n := range owner {
			v, _ := strconv.Atoi(n[2:])
			if v >= next[n[1]] {
				next[n[1]] = v + 1
			}
		}
		var free []string
		for area, v := range next {
			free = append(free, fmt.Sprintf("R%c%03d", area, v))
		}
		sort.Strings(free)
		t.Errorf("reports without a number - append each to docs/specs/reports/report_numbers.tsv with the next free number of its area (now %s), then go run ./cmd/gencatalog:\n  %s",
			strings.Join(free, ", "), strings.Join(missing, "\n  "))
	}
}

// The file is append-only: within an area the numbers rise line by line, so a
// renumbered or reused report shows up as an out-of-order line.
func TestStage139ReportNumbersFileIsAppendOnly(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "docs", "specs", "reports", "report_numbers.tsv"))
	if err != nil {
		t.Fatalf("report_numbers.tsv: %v", err)
	}
	defer f.Close()
	last := map[byte]string{}
	rows := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "number\t") {
			continue
		}
		p := strings.Split(line, "\t")
		if len(p) < 3 {
			t.Fatalf("report_numbers.tsv: %q has %d fields, want number, report_id, report", line, len(p))
		}
		rows++
		n, id := p[0], p[1]
		if len(n) == 5 && n <= last[n[1]] {
			t.Errorf("%s (%s) comes after %s: numbers are appended in order, never changed or reused", n, id, last[n[1]])
		}
		if len(n) == 5 {
			last[n[1]] = n
		}
		if reportNumbers[id] != n {
			t.Errorf("%s: the file says %s, the generated map %q - run go run ./cmd/gencatalog", id, n, reportNumbers[id])
		}
	}
	if rows != len(reportNumbers) {
		t.Errorf("the file has %d reports, the generated map %d - run go run ./cmd/gencatalog", rows, len(reportNumbers))
	}
}
