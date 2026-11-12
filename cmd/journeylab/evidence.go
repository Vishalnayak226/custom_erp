package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// BrowserSection carries the accessibility and help-usability results. The
// journeys are driven over HTTP and cannot see a screen, so this section is
// filled from the browser suite's own output and says so.
type BrowserSection struct {
	Source string          `json:"source"` // always "from_browser_suite"
	Status string          `json:"status"` // filled or not_provided
	File   string          `json:"file,omitempty"`
	Note   string          `json:"note"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// Evidence is the JSON file a journey run writes.
type Evidence struct {
	SchemaVersion int             `json:"schema_version"`
	Kind          string          `json:"kind"`
	GeneratedAt   string          `json:"generated_at"`
	BaseURL       string          `json:"base_url"`
	Database      string          `json:"database"`
	DBStatsSource string          `json:"db_stats_source"`
	Host          string          `json:"host"`
	Overall       string          `json:"overall"`
	Journeys      []JourneyRecord `json:"journeys"`
	Browser       BrowserSection  `json:"browser"`
	Limits        []string        `json:"limits"`
}

// LoadBrowser reads the browser suite's file. A missing file is not an error:
// the section says it was not provided.
func LoadBrowser(path string) BrowserSection {
	b := BrowserSection{Source: "from_browser_suite", Status: "not_provided", Note: "accessibility and help usability need a real browser; run the browser suite and pass its output with -browser-evidence"}
	if path == "" {
		return b
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Note = "browser evidence file could not be read: " + err.Error()
		return b
	}
	b.File = filepath.Base(path)
	b.Status = "filled"
	b.Note = "copied unchanged from the browser suite"
	if json.Valid(raw) {
		b.Data = json.RawMessage(raw)
	} else {
		q, _ := json.Marshal(string(raw))
		b.Data = json.RawMessage(q)
	}
	return b
}

// limitsFor lists what this run cannot show, so the evidence never overstates.
func limitsFor(statsSource string) []string {
	l := []string{
		"journeys are driven over HTTP as each role; screen behaviour comes only from the browser section",
		"wall time, CPU and memory are for this machine and this data size; compare runs on the same host",
		"CPU and RSS are the server process's own; database CPU is not included",
	}
	if statsSource != "pg_stat_statements" {
		l = append(l, "database figures come from pg_stat_database: they count transactions and tuples, not individual queries (query_count_available is false); use a cluster with pg_stat_statements for query counts")
	}
	return l
}

func NewEvidence(base, dbName, source string, js []JourneyRecord, browser BrowserSection) Evidence {
	return Evidence{SchemaVersion: evidenceSchemaVersion, Kind: "journeylab.journeys", GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		BaseURL: base, Database: dbName, DBStatsSource: source, Host: fmt.Sprintf("%s/%s, %d CPU", runtime.GOOS, runtime.GOARCH, runtime.NumCPU()),
		Overall: Overall(js), Journeys: js, Browser: browser, Limits: limitsFor(source)}
}

// WriteEvidence writes <out>/journeys-<stamp>.json and .md and returns the paths.
func WriteEvidence(out string, ev Evidence) (string, string, error) {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", "", err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	jp := filepath.Join(out, "journeys-"+stamp+".json")
	b, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jp, b, 0o644); err != nil {
		return "", "", err
	}
	mp := filepath.Join(out, "journeys-"+stamp+".md")
	return jp, mp, os.WriteFile(mp, []byte(Markdown(ev)), 0o644)
}

func mark(s string) string {
	switch s {
	case "pass":
		return "PASS"
	case "fail":
		return "FAIL"
	}
	return strings.ToUpper(s)
}

// recordHeader files a run's Markdown as a dated record under the documentation
// standard (docs/governance/documentation-standard.md): a record keeps what was
// observed and when, so doclint does not ask it for a living document's fields.
func recordHeader(title, capturedAt string) string {
	return fmt.Sprintf("---\ntitle: %s\ntype: record\nstatus: archived\nowner: qa-owner\nauthority: historical\nconfidentiality: internal\ncaptured_at: %s\n---\n\n", title, capturedAt)
}

// Markdown is the human summary of an evidence file.
func Markdown(ev Evidence) string {
	var b strings.Builder
	b.WriteString(recordHeader("Journey evidence", ev.GeneratedAt))
	fmt.Fprintf(&b, "# Journey evidence: %s\n\n", mark(ev.Overall))
	fmt.Fprintf(&b, "Generated %s against %s (database `%s`, host %s). Database figures from `%s`.\n\n", ev.GeneratedAt, ev.BaseURL, ev.Database, ev.Host, ev.DBStatsSource)
	b.WriteString("| Journey | Result | Roles | Wall ms | Calls | API p50 / p95 ms | DB queries (or tx) | DB ms | CPU ms | RSS KB | Heap B | Index B | WAL B |\n|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, j := range ev.Journeys {
		d := j.Summary.DB
		fmt.Fprintf(&b, "| %s %s | %s | %s | %.0f | %d | %.0f / %.0f | %d | %.0f | %.0f | %d | %d | %d | %d |\n", j.ID, j.Title, mark(j.Status), strings.Join(j.Roles, ", "), j.WallMS,
			j.Summary.HTTP.Calls, j.Summary.HTTP.P50MS, j.Summary.HTTP.P95MS, queriesOrTx(d), d.DBTimeMS, j.Summary.CPUMS, j.Summary.RSSKBEnd, d.HeapGrowth, d.IndexGrowth, d.WALBytes)
	}
	for _, j := range ev.Journeys {
		fmt.Fprintf(&b, "\n## %s %s: %s\n\n", j.ID, j.Title, mark(j.Status))
		if j.Reason != "" {
			fmt.Fprintf(&b, "Reason: %s\n\n", j.Reason)
		}
		if len(j.Steps) > 0 {
			b.WriteString("| Step | Role | ms | Calls | Retries | Bytes | HTTP | Errors |\n|---|---|---:|---:|---:|---:|---|---|\n")
			for _, s := range j.Steps {
				st := make([]string, 0, len(s.Status))
				for _, c := range s.Status {
					st = append(st, fmt.Sprint(c))
				}
				fmt.Fprintf(&b, "| %s | %s | %.0f | %d | %d | %d | %s | %s |\n", s.Name, s.Role, s.WallMS, s.HTTP.Calls, s.HTTP.Retries, s.HTTP.ReqBytes+s.HTTP.RespBytes, strings.Join(st, " "), strings.ReplaceAll(strings.Join(s.Errors, "; "), "|", "/"))
			}
		}
		if len(j.Recon) > 0 {
			b.WriteString("\nReconciliation\n\n| Kind | Check | Result | Detail |\n|---|---|---|---|\n")
			for _, c := range j.Recon {
				res := "PASS"
				if !c.Pass {
					res = "FAIL"
					if c.Warn {
						res = "WARN"
					}
				}
				fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.Kind, c.Name, res, strings.ReplaceAll(c.Detail, "|", "/"))
			}
		}
		for _, n := range j.Evidence {
			fmt.Fprintf(&b, "\n- %s", n)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n## Browser (accessibility and help usability): %s\n\nSource: %s. %s\n", ev.Browser.Status, ev.Browser.Source, ev.Browser.Note)
	if ev.Browser.File != "" {
		fmt.Fprintf(&b, "File: `%s`\n", ev.Browser.File)
	}
	b.WriteString("\n## What this run cannot show\n\n")
	for _, l := range ev.Limits {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	return b.String()
}

func queriesOrTx(d DBDelta) int64 {
	if d.QueryCountOK {
		return d.QueryCount
	}
	return d.Transactions
}
