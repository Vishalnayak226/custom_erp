package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Stage 49.17.4: drill pass rate over time. Read from the dated drill records
// that already exist: the restore drill log (one `- <timestamp> | ... |
// result=PASS` line per run) and the assessment register's drill and
// tabletop rows (a row there means the exercise was held; it records no
// pass/fail, and the scorecard says so rather than counting it as a pass).
// Each row names the file it came from.

var drillRun = regexp.MustCompile(`(?m)^- (\d{4}-\d{2}-\d{2})T[^|]*\|.*\|\s*result=(PASS|FAIL)\s*$`)

type drillCount struct{ pass, fail int }

// drillRows returns one row per month of restore drills (oldest first), an
// overall restore-drill row, and one row per logged drill or tabletop in the
// assessment register. days judges a date as the other rows do.
func drillRows(root string, days func(string) int) []scoreRow {
	const logFile = "`docs/operations/restore-drill-log.md`"
	var rows []scoreRow
	if raw, ok := readRepo(root, "docs/operations/restore-drill-log.md"); ok {
		byMonth := map[string]*drillCount{}
		var total drillCount
		latestFail := ""
		for _, m := range drillRun.FindAllStringSubmatch(raw, -1) {
			c := byMonth[m[1][:7]]
			if c == nil {
				c = &drillCount{}
				byMonth[m[1][:7]] = c
			}
			if m[2] == "PASS" {
				c.pass++
				total.pass++
			} else {
				c.fail++
				total.fail++
				if m[1] > latestFail {
					latestFail = m[1]
				}
			}
		}
		months := make([]string, 0, len(byMonth))
		for k := range byMonth {
			months = append(months, k)
		}
		sort.Strings(months)
		for _, mo := range months {
			c := byMonth[mo]
			state := "all passed"
			if c.fail > 0 {
				state = fmt.Sprintf("attention: %d failed", c.fail)
			}
			rows = append(rows, scoreRow{"Drill pass rate " + mo, fmt.Sprintf("restore drills: %d of %d passed", c.pass, c.pass+c.fail), state, logFile})
		}
		if n := total.pass + total.fail; n > 0 {
			state := "no failed run"
			if total.fail > 0 {
				state = fmt.Sprintf("attention: %d failed, latest %s", total.fail, latestFail)
			}
			rows = append(rows, scoreRow{"Drill pass rate (restore, all)", fmt.Sprintf("%d of %d passed (%d%%)", total.pass, n, total.pass*100/n), state, logFile})
		}
	}
	reg, _ := readRepo(root, "docs/security/assessment-register.md")
	for _, m := range tableRow.FindAllStringSubmatch(reg, -1) {
		c := cells(m[2])
		if len(c) < 5 {
			continue
		}
		date, kind, scope, evidence := m[1], c[0], c[1], c[3]
		if kind != "tabletop" && !strings.Contains(strings.ToLower(scope), "drill") {
			continue
		}
		if i := strings.Index(scope, ":"); i > 0 && i < 40 {
			scope = scope[:i]
		}
		rows = append(rows, scoreRow{"Drill: " + scope, "held " + date + "; no pass/fail recorded", fmt.Sprintf("logged %d days ago", max(0, days(date))), evidence})
	}
	return rows
}
