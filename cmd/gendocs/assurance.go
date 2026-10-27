package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Stage 49.17.5 / 49.17.7 / 49.17.8: the operating cadence, the assessment
// register and the lessons register, read from their own files; and
// docs/qa/generated/security-assurance-statement.md - the dated, honest
// disclosure (supported version and configuration, security contact, material
// known limits, assurance scope), generated so it cannot drift from the
// evidence it quotes. Regenerated with the other references (gate G7).

const assurancePath = "docs/qa/generated/security-assurance-statement.md"

var (
	tableRow         = regexp.MustCompile(`(?m)^\| (\d{4}-\d{2}-\d{2}) \|(.*)\|\s*$`)
	lastVerifiedLine = regexp.MustCompile(`(?m)^last_verified: (\d{4}-\d{2}-\d{2})`)
	riskBlock        = regexp.MustCompile(`(?m)^### (R-\d+) — (.+)$`)
	severityCell     = regexp.MustCompile(`\| \*\*Severity\*\* \| (\w+)`)
	testName         = regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")
)

type assessment struct{ date, kind, scope, expires string }

type lesson struct{ date, source, wrong, test string }

type openRisk struct{ id, title, severity string }

func readRepo(root, rel string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	return string(b), err == nil
}

func cells(row string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func assessments(root string) []assessment {
	raw, _ := readRepo(root, "docs/security/assessment-register.md")
	var out []assessment
	for _, m := range tableRow.FindAllStringSubmatch(raw, -1) {
		c := cells(m[2])
		if len(c) >= 5 {
			out = append(out, assessment{date: m[1], kind: c[0], scope: c[1], expires: c[4]})
		}
	}
	return out
}

func lessons(root string) []lesson {
	raw, _ := readRepo(root, "docs/security/security-lessons.md")
	var out []lesson
	for _, m := range tableRow.FindAllStringSubmatch(raw, -1) {
		c := cells(m[2])
		if len(c) >= 4 {
			l := lesson{date: m[1], source: c[0], wrong: c[1]}
			if t := testName.FindStringSubmatch(c[3]); t != nil {
				l.test = t[1]
			}
			out = append(out, l)
		}
	}
	return out
}

func openRisks(root string) []openRisk {
	raw, ok := readRepo(root, "docs/security/risk_register.md")
	if !ok {
		return nil
	}
	if i := strings.Index(raw, "\n## Closed"); i >= 0 {
		raw = raw[:i]
	}
	idx := riskBlock.FindAllStringSubmatchIndex(raw, -1)
	var out []openRisk
	for i, m := range idx {
		end := len(raw)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		r := openRisk{id: raw[m[2]:m[3]], title: raw[m[4]:m[5]], severity: "unrated"}
		if s := severityCell.FindStringSubmatch(raw[m[0]:end]); s != nil {
			r.severity = s[1]
		}
		out = append(out, r)
	}
	return out
}

// evidenceAsOf is the newest dated ledger section, or stamp when that is
// later: deterministic for a source revision, and it moves with the work.
func evidenceAsOf(root, stamp string) time.Time {
	asOf := stamp
	if ledger, ok := readRepo(root, "docs/project_ledger.md"); ok {
		for _, m := range ledgerDate.FindAllStringSubmatch(ledger, -1) {
			if m[1] > asOf {
				asOf = m[1]
			}
		}
	}
	t, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return time.Now()
	}
	return t
}

// cadenceRows judges each activity of docs/security/operating-cadence.md by
// its own evidence file's date.
func cadenceRows(root string, days func(string) int) []scoreRow {
	latestKind := map[string]string{}
	for _, a := range assessments(root) {
		if a.date > latestKind[a.kind] {
			latestKind[a.kind] = a.date
		}
	}
	verified := func(rel string) string {
		raw, _ := readRepo(root, rel)
		if m := lastVerifiedLine.FindStringSubmatch(raw); m != nil {
			return m[1]
		}
		return ""
	}
	activities := []struct {
		name, date, evidence string
		every                int
	}{
		{"threat model review", verified("docs/security/threat_model.md"), "`docs/security/threat_model.md`", 100},
		{"risk register review", verified("docs/security/risk_register.md"), "`docs/security/risk_register.md`", 100},
		{"vendor review", verified("docs/security/third-party-risk-register.md"), "`docs/security/third-party-risk-register.md`", 100},
		{"access and detection review", latestKind["internal-review"], "`docs/security/assessment-register.md`", 35},
		{"tabletop exercise", latestKind["tabletop"], "`docs/security/assessment-register.md`", 100},
		{"program review", verified("docs/security/secure-development-lifecycle.md"), "`docs/security/secure-development-lifecycle.md`", 380},
		{"independent penetration test", latestKind["external"], "`docs/security/assessment-register.md`", 380},
	}
	var rows []scoreRow
	for _, a := range activities {
		measure, state := "never done", fmt.Sprintf("attention: due every %d days, never logged", a.every)
		if a.date != "" {
			measure = "last " + a.date
			state = "current"
			if d := days(a.date); d > a.every {
				state = fmt.Sprintf("attention: overdue - %d days, due every %d", d, a.every)
			}
		}
		rows = append(rows, scoreRow{"Cadence: " + a.name, measure, state, a.evidence})
	}
	return rows
}

func assuranceFiles(root, stamp string) map[string][]byte {
	now := evidenceAsOf(root, stamp)
	asOf := now.Format("2006-01-02")
	version, _ := readRepo(root, "internal/server/VERSION")
	version = strings.TrimSpace(version)
	if version == "" {
		version = "unknown"
	}

	var b strings.Builder
	b.WriteString(generatedHeader("Security Assurance Statement", stamp,
		"`internal/server/VERSION`, `docs/security/risk_register.md`, `docs/security/assessment-register.md`, `docs/security/security-lessons.md`",
		"`pwsh docs/update-docs.ps1 -Group Content`"))
	fmt.Fprintf(&b, "What is supported, how to report a problem, what is not yet true, and how far the evidence reaches - as of %s (Stage 49.17.8). Not a certification.\n\n", asOf)

	b.WriteString("## Supported version and configuration\n\n")
	fmt.Fprintf(&b, "- **Version %s** (`internal/server/VERSION`). Only the latest release receives security fixes; there is no long-term support line.\n", version)
	b.WriteString("- **Configuration:** one Go binary and PostgreSQL behind a TLS reverse proxy (Caddy) on a Linux host, installed by `deploy/`; each client in its own database schema; the browser app served by the same binary from its own origin, loading nothing from any other. Nothing else is required: no CDN, WAF, SIEM or secrets service (49.18.5).\n")
	b.WriteString("- **Not supported:** other databases, running without TLS, sharing one schema between clients, or browser extensions that inject scripts into the app.\n\n")

	b.WriteString("## Security contact\n\n")
	b.WriteString("Report through the channel in [SECURITY.md](../../../SECURITY.md): a private GitHub advisory once enabled; until then an issue containing only \"I have a security report, please advise a private channel\". No encryption key is published yet.\n\n")

	b.WriteString("## Material known limits\n\n")
	risks := openRisks(root)
	bySeverity := map[string][]openRisk{}
	for _, r := range risks {
		bySeverity[r.severity] = append(bySeverity[r.severity], r)
	}
	fmt.Fprintf(&b, "- **%d open risks** in the risk register", len(risks))
	if len(risks) > 0 {
		keys := make([]string, 0, len(bySeverity))
		for k := range bySeverity {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%d %s", len(bySeverity[k]), strings.ToLower(k)))
		}
		b.WriteString(" (" + strings.Join(parts, ", ") + ")")
	}
	b.WriteString(". High and critical ones:\n")
	listed := 0
	for _, sev := range []string{"Critical", "High"} {
		for _, r := range bySeverity[sev] {
			fmt.Fprintf(&b, "  - %s %s (%s)\n", r.id, r.title, strings.ToLower(sev))
			listed++
		}
	}
	if listed == 0 {
		b.WriteString("  - none\n")
	}
	b.WriteString("- Rows marked *attention* on the [security scorecard](security-scorecard.md) - overdue reviews, drills and documents - are limits too.\n\n")

	b.WriteString("## Assurance scope\n\n")
	current, expired, external := 0, 0, 0
	latest := map[string]assessment{}
	for _, a := range assessments(root) {
		if a.expires >= asOf {
			current++
		} else {
			expired++
		}
		if a.kind == "external" {
			external++
		}
		if a.date >= latest[a.kind].date {
			latest[a.kind] = a
		}
	}
	fmt.Fprintf(&b, "- **%d current, %d expired** assessments in the [assessment register](../../security/assessment-register.md).\n", current, expired)
	kinds := make([]string, 0, len(latest))
	for k := range latest {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		a := latest[k]
		fmt.Fprintf(&b, "  - latest %s: %s, %s (counts until %s)\n", k, a.date, a.scope, a.expires)
	}
	if external == 0 {
		b.WriteString("- **No independent external assessment has been performed.** Everything above is this project testing itself.\n")
	}
	ls := lessons(root)
	fmt.Fprintf(&b, "- **%d security lessons** fed back, each held by a regression test that runs in every release ([security-lessons.md](../../security/security-lessons.md)).\n", len(ls))
	return map[string][]byte{assurancePath: []byte(b.String())}
}
