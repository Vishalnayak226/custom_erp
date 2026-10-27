package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// BLD-002: docs/qa/generated/backlog-journey-map.md - every open item of the
// live checklist reconciled into the business journeys JRN-01..23
// (docs/product/erp-build-checklist.md, Wave 8), with each journey's owner,
// reference configuration, acceptance environment and what is still open,
// waiting on a decision or blocked. Generated, so the map follows the backlog
// as stages open and close; a stage nobody has placed yet is listed under
// "Unmapped" instead of being guessed. Regenerated with the references (G7).

const journeyMapPath = "docs/qa/generated/backlog-journey-map.md"

var (
	openItemLine = regexp.MustCompile(`(?m)^\s*- \[ \] \*\*(\d+(?:\.[0-9A-Za-z]+)+)\.?\s+([^*]+?)\.?\*\*(.*)$`)
	journeyRow   = regexp.MustCompile(`(?m)^\| \[[ x]\] \| \*\*(JRN-\d+) ([^*]+)\*\* \| [^|]* \| ([^|]+) \|`)
	statusText   = regexp.MustCompile(`\*Status: ([^*]+)\*`)
)

// stageJourney places each stage's open items on the journey they serve.
// "cross" = acceptance or audit work spanning journeys; "eng" = engineering
// housekeeping that no customer journey rests on. A key with a dot (75.2)
// places one part of a stage on its own.
var stageJourney = map[string]string{
	"20": "cross", "26": "cross", "31": "JRN-01", "34": "JRN-08", "38": "JRN-21", "39": "JRN-23",
	"47": "cross", "48": "JRN-23", "49": "JRN-22", "50": "cross", "51": "JRN-07", "52": "JRN-04",
	// Stages 26 and 47 span journeys; these parts follow the JRN table's own
	// owner column, which names the sub-items each journey rests on.
	"26.2": "JRN-12", "26.4": "JRN-08", "26.6": "JRN-09", "26.7": "JRN-15", "26.8": "JRN-16",
	"26.9": "JRN-13", "26.10": "JRN-20", "47.1": "JRN-22", "47.2": "JRN-01", "47.3": "JRN-03",
	"47.4": "JRN-01", "47.5": "JRN-04", "47.6": "JRN-01", "47.7": "JRN-22", "47.8": "JRN-22",
	"47.10": "JRN-20", "47.13": "JRN-08", "47.15": "JRN-23", "47.16": "JRN-12", "49.5": "JRN-03",
	"49.8": "JRN-21",
	"53":   "JRN-01", "55": "cross", "56": "JRN-21", "57": "cross", "58": "JRN-04", "66": "JRN-12",
	"71": "JRN-07", "72": "JRN-08", "73": "cross", "74": "cross", "75": "JRN-21", "75.2": "JRN-12",
	"75.3": "JRN-20", "75.4": "JRN-08", "76": "JRN-07", "77": "JRN-01", "78": "JRN-07", "80": "JRN-08",
	"81": "JRN-08", "83": "JRN-23", "84": "JRN-07", "86": "JRN-07", "88": "JRN-02", "89": "JRN-08",
	"90": "JRN-21", "91": "JRN-23", "92": "JRN-22", "93": "JRN-09", "94": "JRN-07", "95": "JRN-08",
	"97": "JRN-23", "98": "JRN-07", "99": "JRN-02", "100": "JRN-20", "101": "JRN-15", "102": "JRN-01",
	"103": "eng", "104": "JRN-21", "105": "eng", "106": "JRN-04", "107": "JRN-04", "108": "JRN-04",
	"109": "JRN-21", "110": "JRN-07", "111": "JRN-04", "112": "JRN-04", "113": "JRN-04", "114": "JRN-07",
	"115": "JRN-07", "116": "JRN-23", "117": "JRN-08", "118": "JRN-07", "119": "JRN-04", "120": "JRN-23",
	"121": "JRN-04", "122": "JRN-08", "123": "JRN-07", "124": "JRN-23", "125": "JRN-22", "126": "JRN-07",
	"127": "JRN-07", "128": "JRN-23", "129": "JRN-07", "130": "cross", "131": "JRN-15", "132": "eng",
	"133": "JRN-04", "134": "cross", "135": "cross", "136": "cross", "137": "JRN-04", "138": "JRN-23",
}

// journeyConfig is which approved reference configuration (BLD-002 §9) a
// journey is accepted in; "" = outside both, acceptance deferred until a
// customer needs it.
var journeyConfig = map[string]string{
	"JRN-01": "REF-RETAIL-IN", "JRN-02": "REF-RETAIL-IN, REF-WAREHOUSE-IN", "JRN-03": "REF-RETAIL-IN",
	"JRN-04": "REF-RETAIL-IN, REF-WAREHOUSE-IN", "JRN-05": "REF-WAREHOUSE-IN", "JRN-06": "REF-WAREHOUSE-IN",
	"JRN-07": "REF-WAREHOUSE-IN", "JRN-08": "REF-RETAIL-IN", "JRN-09": "REF-RETAIL-IN, REF-WAREHOUSE-IN",
	"JRN-12": "REF-RETAIL-IN, REF-WAREHOUSE-IN", "JRN-15": "REF-RETAIL-IN", "JRN-20": "REF-RETAIL-IN, REF-WAREHOUSE-IN",
	"JRN-21": "REF-RETAIL-IN, REF-WAREHOUSE-IN", "JRN-22": "REF-RETAIL-IN, REF-WAREHOUSE-IN", "JRN-23": "REF-RETAIL-IN, REF-WAREHOUSE-IN",
}

type journeyItem struct{ id, title, flag string }

func journeyFor(id string) (string, string) {
	parts := strings.SplitN(id, ".", 3)
	if len(parts) >= 2 {
		if j, ok := stageJourney[parts[0]+"."+parts[1]]; ok {
			return j, parts[0]
		}
	}
	return stageJourney[parts[0]], parts[0]
}

func journeyMapFiles(checklist, buildChecklist, stamp string) map[string][]byte {
	type journey struct{ name, owner string }
	journeys := map[string]journey{}
	var order []string
	for _, m := range journeyRow.FindAllStringSubmatch(buildChecklist, -1) {
		journeys[m[1]] = journey{strings.TrimSpace(m[2]), strings.TrimSpace(m[3])}
		order = append(order, m[1])
	}
	byJourney := map[string][]journeyItem{}
	unmappedStages := map[string]int{}
	total := 0
	for _, m := range openItemLine.FindAllStringSubmatch(checklist, -1) {
		total++
		it := journeyItem{id: m[1], title: strings.TrimSpace(m[2])}
		if len(it.title) > 90 {
			it.title = strings.TrimSpace(it.title[:87]) + "..."
		}
		rest := strings.ToLower(m[3])
		status := ""
		if s := statusText.FindStringSubmatch(m[3]); s != nil {
			status = strings.ToLower(s[1])
		}
		switch {
		case strings.Contains(rest, "needs design decision") || strings.Contains(rest, "[needs decision") || strings.HasPrefix(status, "needs decision"):
			it.flag = "decision"
		case strings.HasPrefix(status, "blocked"):
			it.flag = "blocked"
		}
		j, stage := journeyFor(it.id)
		if j == "" {
			unmappedStages[stage]++
			j = "unmapped"
		}
		byJourney[j] = append(byJourney[j], it)
	}

	var b strings.Builder
	b.WriteString(generatedHeader("Backlog Journey Map", stamp,
		"`docs/micro_checklist.md` open items, `docs/product/erp-build-checklist.md` JRN table",
		"`pwsh docs/update-docs.ps1 -Group Content`"))
	fmt.Fprintf(&b, "BLD-002: the %d open checklist items reconciled into the business journeys they serve. ", total)
	b.WriteString("Acceptance environment for every journey: Chromium at three viewports, en-IN, tenant tiers Small / Mid / Large (BLD-002 §9). ")
	b.WriteString("Reference configurations: REF-RETAIL-IN (PIM, OMS, CRM) and REF-WAREHOUSE-IN (WMS, procurement, OMS); a journey in neither is deferred until a customer needs it, which is its exclusion rationale. Item IDs are kept as they are in the checklist.\n\n")
	b.WriteString("| Journey | Reference configuration | Owner | Open | Needs decision | Blocked |\n|---|---|---|---:|---:|---:|\n")
	count := func(items []journeyItem, flag string) int {
		n := 0
		for _, it := range items {
			if it.flag == flag {
				n++
			}
		}
		return n
	}
	for _, id := range order {
		cfg := journeyConfig[id]
		if cfg == "" {
			cfg = "deferred - outside both"
		}
		items := byJourney[id]
		fmt.Fprintf(&b, "| %s %s | %s | %s | %d | %d | %d |\n", id, journeys[id].name, cfg, journeys[id].owner, len(items), count(items, "decision"), count(items, "blocked"))
	}
	for _, g := range []struct{ key, label string }{{"cross", "Cross-journey acceptance and audits"}, {"eng", "Engineering housekeeping"}, {"unmapped", "Unmapped - place these stages"}} {
		items := byJourney[g.key]
		fmt.Fprintf(&b, "| %s | - | - | %d | %d | %d |\n", g.label, len(items), count(items, "decision"), count(items, "blocked"))
	}
	if len(unmappedStages) > 0 {
		stages := make([]string, 0, len(unmappedStages))
		for s, n := range unmappedStages {
			stages = append(stages, fmt.Sprintf("Stage %s (%d)", s, n))
		}
		sort.Strings(stages)
		fmt.Fprintf(&b, "\n**Unmapped:** %s - add each to `stageJourney` in `cmd/gendocs/journeys.go`.\n", strings.Join(stages, ", "))
	}

	sections := append(append([]string{}, order...), "cross", "eng", "unmapped")
	for _, id := range sections {
		items := byJourney[id]
		if len(items) == 0 {
			continue
		}
		title := id
		if j, ok := journeys[id]; ok {
			title = id + " " + j.name
		} else {
			title = map[string]string{"cross": "Cross-journey acceptance and audits", "eng": "Engineering housekeeping", "unmapped": "Unmapped"}[id]
		}
		fmt.Fprintf(&b, "\n## %s\n\n", title)
		for _, it := range items {
			flag := ""
			if it.flag != "" {
				flag = " *(" + it.flag + ")*"
			}
			fmt.Fprintf(&b, "- **%s** %s%s\n", it.id, strings.ReplaceAll(it.title, "|", "/"), flag)
		}
	}
	return map[string][]byte{journeyMapPath: []byte(b.String())}
}
