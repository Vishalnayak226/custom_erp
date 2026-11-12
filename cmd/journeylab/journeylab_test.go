package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestPercentileNearestRank(t *testing.T) {
	v := []float64{50, 10, 40, 20, 30}
	cases := []struct{ p, want float64 }{{0, 10}, {20, 10}, {50, 30}, {95, 50}, {100, 50}}
	for _, c := range cases {
		if got := Percentile(v, c.p); got != c.want {
			t.Errorf("p%v = %v, want %v", c.p, got, c.want)
		}
	}
	if Percentile(nil, 50) != 0 {
		t.Error("empty input must give 0")
	}
	if v[0] != 50 {
		t.Error("input must not be reordered")
	}
}

func TestSummarizeCalls(t *testing.T) {
	calls := []Call{
		{Status: 200, MS: 10, ReqBytes: 5, RespBytes: 100, CountsAsTx: true},
		{Status: 200, MS: 20, RespBytes: 50},
		{Status: 409, MS: 30, Retries: 2, Expected: true},
		{Status: 500, MS: 40},
		{Err: "boom", MS: 50},
	}
	s := SummarizeCalls(calls)
	if s.Calls != 5 || s.Errors != 2 || s.Retries != 2 || s.Writes != 1 || s.RespBytes != 150 || s.ReqBytes != 5 {
		t.Fatalf("unexpected stats %+v", s)
	}
	if s.P50MS != 30 || s.P95MS != 50 || s.MaxMS != 50 {
		t.Fatalf("unexpected percentiles %+v", s)
	}
}

func TestDeltaCountersAndBackwards(t *testing.T) {
	a := DBSnap{Source: "pg_stat_statements", XactCommit: 100, StmtCalls: 1000, StmtTimeMS: 500, HeapBytes: 100, IndexBytes: 50, WALBytes: 1000, AuditRows: 10, ActiveTimeMS: 5}
	b := DBSnap{Source: "pg_stat_statements", XactCommit: 130, StmtCalls: 1090, StmtTimeMS: 560.5, HeapBytes: 300, IndexBytes: 90, WALBytes: 4000, AuditRows: 14, ActiveTimeMS: 9}
	d := Delta(a, b)
	if d.Transactions != 30 || d.QueryCount != 90 || !d.QueryCountOK || d.DBTimeMS != 60.5 || d.HeapGrowth != 200 || d.IndexGrowth != 40 || d.WALBytes != 3000 || d.AuditRows != 4 {
		t.Fatalf("unexpected delta %+v", d)
	}
	// a restart or stats reset: counters go backwards; the new reading stands, never a negative
	r := Delta(b, DBSnap{Source: "pg_stat_statements", XactCommit: 7, StmtCalls: 12, StmtTimeMS: 3, WALBytes: 99})
	if r.Transactions != 7 || r.QueryCount != 12 || r.DBTimeMS != 3 || r.WALBytes != 99 {
		t.Fatalf("backwards counters not handled: %+v", r)
	}
	// without pg_stat_statements the query count is unavailable and active time stands in
	f := Delta(DBSnap{Source: "pg_stat_database", ActiveTimeMS: 5}, DBSnap{Source: "pg_stat_database", ActiveTimeMS: 12})
	if f.QueryCountOK || f.QueryCount != 0 || f.DBTimeMS != 7 {
		t.Fatalf("fallback source wrong: %+v", f)
	}
	sum := AddDelta(d, d)
	if sum.Transactions != 60 || sum.HeapGrowth != 400 {
		t.Fatalf("AddDelta wrong: %+v", sum)
	}
}

func TestPer1kAndProjection(t *testing.T) {
	if Per1k(5000, 10) != 500000 || Per1k(5, 0) != 0 {
		t.Fatal("Per1k")
	}
	p := ProjectDisk(1800, 10_000_000, 24) // 10 MB in 30 min
	if p.BytesPerHour != 20_000_000 || p.ProjectedBytes != 480_000_000 {
		t.Fatalf("projection %+v", p)
	}
	if z := ProjectDisk(0, 100, 24); z.ProjectedBytes != 0 {
		t.Fatal("no window must project nothing")
	}
	if !strings.Contains(p.Basis, "linear") {
		t.Fatal("projection must say it is linear")
	}
}

func TestProcDiffHandlesReplacedProcess(t *testing.T) {
	d := ProcDiff(ProcSnap{OK: true, CPUMS: 1000, RSSKB: 10}, ProcSnap{OK: true, CPUMS: 40, RSSKB: 20, PeakKB: 30})
	if !d.Available || d.CPUMS != 40 || d.RSSKB != 20 || d.PeakRSSKB != 30 {
		t.Fatalf("%+v", d)
	}
	if ProcDiff(ProcSnap{}, ProcSnap{OK: true}).Available {
		t.Fatal("an unreadable first reading must not report a figure")
	}
}

func TestGuards(t *testing.T) {
	if err := CheckBaseURL("http://localhost:8730", false); err != nil {
		t.Fatal(err)
	}
	if err := CheckBaseURL("http://127.0.0.1:1", false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"https://erp.example.com", "http://10.0.0.5:8080", "http://[2001:db8::1]/"} {
		err := CheckBaseURL(bad, false)
		if err == nil || !strings.Contains(err.Error(), "refused") {
			t.Errorf("%s must be refused, got %v", bad, err)
		}
		if CheckBaseURL(bad, true) != nil {
			t.Errorf("%s must pass with allow-remote", bad)
		}
	}
	if CheckScratchDB("postgres://u@localhost:5/erp_scratch_1", "", false) != nil {
		t.Error("a scratch name must pass")
	}
	if err := CheckScratchDB("postgres://u@localhost:5/custom_erp", "", false); err == nil || !strings.Contains(err.Error(), "-allow-db custom_erp") {
		t.Errorf("a non-scratch name must be refused and say how to allow it, got %v", err)
	}
	if CheckScratchDB("postgres://u@localhost:5/custom_erp", "custom_erp", false) != nil {
		t.Error("-allow-db naming the database must pass")
	}
	if CheckScratchDB("postgres://u@localhost:5/custom_erp", "other", false) == nil {
		t.Error("-allow-db naming a different database must not pass")
	}
	if CheckScratchDB("postgres://u@db.example.com:5/erp_scratch", "", false) == nil {
		t.Error("a remote database host must be refused")
	}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if CheckEnvironment(env(map[string]string{"ENV": "production", "ERP_DISABLE_EXTERNAL_SIDE_EFFECTS": "1"})) == nil {
		t.Error("production must be refused")
	}
	if CheckEnvironment(env(map[string]string{"APP_ENV": "Production", "ERP_DISABLE_EXTERNAL_SIDE_EFFECTS": "1"})) == nil {
		t.Error("APP_ENV production must be refused")
	}
	if CheckEnvironment(env(map[string]string{})) == nil {
		t.Error("live side effects must be refused")
	}
	if err := CheckEnvironment(env(map[string]string{"ERP_DISABLE_EXTERNAL_SIDE_EFFECTS": "1"})); err != nil {
		t.Error(err)
	}
}

func TestGSTINChecksum(t *testing.T) {
	if got := gstinWithCheck("29AABCA0001A1Z"); got != "29AABCA0001A1ZS" {
		t.Fatalf("got %s, the known valid value is 29AABCA0001A1ZS", got)
	}
}

func TestVerdictAndOverall(t *testing.T) {
	ok := []ReconCheck{{Pass: true}, {Pass: false, Warn: true}}
	if Verdict(0, false, ok) != "pass" {
		t.Error("warnings must not turn it red")
	}
	if Verdict(0, false, append(ok, ReconCheck{Pass: false})) != "fail" {
		t.Error("a failed check must turn it red")
	}
	if Verdict(1, false, ok) != "fail" || Verdict(0, true, ok) != "fail" {
		t.Error("a step error or failed step must turn it red")
	}
	if Overall([]JourneyRecord{{Status: "pass"}, {Status: "not_driven"}}) != "pass" ||
		Overall([]JourneyRecord{{Status: "pass"}, {Status: "fail"}}) != "fail" ||
		Overall([]JourneyRecord{{Status: "not_driven"}}) != "not_driven" {
		t.Error("overall roll-up")
	}
}

func TestQuartP95(t *testing.T) {
	var cs []CycleResult
	for i := 0; i < 16; i++ {
		cs = append(cs, CycleResult{Status: "pass", P95MS: float64(100 + i*10)})
	}
	a, b := quartP95(cs)
	if a >= b || a < 100 || b > 250 {
		t.Fatalf("quarters %v %v", a, b)
	}
	if x, y := quartP95(cs[:3]); x != 0 || y != 0 {
		t.Error("too few cycles must give no figure")
	}
}

func sampleEvidence(status string) Evidence {
	j := JourneyRecord{ID: "J9", Title: "Sample", Status: status, Roles: []string{"jl-x"}, WallMS: 12,
		Steps:   []StepRecord{{Name: "s", Role: "jl-x", Status: []int{200}, HTTP: CallStats{Calls: 1}}},
		Summary: JourneySum{HTTP: CallStats{Calls: 1}},
		Recon:   []ReconCheck{{Kind: "gl", Name: "ledger total debits = credits", Pass: status == "pass"}}}
	return NewEvidence("http://localhost:1", "erp_scratch", "pg_stat_database", []JourneyRecord{j}, LoadBrowser(""))
}

func TestEvidenceJSONShape(t *testing.T) {
	dir := t.TempDir()
	bf := filepath.Join(dir, "browser.json")
	os.WriteFile(bf, []byte(`{"a11y":{"violations":0},"help":{"tasks":3}}`), 0o644)
	ev := NewEvidence("http://localhost:1", "erp_scratch", "pg_stat_database", []JourneyRecord{sampleEvidence("pass").Journeys[0]}, LoadBrowser(bf))
	jp, mp, err := WriteEvidence(dir, ev)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(jp)
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema_version", "kind", "generated_at", "base_url", "database", "db_stats_source", "overall", "journeys", "browser", "limits"} {
		if _, ok := m[k]; !ok {
			t.Errorf("evidence is missing %q", k)
		}
	}
	br := m["browser"].(map[string]interface{})
	if br["source"] != "from_browser_suite" || br["status"] != "filled" || br["data"] == nil {
		t.Errorf("browser section %v", br)
	}
	step := m["journeys"].([]interface{})[0].(map[string]interface{})["steps"].([]interface{})[0].(map[string]interface{})
	for _, k := range []string{"role", "wall_ms", "http", "http_statuses", "db", "server_process"} {
		if _, ok := step[k]; !ok {
			t.Errorf("step is missing %q", k)
		}
	}
	md, _ := os.ReadFile(mp)
	if !strings.Contains(string(md), "from_browser_suite") || !strings.Contains(string(md), "# Journey evidence: PASS") {
		t.Errorf("markdown summary:\n%s", md)
	}
	// without a file the section says so and is not marked filled
	if b := LoadBrowser(""); b.Status != "not_provided" || b.Source != "from_browser_suite" {
		t.Errorf("%+v", b)
	}
	if b := LoadBrowser(filepath.Join(dir, "nope.json")); b.Status != "not_provided" {
		t.Errorf("%+v", b)
	}
	// pg_stat_database evidence says it cannot count queries
	if !strings.Contains(strings.Join(ev.Limits, " "), "pg_stat_database") {
		t.Error("limits must name the weaker statistics source")
	}
}

func TestRedEvidenceIsRed(t *testing.T) {
	red := sampleEvidence("fail")
	if red.Overall != "fail" || !strings.Contains(Markdown(red), "# Journey evidence: FAIL") {
		t.Fatal("a failed journey must make the evidence red")
	}
}

func TestClientRetriesAndRelogin(t *testing.T) {
	var hits, logins atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/login"):
			logins.Add(1)
			fmt.Fprint(w, `{"token":"fresh"}`)
		case r.URL.Path == "/flaky":
			if hits.Add(1) == 1 {
				w.WriteHeader(503)
				return
			}
			fmt.Fprint(w, `{"ok":true}`)
		case r.URL.Path == "/expired":
			if r.Header.Get("Authorization") != "Bearer fresh" {
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"ok":true}`)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	c.Sess["u"] = &Session{Name: "u", Username: "u", Password: "p", Token: "old"}
	r, err := c.Do("u", "GET", "/flaky", nil, CallOpts{})
	if err != nil || r.Status != 200 {
		t.Fatalf("flaky: %v %+v", err, r)
	}
	r, err = c.Do("u", "POST", "/expired", map[string]int{"a": 1}, CallOpts{})
	if err != nil || r.Status != 200 || logins.Load() != 1 {
		t.Fatalf("expired token must trigger one sign-in and succeed: %v %+v logins=%d", err, r, logins.Load())
	}
	calls := c.Take()
	if len(calls) != 2 || calls[0].Retries != 1 || calls[1].Retries != 1 || !calls[1].CountsAsTx || calls[0].CountsAsTx {
		t.Fatalf("recorded calls %+v", calls)
	}
	if len(c.Take()) != 0 {
		t.Fatal("Take must clear")
	}
}

func TestPGTargetAndClusterGuards(t *testing.T) {
	p, err := parsePG("postgres://bob:pass@localhost:5490/erp_x?sslmode=disable")
	if err != nil || p.User != "bob" || p.Pass != "pass" || p.Port != "5490" || p.DB != "erp_x" {
		t.Fatalf("%+v %v", p, err)
	}
	if !strings.Contains(p.URL("other"), "/other") {
		t.Error("URL for another database")
	}
	if err := (Cluster{Dir: t.TempDir(), Port: 5490}).Up(); err == nil || !strings.Contains(err.Error(), "shared") {
		t.Errorf("a shared port must be refused, got %v", err)
	}
	if err := (Cluster{Dir: t.TempDir(), Port: 5600}).Stop(); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("a folder this tool did not create must not be managed, got %v", err)
	}
	if p, err := FreePort(5600); err != nil || p < 5600 {
		t.Errorf("FreePort %d %v", p, err)
	}
}

// The fail-first proof against a real database: an unbalanced ledger row turns
// the reconciliation red, and removing it turns it green. It uses its own
// throwaway schema, so no shared table is touched.
func TestInjectedMismatchTurnsReconciliationRed(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	d, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	sch := fmt.Sprintf("jl_unit_%d", os.Getpid())
	for _, q := range []string{
		fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, sch),
		fmt.Sprintf(`CREATE SCHEMA %s`, sch),
		fmt.Sprintf(`CREATE TABLE %s.gl_postings (account_code text, debit bigint DEFAULT 0, credit bigint DEFAULT 0, document_type text, document_id text)`, sch),
		fmt.Sprintf(`CREATE TABLE %s.inventory_availability (sku text, location_code text, on_hand numeric)`, sch),
		fmt.Sprintf(`INSERT INTO %s.gl_postings VALUES ('1200', 500, 0, 'T', 'a'), ('4000', 0, 500, 'T', 'a')`, sch),
		fmt.Sprintf(`INSERT INTO %s.inventory_availability VALUES ('S', 'L', 10)`, sch),
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	defer d.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, sch))
	r := &Runner{DB: d, Cfg: Config{Schema: sch, Quick: true}}
	j := r.NewJourney("T", "t")
	j.StockBase("S", "L")
	j.Expect("S", "L", 0, "unchanged")
	green := j.Reconcile()
	if !ReconPassed(green) {
		t.Fatalf("a balanced ledger must reconcile: %+v", green)
	}
	if err := r.InjectFault(); err != nil {
		t.Fatal(err)
	}
	red := j.Reconcile()
	if ReconPassed(red) {
		t.Fatalf("an injected unbalanced row must turn the reconciliation red: %+v", red)
	}
	rec := j.Finish(red)
	if rec.Status != "fail" || rec.Reason == "" {
		t.Fatalf("journey must be red with a reason: %+v", rec)
	}
	// a stock promise that does not hold is caught too
	r.RemoveInjected()
	if !ReconPassed(j.Reconcile()) {
		t.Fatal("removing the injected row must turn it green again")
	}
	j.Expect("S", "L", 3, "promised but never moved")
	if ReconPassed(j.Reconcile()) {
		t.Fatal("an unmet stock expectation must turn it red")
	}
}

// 47.17.2 / 47.17.3: only a complete, honest run is release evidence, and a
// red run is recorded as a fail rather than dropped.
func TestGateEvidenceFromAJourneyRun(t *testing.T) {
	mk := func(status ...string) Evidence {
		var js []JourneyRecord
		for i, s := range status {
			js = append(js, JourneyRecord{ID: journeyIDs[i], Status: s})
		}
		return Evidence{Journeys: js, Overall: Overall(js), Host: "h"}
	}
	now := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	all := []string{"pass", "pass", "pass", "pass", "pass", "pass", "pass"}

	ge, ok, _ := GateEvidence(mk(all...), false, "abc", "e.json", now)
	if !ok || ge.Kind != "journey" || ge.Result != "pass" || ge.Date != "2026-11-12" || ge.Commit != "abc" {
		t.Fatalf("complete passing run: %+v ok=%v", ge, ok)
	}
	red := append([]string{"fail"}, all[1:]...)
	if ge, ok, _ := GateEvidence(mk(red...), false, "abc", "e.json", now); !ok || ge.Result != "fail" || !strings.Contains(ge.Note, "J1=fail") {
		t.Fatalf("a failing journey must be recorded as fail: %+v ok=%v", ge, ok)
	}
	blocked := append(append([]string{}, all[:6]...), "blocked")
	if ge, ok, _ := GateEvidence(mk(blocked...), false, "abc", "e.json", now); !ok || ge.Result != "fail" {
		t.Fatalf("a blocked journey is not a pass: %+v", ge)
	}
	if _, ok, why := GateEvidence(mk(all[:3]...), false, "abc", "e.json", now); ok || !strings.Contains(why, "J4") {
		t.Fatalf("a subset run must not count: ok=%v why=%q", ok, why)
	}
	if _, ok, _ := GateEvidence(mk(all...), true, "abc", "e.json", now); ok {
		t.Fatal("the fail-first run must not count")
	}
}
