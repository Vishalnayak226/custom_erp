// Command epicledger keeps the per-epic before/after ledger (Stage 47.18.1).
//
// "record" measures what can be measured from a source tree (static asset
// bytes, dependencies, docs pages, runbooks, tools, routes) and folds in the
// evidence the other tools already write: the release budget JSON
// (cmd/releasebudget) and the journey and soak evidence (cmd/journeylab).
// Anything it cannot measure is written as "not_measured" with the reason,
// never as zero. "compare" prints the difference between two entries.
//
// The ledger is one JSON file (default docs/qa/epic-ledger.json). It only
// reads the tree and the files it is given; it never touches a database.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const schemaVersion = 1

// Metric is one measured figure. Status is "measured" or "not_measured".
type Metric struct {
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Source string  `json:"source"`
	Status string  `json:"status"`
	Note   string  `json:"note,omitempty"`
}

// Entry is the state of the product at one moment.
type Entry struct {
	ID         string            `json:"id"`
	Label      string            `json:"label"`
	RecordedAt string            `json:"recorded_at"`
	Commit     string            `json:"commit,omitempty"`
	Root       string            `json:"root"`
	Metrics    map[string]Metric `json:"metrics"`
}

// Ledger is the file.
type Ledger struct {
	SchemaVersion int     `json:"schema_version"`
	Entries       []Entry `json:"entries"`
}

func measured(v float64, unit, source string) Metric {
	return Metric{Value: v, Unit: unit, Source: source, Status: "measured"}
}

func notMeasured(unit, why string) Metric {
	return Metric{Unit: unit, Status: "not_measured", Note: why}
}

// StaticMetrics reads the tree under root.
func StaticMetrics(root string) map[string]Metric {
	m := map[string]Metric{}
	pub, gz, files := dirBytes(filepath.Join(root, "public"))
	if files > 0 {
		m["static.public_bytes"] = measured(float64(pub), "bytes", "files under public/")
		m["static.public_gzip_bytes"] = measured(float64(gz), "bytes", "each file gzipped on its own, summed (what a browser downloads)")
		m["static.public_files"] = measured(float64(files), "files", "files under public/")
	} else {
		m["static.public_bytes"] = notMeasured("bytes", "no public/ folder under the root")
	}
	m["deps.go_modules"] = countRequires(filepath.Join(root, "go.mod"))
	m["support.guide_and_kb_pages"] = measured(float64(countFiles(filepath.Join(root, "docs", "guides"), ".md")+countFiles(filepath.Join(root, "docs", "kb"), ".md")), "pages", "markdown files under docs/guides and docs/kb")
	m["support.runbooks"] = measured(float64(countFiles(filepath.Join(root, "docs", "operations"), ".md")), "files", "markdown files under docs/operations")
	m["support.cmd_tools"] = measured(float64(countDirs(filepath.Join(root, "cmd"))), "tools", "folders under cmd/")
	m["support.routes_with_permission"] = countRoutes(filepath.Join(root, "internal", "server", "route_capabilities.go"))
	m["support.queue_categories"] = notMeasured("categories", "the product has no support queue, so there is nothing to count")
	m["browser.boot_bytes_and_cpu"] = notMeasured("bytes", "needs a real browser; comes from the browser suite (47.9.4), not from this tool")
	return m
}

func dirBytes(dir string) (raw, gz int64, files int) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		raw += int64(len(b))
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		_, _ = w.Write(b)
		_ = w.Close()
		gz += int64(buf.Len())
		files++
		return nil
	})
	return
}

func countFiles(dir, ext string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(strings.ToLower(p), ext) {
			n++
		}
		return nil
	})
	return n
}

func countDirs(dir string) int {
	es, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range es {
		if e.IsDir() {
			n++
		}
	}
	return n
}

var requireLine = regexp.MustCompile(`^\s*[A-Za-z0-9][^\s]*\.[^\s]*/?[^\s]*\s+v[0-9]`)

func countRequires(path string) Metric {
	b, err := os.ReadFile(path)
	if err != nil {
		return notMeasured("modules", "no go.mod under the root")
	}
	n, in := 0, false
	for _, l := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "require ("):
			in = true
		case in && t == ")":
			in = false
		case in && requireLine.MatchString(l):
			n++
		case strings.HasPrefix(t, "require ") && !strings.HasSuffix(t, "("):
			n++
		}
	}
	return measured(float64(n), "modules", "require lines in go.mod, direct and indirect")
}

var routeLine = regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE) /`)

func countRoutes(path string) Metric {
	b, err := os.ReadFile(path)
	if err != nil {
		return notMeasured("routes", "route_capabilities.go not found under the root")
	}
	return measured(float64(len(routeLine.FindAll(b, -1))), "routes", "method-and-path entries in the route capability registry")
}

// BinaryMetric measures a built binary.
func BinaryMetric(path string) Metric {
	st, err := os.Stat(path)
	if err != nil {
		return notMeasured("bytes", "binary not readable: "+err.Error())
	}
	return measured(float64(st.Size()), "bytes", filepath.Base(path))
}

// BudgetMetrics folds in a cmd/releasebudget report.
func BudgetMetrics(raw []byte) (map[string]Metric, error) {
	var r struct {
		Metrics map[string]struct {
			Value  float64 `json:"value"`
			Unit   string  `json:"unit"`
			Status string  `json:"status"`
			Note   string  `json:"note"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("not a release budget report: %w", err)
	}
	out := map[string]Metric{}
	for k, v := range r.Metrics {
		if v.Status == "not_measured" || v.Status == "unavailable" || v.Status == "skipped" {
			out["budget."+k] = notMeasured(v.Unit, firstNonEmpty(v.Note, "the budget tool could not measure it"))
			continue
		}
		out["budget."+k] = measured(v.Value, v.Unit, "release budget report")
	}
	return out, nil
}

// JourneyMetrics folds in cmd/journeylab journey evidence: memory after the
// journeys and database growth per 1,000 business writes (the median journey
// that wrote anything), plus the backup size and time from the recovery drill.
func JourneyMetrics(raw []byte) (map[string]Metric, error) {
	var ev struct {
		Kind     string `json:"kind"`
		Journeys []struct {
			ID      string   `json:"id"`
			Status  string   `json:"status"`
			Notes   []string `json:"notes"`
			Summary struct {
				Writes    int     `json:"business_writes"`
				RSSKBEnd  int64   `json:"server_rss_kb_end"`
				PeakRSSKB int64   `json:"server_peak_rss_kb"`
				Heap      float64 `json:"heap_bytes_per_1k_tx"`
				Index     float64 `json:"index_bytes_per_1k_tx"`
				WAL       float64 `json:"wal_bytes_per_1k_tx"`
				Audit     float64 `json:"audit_rows_per_1k_tx"`
				Outbox    float64 `json:"outbox_rows_per_1k_tx"`
			} `json:"summary"`
		} `json:"journeys"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("not a journey evidence file: %w", err)
	}
	if ev.Kind != "" && !strings.HasSuffix(ev.Kind, ".journeys") {
		return nil, fmt.Errorf("evidence kind is %q, expected journey evidence", ev.Kind)
	}
	out := map[string]Metric{}
	var heap, idx, wal, aud, obx []float64
	var rss, peak int64
	for _, j := range ev.Journeys {
		if j.Status == "not_driven" {
			continue
		}
		if j.Summary.RSSKBEnd > rss {
			rss = j.Summary.RSSKBEnd
		}
		if j.Summary.PeakRSSKB > peak {
			peak = j.Summary.PeakRSSKB
		}
		if j.Summary.Writes > 0 {
			heap, idx, wal = append(heap, j.Summary.Heap), append(idx, j.Summary.Index), append(wal, j.Summary.WAL)
			aud, obx = append(aud, j.Summary.Audit), append(obx, j.Summary.Outbox)
		}
		for _, n := range j.Notes {
			var bytesN int64
			var secs float64
			if _, err := fmt.Sscanf(n, "backup: %d bytes", &bytesN); err == nil && bytesN > 0 {
				out["backup.size_bytes"] = measured(float64(bytesN), "bytes", "journey J7 backup note")
			}
			if _, err := fmt.Sscanf(n, "backup to verified restore: %f s for %d bytes", &secs, &bytesN); err == nil && secs > 0 {
				out["backup.seconds_to_verified_restore"] = measured(secs, "seconds", "journey J7 note")
			}
		}
	}
	if peak > 0 {
		out["memory.server_rss_after_journeys_kb"] = measured(float64(rss), "KB", "journey evidence, highest end-of-journey reading")
		out["memory.server_peak_rss_kb"] = measured(float64(peak), "KB", "journey evidence")
	} else {
		out["memory.server_rss_after_journeys_kb"] = notMeasured("KB", "journey evidence has no server process readings")
	}
	if len(heap) > 0 {
		src := fmt.Sprintf("median of %d journeys that wrote", len(heap))
		out["db.heap_bytes_per_1k_tx"] = measured(median(heap), "bytes per 1k writes", src)
		out["db.index_bytes_per_1k_tx"] = measured(median(idx), "bytes per 1k writes", src)
		out["db.wal_bytes_per_1k_tx"] = measured(median(wal), "bytes per 1k writes", src)
		for _, k := range []string{"db.wal_bytes_per_1k_tx"} {
			m := out[k]
			m.Note = "WAL position is cluster-wide, so on a shared Postgres it also counts other databases' writes; trust it only from a private cluster"
			out[k] = m
		}
		for _, k := range []string{"db.heap_bytes_per_1k_tx", "db.index_bytes_per_1k_tx"} {
			m := out[k]
			m.Note = "relation sizes grow in 8 KB pages, so a short run is coarse"
			out[k] = m
		}
		out["db.audit_rows_per_1k_tx"] = measured(median(aud), "rows per 1k writes", src)
		out["jobs.outbox_rows_per_1k_tx"] = measured(median(obx), "rows per 1k writes", src)
	} else {
		for _, k := range []string{"db.heap_bytes_per_1k_tx", "db.index_bytes_per_1k_tx", "db.wal_bytes_per_1k_tx", "db.audit_rows_per_1k_tx", "jobs.outbox_rows_per_1k_tx"} {
			out[k] = notMeasured("", "no journey in the evidence made business writes")
		}
	}
	if _, ok := out["backup.size_bytes"]; !ok {
		out["backup.size_bytes"] = notMeasured("bytes", "the recovery drill (J7) was not driven in this evidence")
		out["backup.seconds_to_verified_restore"] = notMeasured("seconds", "the recovery drill (J7) was not driven in this evidence")
	}
	return out, nil
}

// SoakMetrics folds in cmd/journeylab soak evidence: idle-to-peak memory over
// a long run and the 24-hour disk projection.
func SoakMetrics(raw []byte) (map[string]Metric, error) {
	var ev struct {
		Kind    string  `json:"kind"`
		Seconds float64 `json:"duration_actual_seconds"`
		Start   int64   `json:"server_rss_kb_start"`
		End     int64   `json:"server_rss_kb_end"`
		Peak    int64   `json:"server_rss_kb_peak"`
		P95Last float64 `json:"api_p95_ms_last_quarter"`
		Disk    struct {
			Projected int64 `json:"projected_growth_bytes"`
		} `json:"disk_projection_24h"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("not a soak evidence file: %w", err)
	}
	if !strings.Contains(ev.Kind, "soak") {
		return nil, fmt.Errorf("evidence kind is %q, expected soak evidence", ev.Kind)
	}
	src := fmt.Sprintf("soak run of %.0f s", ev.Seconds)
	m := map[string]Metric{
		"memory.soak_rss_start_kb":     measured(float64(ev.Start), "KB", src),
		"memory.soak_rss_peak_kb":      measured(float64(ev.Peak), "KB", src),
		"db.soak_projected_24h_growth": measured(float64(ev.Disk.Projected), "bytes", src+", linear projection"),
	}
	if ev.P95Last > 0 {
		m["api.soak_p95_ms_last_quarter"] = measured(ev.P95Last, "ms", src)
	} else {
		m["api.soak_p95_ms_last_quarter"] = notMeasured("ms", "the soak ran too few cycles (fewer than 8) for a quarter-by-quarter figure")
	}
	return m, nil
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s) == 0 {
		return 0
	}
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// LoadLedger reads the file; a missing file is an empty ledger.
func LoadLedger(path string) (Ledger, error) {
	l := Ledger{SchemaVersion: schemaVersion}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return l, fmt.Errorf("%s is not a ledger: %w", path, err)
	}
	return l, nil
}

// Add appends an entry. A repeated id is refused unless replace is set.
func (l *Ledger) Add(e Entry, replace bool) error {
	for i, x := range l.Entries {
		if x.ID == e.ID {
			if !replace {
				return fmt.Errorf("entry %q already exists (use -replace to overwrite it)", e.ID)
			}
			l.Entries[i] = e
			return nil
		}
	}
	l.Entries = append(l.Entries, e)
	return nil
}

func (l Ledger) Find(id string) (Entry, bool) {
	for _, e := range l.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func (l Ledger) Save(path string) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Row is one line of a comparison.
type Row struct {
	Name         string
	Unit         string
	Before       *float64
	After        *float64
	Delta        *float64
	Pct          *float64
	Note         string
	BeforeStatus string
	AfterStatus  string
}

// Compare lines two entries up. A metric missing or not measured on either
// side shows no delta and says why.
func Compare(before, after Entry) []Row {
	names := map[string]bool{}
	for k := range before.Metrics {
		names[k] = true
	}
	for k := range after.Metrics {
		names[k] = true
	}
	var keys []string
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var rows []Row
	for _, k := range keys {
		b, bok := before.Metrics[k]
		a, aok := after.Metrics[k]
		r := Row{Name: k, Unit: firstNonEmpty(a.Unit, b.Unit)}
		r.BeforeStatus, r.AfterStatus = statusOf(b, bok), statusOf(a, aok)
		if bok && b.Status == "measured" {
			v := b.Value
			r.Before = &v
		}
		if aok && a.Status == "measured" {
			v := a.Value
			r.After = &v
		}
		switch {
		case r.Before != nil && r.After != nil:
			d := *r.After - *r.Before
			r.Delta = &d
			if *r.Before != 0 {
				p := d / *r.Before * 100
				r.Pct = &p
			}
		case !bok:
			r.Note = "not in the before entry"
		case !aok:
			r.Note = "not in the after entry"
		default:
			r.Note = firstNonEmpty(a.Note, b.Note, "not measured")
		}
		rows = append(rows, r)
	}
	return rows
}

func statusOf(m Metric, ok bool) string {
	if !ok {
		return "absent"
	}
	return m.Status
}

func fnum(p *float64) string {
	if p == nil {
		return "-"
	}
	if *p == float64(int64(*p)) {
		return fmt.Sprintf("%d", int64(*p))
	}
	return fmt.Sprintf("%.2f", *p)
}

// CompareMarkdown renders the rows. The front matter files the page as a dated
// record (docs/governance/documentation-standard.md), not a living document.
func CompareMarkdown(before, after Entry, rows []Row) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: Epic before/after\ntype: record\nstatus: archived\nowner: qa-owner\nauthority: historical\nconfidentiality: internal\ncaptured_at: %s\n---\n\n", after.RecordedAt)
	fmt.Fprintf(&b, "# Epic before/after: %s -> %s\n\nBefore: %s (%s, commit %s). After: %s (%s, commit %s).\n\n", before.ID, after.ID, before.Label, before.RecordedAt, short(before.Commit), after.Label, after.RecordedAt, short(after.Commit))
	b.WriteString("| Metric | Unit | Before | After | Change | % | Note |\n|---|---|---:|---:|---:|---:|---|\n")
	for _, r := range rows {
		pct := "-"
		if r.Pct != nil {
			pct = fmt.Sprintf("%+.1f%%", *r.Pct)
		}
		d := "-"
		if r.Delta != nil {
			d = fmt.Sprintf("%+g", round2(*r.Delta))
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", r.Name, r.Unit, fnum(r.Before), fnum(r.After), d, pct, strings.ReplaceAll(r.Note, "|", "/"))
	}
	return b.String()
}

func round2(f float64) float64 { return float64(int64(f*100+sign(f)*0.5)) / 100 }

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

func short(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	if s == "" {
		return "unknown"
	}
	return s
}

func gitHead(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func readFile(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	return os.ReadFile(path)
}

func usage() int {
	fmt.Fprintln(os.Stderr, `usage:
  epicledger record  -id <id> -label <text> [-root .] [-ledger docs/qa/epic-ledger.json]
                     [-binary <exe>] [-budget <releasebudget.json>] [-journeys <journeys-*.json>] [-soak <soak-*.json>]
                     [-commit <sha>] [-replace]
  epicledger compare -before <id> -after <id> [-ledger docs/qa/epic-ledger.json] [-md <file>]`)
	return 2
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) < 1 {
		return usage()
	}
	switch args[0] {
	case "record":
		return cmdRecord(args[1:])
	case "compare":
		return cmdCompare(args[1:])
	}
	return usage()
}

func cmdRecord(args []string) int {
	fs := flag.NewFlagSet("record", flag.ExitOnError)
	id := fs.String("id", "", "entry id, e.g. baseline-2026-11-06 or after-stage-47")
	label := fs.String("label", "", "short description")
	root := fs.String("root", ".", "source tree to measure")
	ledger := fs.String("ledger", filepath.Join("docs", "qa", "epic-ledger.json"), "ledger file")
	binary := fs.String("binary", "", "built server binary to size")
	budget := fs.String("budget", "", "cmd/releasebudget JSON report")
	journeys := fs.String("journeys", "", "cmd/journeylab journey evidence JSON")
	soak := fs.String("soak", "", "cmd/journeylab soak evidence JSON")
	commit := fs.String("commit", "", "commit id (default: git HEAD of -root)")
	replace := fs.Bool("replace", false, "overwrite an entry with the same id")
	_ = fs.Parse(args)
	if *id == "" || *label == "" {
		fmt.Fprintln(os.Stderr, "epicledger: -id and -label are required")
		return 2
	}
	e := Entry{ID: *id, Label: *label, RecordedAt: time.Now().UTC().Format(time.RFC3339), Commit: firstNonEmpty(*commit, gitHead(*root)), Root: *root, Metrics: StaticMetrics(*root)}
	merge := func(m map[string]Metric) {
		for k, v := range m {
			e.Metrics[k] = v
		}
	}
	if *binary != "" {
		e.Metrics["static.server_binary_bytes"] = BinaryMetric(*binary)
	} else {
		e.Metrics["static.server_binary_bytes"] = notMeasured("bytes", "no -binary given")
	}
	for _, in := range []struct {
		flag string
		path string
		f    func([]byte) (map[string]Metric, error)
	}{{"-budget", *budget, BudgetMetrics}, {"-journeys", *journeys, JourneyMetrics}, {"-soak", *soak, SoakMetrics}} {
		if in.path == "" {
			continue
		}
		raw, err := readFile(in.path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "epicledger: %s: %v\n", in.flag, err)
			return 1
		}
		m, err := in.f(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "epicledger: %s %s: %v\n", in.flag, in.path, err)
			return 1
		}
		merge(m)
	}
	if *journeys == "" {
		for _, k := range []string{"memory.server_rss_after_journeys_kb", "db.heap_bytes_per_1k_tx", "db.index_bytes_per_1k_tx", "db.wal_bytes_per_1k_tx", "db.audit_rows_per_1k_tx", "jobs.outbox_rows_per_1k_tx", "backup.size_bytes", "backup.seconds_to_verified_restore"} {
			e.Metrics[k] = notMeasured("", "no -journeys evidence given")
		}
	}
	l, err := LoadLedger(*ledger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "epicledger:", err)
		return 1
	}
	if err := l.Add(e, *replace); err != nil {
		fmt.Fprintln(os.Stderr, "epicledger:", err)
		return 1
	}
	if err := l.Save(*ledger); err != nil {
		fmt.Fprintln(os.Stderr, "epicledger:", err)
		return 1
	}
	n, nm := 0, 0
	for _, m := range e.Metrics {
		if m.Status == "measured" {
			n++
		} else {
			nm++
		}
	}
	fmt.Printf("recorded %q in %s: %d measured, %d not measured\n", e.ID, *ledger, n, nm)
	return 0
}

func cmdCompare(args []string) int {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	before := fs.String("before", "", "id of the earlier entry")
	after := fs.String("after", "", "id of the later entry")
	ledger := fs.String("ledger", filepath.Join("docs", "qa", "epic-ledger.json"), "ledger file")
	md := fs.String("md", "", "also write the comparison to this Markdown file")
	_ = fs.Parse(args)
	l, err := LoadLedger(*ledger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "epicledger:", err)
		return 1
	}
	b, ok1 := l.Find(*before)
	a, ok2 := l.Find(*after)
	if !ok1 || !ok2 {
		fmt.Fprintf(os.Stderr, "epicledger: entry not found (before %q found=%v, after %q found=%v)\n", *before, ok1, *after, ok2)
		return 1
	}
	text := CompareMarkdown(b, a, Compare(b, a))
	fmt.Print(text)
	if *md != "" {
		if err := os.WriteFile(*md, []byte(text), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "epicledger:", err)
			return 1
		}
	}
	return 0
}
