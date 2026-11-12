package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, extraJS string) string {
	root := t.TempDir()
	write(t, root, "public/app.js", strings.Repeat("var a = 1;\n", 200)+extraJS)
	write(t, root, "public/index.html", "<html></html>")
	write(t, root, "go.mod", "module x\n\ngo 1.22\n\nrequire (\n\tgithub.com/a/b v1.0.0\n\tgithub.com/c/d v2.1.0 // indirect\n)\n\nrequire github.com/e/f v0.1.0\n")
	write(t, root, "docs/guides/user-guide.md", "# u")
	write(t, root, "docs/kb/a.md", "# a")
	write(t, root, "docs/operations/runbook.md", "# r")
	write(t, root, "cmd/one/main.go", "package main")
	write(t, root, "cmd/two/main.go", "package main")
	write(t, root, "internal/server/route_capabilities.go", "package server\nvar x = map[string]int{\n\t\"GET /api/a\": 1,\n\t\"POST /api/b\": 2,\n}\n")
	return root
}

func TestStaticMetrics(t *testing.T) {
	m := StaticMetrics(fixture(t, ""))
	want := map[string]float64{"static.public_files": 2, "deps.go_modules": 3, "support.guide_and_kb_pages": 2, "support.runbooks": 1, "support.cmd_tools": 2, "support.routes_with_permission": 2}
	for k, v := range want {
		if got := m[k]; got.Status != "measured" || got.Value != v {
			t.Errorf("%s = %+v, want %v", k, got, v)
		}
	}
	if m["static.public_gzip_bytes"].Value >= m["static.public_bytes"].Value {
		t.Error("repetitive text should gzip smaller")
	}
	if m["support.queue_categories"].Status != "not_measured" || m["support.queue_categories"].Note == "" {
		t.Error("an unmeasurable figure must be not_measured with a reason")
	}
}

func TestEmptyRootIsNotZero(t *testing.T) {
	m := StaticMetrics(t.TempDir())
	if m["static.public_bytes"].Status != "not_measured" || m["deps.go_modules"].Status != "not_measured" {
		t.Errorf("a missing folder must be not_measured, not 0: %+v %+v", m["static.public_bytes"], m["deps.go_modules"])
	}
}

func TestBudgetIngest(t *testing.T) {
	m, err := BudgetMetrics([]byte(`{"schema_version":1,"metrics":{"binary_bytes":{"value":100,"unit":"bytes","status":"pass"},"db_size":{"value":0,"unit":"bytes","status":"not_measured","note":"no database url"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if m["budget.binary_bytes"].Value != 100 || m["budget.db_size"].Status != "not_measured" {
		t.Errorf("%+v", m)
	}
	if _, err := BudgetMetrics([]byte("nope")); err == nil {
		t.Error("garbage must be refused")
	}
}

const journeyJSON = `{"kind":"journeylab.journeys","journeys":[
 {"id":"J1","status":"pass","summary":{"business_writes":10,"server_rss_kb_end":50000,"server_peak_rss_kb":60000,"heap_bytes_per_1k_tx":100,"index_bytes_per_1k_tx":10,"wal_bytes_per_1k_tx":1000,"audit_rows_per_1k_tx":5,"outbox_rows_per_1k_tx":2}},
 {"id":"J2","status":"pass","summary":{"business_writes":10,"server_rss_kb_end":52000,"server_peak_rss_kb":61000,"heap_bytes_per_1k_tx":300,"index_bytes_per_1k_tx":30,"wal_bytes_per_1k_tx":3000,"audit_rows_per_1k_tx":7,"outbox_rows_per_1k_tx":4}},
 {"id":"J3","status":"pass","summary":{"business_writes":0,"server_rss_kb_end":1,"server_peak_rss_kb":1}},
 {"id":"J7","status":"pass","notes":["backup: 701699 bytes","backup to verified restore: 3.5 s for 701699 bytes"],"summary":{"business_writes":0}},
 {"id":"J6","status":"not_driven","summary":{}}]}`

func TestJourneyIngest(t *testing.T) {
	m, err := JourneyMetrics([]byte(journeyJSON))
	if err != nil {
		t.Fatal(err)
	}
	if got := m["db.heap_bytes_per_1k_tx"].Value; got != 200 {
		t.Errorf("median of 100 and 300 = 200, got %v", got)
	}
	if m["memory.server_peak_rss_kb"].Value != 61000 || m["memory.server_rss_after_journeys_kb"].Value != 52000 {
		t.Errorf("rss %+v", m)
	}
	if m["backup.size_bytes"].Value != 701699 || m["backup.seconds_to_verified_restore"].Value != 3.5 {
		t.Errorf("backup %+v %+v", m["backup.size_bytes"], m["backup.seconds_to_verified_restore"])
	}
	if _, err := JourneyMetrics([]byte(`{"kind":"journeylab.soak"}`)); err == nil {
		t.Error("soak evidence is not journey evidence")
	}
}

func TestJourneyIngestWithoutDrill(t *testing.T) {
	m, _ := JourneyMetrics([]byte(`{"kind":"journeylab.journeys","journeys":[{"id":"J1","status":"pass","summary":{"business_writes":4,"server_peak_rss_kb":9,"heap_bytes_per_1k_tx":1}}]}`))
	if m["backup.size_bytes"].Status != "not_measured" {
		t.Error("no J7 means backup size is not measured")
	}
}

func TestSoakIngest(t *testing.T) {
	m, err := SoakMetrics([]byte(`{"kind":"journeylab.soak","duration_actual_seconds":1500,"server_rss_kb_start":40000,"server_rss_kb_peak":70000,"api_p95_ms_last_quarter":80,"disk_projection_24h":{"projected_growth_bytes":123456}}`))
	if err != nil {
		t.Fatal(err)
	}
	if m["db.soak_projected_24h_growth"].Value != 123456 || m["memory.soak_rss_peak_kb"].Value != 70000 {
		t.Errorf("%+v", m)
	}
	if _, err := SoakMetrics([]byte(journeyJSON)); err == nil {
		t.Error("journey evidence is not soak evidence")
	}
}

func TestLedgerRoundTripAndDuplicates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "ledger.json")
	l, err := LoadLedger(p)
	if err != nil || len(l.Entries) != 0 {
		t.Fatalf("a missing ledger is empty: %v %v", l, err)
	}
	e := Entry{ID: "a", Label: "first", Metrics: map[string]Metric{"x": measured(1, "n", "t")}}
	if err := l.Add(e, false); err != nil {
		t.Fatal(err)
	}
	if err := l.Add(e, false); err == nil {
		t.Error("a repeated id must be refused")
	}
	e.Label = "second"
	if err := l.Add(e, true); err != nil || l.Entries[0].Label != "second" {
		t.Errorf("replace failed: %v", err)
	}
	if err := l.Save(p); err != nil {
		t.Fatal(err)
	}
	back, err := LoadLedger(p)
	if err != nil || len(back.Entries) != 1 || back.Entries[0].Metrics["x"].Value != 1 {
		t.Errorf("round trip: %+v %v", back, err)
	}
	os.WriteFile(p, []byte("not json"), 0o644)
	if _, err := LoadLedger(p); err == nil {
		t.Error("a corrupt ledger must be an error, not silently replaced")
	}
}

func TestCompare(t *testing.T) {
	b := Entry{ID: "b", Metrics: map[string]Metric{
		"static.public_bytes": measured(1000, "bytes", ""),
		"deps.go_modules":     measured(10, "modules", ""),
		"db.wal":              notMeasured("bytes", "no db"),
		"only.before":         measured(1, "n", ""),
	}}
	a := Entry{ID: "a", Metrics: map[string]Metric{
		"static.public_bytes": measured(1100, "bytes", ""),
		"deps.go_modules":     measured(10, "modules", ""),
		"db.wal":              measured(5, "bytes", ""),
		"only.after":          measured(2, "n", ""),
	}}
	rows := map[string]Row{}
	for _, r := range Compare(b, a) {
		rows[r.Name] = r
	}
	if r := rows["static.public_bytes"]; r.Delta == nil || *r.Delta != 100 || r.Pct == nil || *r.Pct != 10 {
		t.Errorf("growth %+v", r)
	}
	if r := rows["deps.go_modules"]; r.Delta == nil || *r.Delta != 0 {
		t.Errorf("unchanged %+v", r)
	}
	if r := rows["db.wal"]; r.Delta != nil || r.Note == "" {
		t.Errorf("a not-measured side must give no delta: %+v", r)
	}
	if rows["only.before"].Note != "not in the after entry" || rows["only.after"].Note != "not in the before entry" {
		t.Errorf("%+v %+v", rows["only.before"], rows["only.after"])
	}
	md := CompareMarkdown(b, a, Compare(b, a))
	if !strings.Contains(md, "+10.0%") || !strings.Contains(md, "| db.wal |") {
		t.Errorf("markdown:\n%s", md)
	}
}

// The ledger must notice a real change in the tree: adding to public/ and a
// dependency moves the figures. This is the fail-first check for the tool.
func TestRecordSeesTreeChange(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), "l.json")
	before := fixture(t, "")
	after := fixture(t, strings.Repeat("var added = 'x';\n", 500))
	write(t, after, "go.mod", "module x\n\ngo 1.22\n\nrequire (\n\tgithub.com/a/b v1.0.0\n\tgithub.com/c/d v2.1.0 // indirect\n\tgithub.com/new/dep v1.0.0\n)\n\nrequire github.com/e/f v0.1.0\n")
	if rc := run([]string{"record", "-id", "before", "-label", "b", "-root", before, "-ledger", ledger, "-commit", "aaa"}); rc != 0 {
		t.Fatal("record before failed")
	}
	if rc := run([]string{"record", "-id", "after", "-label", "a", "-root", after, "-ledger", ledger, "-commit", "bbb"}); rc != 0 {
		t.Fatal("record after failed")
	}
	if rc := run([]string{"record", "-id", "after", "-label", "a", "-root", after, "-ledger", ledger}); rc == 0 {
		t.Error("a duplicate id must fail")
	}
	l, _ := LoadLedger(ledger)
	b, _ := l.Find("before")
	a, _ := l.Find("after")
	rows := map[string]Row{}
	for _, r := range Compare(b, a) {
		rows[r.Name] = r
	}
	if r := rows["static.public_bytes"]; r.Delta == nil || *r.Delta <= 0 {
		t.Errorf("a bigger public/ must show growth: %+v", r)
	}
	if r := rows["deps.go_modules"]; r.Delta == nil || *r.Delta != 1 {
		t.Errorf("a new dependency must show +1: %+v", r)
	}
	if rc := run([]string{"compare", "-before", "before", "-after", "after", "-ledger", ledger}); rc != 0 {
		t.Error("compare failed")
	}
	if rc := run([]string{"compare", "-before", "nope", "-after", "after", "-ledger", ledger}); rc == 0 {
		t.Error("an unknown id must fail")
	}
}

func TestSoakTooFewCyclesIsNotMeasured(t *testing.T) {
	m, err := SoakMetrics([]byte(`{"kind":"journeylab.soak","duration_actual_seconds":900,"api_p95_ms_last_quarter":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if m["api.soak_p95_ms_last_quarter"].Status != "not_measured" {
		t.Errorf("a 0 p95 means not computed, not 0 ms: %+v", m["api.soak_p95_ms_last_quarter"])
	}
}

func TestWALFigureCarriesItsCaveat(t *testing.T) {
	m, _ := JourneyMetrics([]byte(journeyJSON))
	if !strings.Contains(m["db.wal_bytes_per_1k_tx"].Note, "cluster-wide") {
		t.Error("the WAL figure must say it is cluster-wide")
	}
}
