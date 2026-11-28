package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The histogram reads a percentile at most 5% above the exact value, never below.
func TestHistPercentileWithinFivePercent(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var h Hist
	var vals []float64
	for i := 0; i < 20000; i++ {
		v := math.Exp(rng.Float64()*9) / 10 // 0.1 ms to ~800 ms, skewed like real latency
		vals = append(vals, v)
		h.Add(v)
	}
	for _, p := range []float64{50, 90, 95, 99, 99.9, 100} {
		exact := Percentile(vals, p)
		got := h.Pct(p)
		if got < exact-0.01 || got > exact*1.05+0.01 {
			t.Errorf("p%v: histogram %.3f, exact %.3f (want within +5%%, never low)", p, got, exact)
		}
	}
	if (&Hist{}).Pct(95) != 0 {
		t.Error("an empty histogram reads 0")
	}
}

// Two generators' histograms add up to exactly the histogram of all the values.
func TestHistMergeEqualsWhole(t *testing.T) {
	var a, b, whole Hist
	for i := 1; i <= 5000; i++ {
		v := float64(i%977) + 0.5
		whole.Add(v)
		if i%2 == 0 {
			a.Add(v)
		} else {
			b.Add(v)
		}
	}
	var m Hist
	m.Merge(&a)
	m.Merge(&b)
	m.Merge(nil)
	if m.N != whole.N || m.MaxMS != whole.MaxMS || math.Abs(m.SumMS-whole.SumMS) > 1e-6 {
		t.Fatalf("merged n/max/sum %d/%v/%v, whole %d/%v/%v", m.N, m.MaxMS, m.SumMS, whole.N, whole.MaxMS, whole.SumMS)
	}
	for _, p := range []float64{50, 95, 99} {
		if m.Pct(p) != whole.Pct(p) {
			t.Errorf("p%v merged %v, whole %v", p, m.Pct(p), whole.Pct(p))
		}
	}
	// A histogram survives the JSON a generator writes and the merge reads.
	raw, _ := json.Marshal(&m)
	var back Hist
	if err := json.Unmarshal(raw, &back); err != nil || back.Pct(95) != m.Pct(95) || back.N != m.N {
		t.Fatalf("JSON round trip: %v, p95 %v vs %v", err, back.Pct(95), m.Pct(95))
	}
}

// Each load user signs in from its own address, which the server must not skip
// as a trusted proxy hop (private, loopback, link-local).
func TestLoadXFFDistinctAndNotTrusted(t *testing.T) {
	_, bench, _ := net.ParseCIDR("198.18.0.0/15")
	seen := map[string]bool{}
	for i := 0; i < 100000; i++ {
		s := loadXFF(i)
		ip := net.ParseIP(s)
		if ip == nil || !bench.Contains(ip) {
			t.Fatalf("user %d: %q is not in 198.18.0.0/15", i, s)
		}
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			t.Fatalf("user %d: %q would be skipped as a proxy hop", i, s)
		}
		if seen[s] {
			t.Fatalf("user %d: %q repeats", i, s)
		}
		seen[s] = true
	}
}

func TestCoresFor(t *testing.T) {
	if got := coresFor(500, 4); got != 3.33 {
		t.Fatalf("coresFor(500, 4) = %v, want 3.33", got)
	}
	if coresFor(0, 4) != 0 || coresFor(500, 0) != 0 {
		t.Fatal("no rate or no cost needs no cores")
	}
}

// The next page is due a pause after the last one was due, whatever the reply
// took, so the count of pages in a window is fixed by the schedule alone.
func TestNextDueIsOpenLoop(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	rng := rand.New(rand.NewSource(1))
	due, n := start, 0
	for {
		next := nextDue(due, 10*time.Second, 30*time.Second, rng)
		if d := next.Sub(due); d < 10*time.Second || d > 30*time.Second {
			t.Fatalf("pause %v outside 10-30 s", d)
		}
		due = next
		if due.After(start.Add(10 * time.Minute)) {
			break
		}
		n++
	}
	if n < 20 || n > 60 {
		t.Fatalf("%d pages in 10 minutes at one every 10-30 s", n)
	}
	// A fixed pause gives an exact count.
	due, n = start, 0
	for due = nextDue(due, 10*time.Second, 10*time.Second, rng); !due.After(start.Add(time.Minute)); due = nextDue(due, 10*time.Second, 10*time.Second, rng) {
		n++
	}
	if n != 6 {
		t.Fatalf("one page every 10 s for a minute: %d pages, want 6", n)
	}
	// The weighted page pick reaches every page and never a zero-weight one.
	got := map[string]int{}
	for i := 0; i < 5000; i++ {
		got[pickPage(cashierPages, rng).name]++
	}
	for _, p := range cashierPages {
		if got[p.name] == 0 {
			t.Errorf("page %q never picked", p.name)
		}
	}
}

func loadResultWith(okReads, failedReads int64, readMS, writeMS float64) *LoadResult {
	r := &LoadResult{Users: 10, SteadyS: 100, SteadyReqs: 1000, Pages: map[string]*PageStat{}}
	rd := &PageStat{Kind: "read", OK: okReads, Failed: failedReads}
	for i := int64(0); i < okReads+failedReads; i++ {
		rd.Due.Add(readMS)
	}
	wr := &PageStat{Kind: "write", OK: 100}
	for i := 0; i < 100; i++ {
		wr.Due.Add(writeMS)
	}
	r.Pages["item search"], r.Pages["sale"] = rd, wr
	r.Recon = []ReconCheck{{Name: "stock", Pass: true}}
	return r
}

// The verdict follows the 297.5 budgets.
func TestLoadJudge(t *testing.T) {
	verdict := func(r *LoadResult) string { r.judge(); return r.Verdict }
	if v := verdict(loadResultWith(1000, 0, 120, 300)); v != "pass" {
		t.Fatalf("all in budget: %s", v)
	}
	if v := verdict(loadResultWith(990, 10, 120, 300)); v != "fail" {
		t.Fatalf("10 of 1,100 pages failed (0.9%%): %s", v)
	}
	if v := verdict(loadResultWith(1000, 1, 120, 300)); v != "pass" {
		t.Fatalf("1 of 1,101 pages failed (0.09%%): %s", v)
	}
	if v := verdict(loadResultWith(1000, 0, 700, 300)); v != "fail" {
		t.Fatal("reads p95 700 ms must fail")
	}
	if v := verdict(loadResultWith(1000, 0, 120, 1500)); v != "fail" {
		t.Fatal("writes p95 1.5 s must fail")
	}
	r := loadResultWith(1000, 0, 120, 300)
	r.Recon = append(r.Recon, ReconCheck{Name: "trial balance", Pass: false})
	if r.judge(); r.Verdict != "fail" {
		t.Fatal("books that do not tie must fail")
	}
	r = loadResultWith(1000, 0, 120, 300)
	r.Generator.PeakPct = 85
	if r.judge(); r.Verdict != "invalid" {
		t.Fatalf("a generator at 85%% CPU makes the run invalid, got %s", r.Verdict)
	}
	empty := &LoadResult{Pages: map[string]*PageStat{}}
	if empty.judge(); empty.Verdict != "fail" {
		t.Fatal("a run with no pages must fail")
	}
}

// Merging two generators adds the pages and takes the shared server's figures once.
func TestMergeLoad(t *testing.T) {
	a := loadResultWith(500, 0, 100, 200)
	a.Generators, a.Users = []string{"g1"}, 100
	a.Server = ProcDelta{Available: true, CPUMS: 4000}
	a.DBSteady = DBDelta{DBTimeMS: 1500}
	b := loadResultWith(500, 0, 300, 400)
	b.Generators, b.Users = []string{"g2"}, 100
	b.Server = ProcDelta{Available: true, CPUMS: 4200}
	b.DBSteady = DBDelta{DBTimeMS: 1400}
	m := mergeLoad([]*LoadResult{a, b})
	if m.Users != 200 || len(m.Generators) != 2 || m.Pages["item search"].OK != 1000 || m.SteadyReqs != 2000 {
		t.Fatalf("merged users %d gens %v reads %d reqs %d", m.Users, m.Generators, m.Pages["item search"].OK, m.SteadyReqs)
	}
	if m.Server.CPUMS != 4200 || m.DBSteady.DBTimeMS != 1500 {
		t.Fatalf("shared server figures: cpu %v db %v", m.Server.CPUMS, m.DBSteady.DBTimeMS)
	}
	if m.Sizing.ServerCPUMSReq != 2.1 || m.Sizing.RPS != 20 {
		t.Fatalf("sizing %+v", m.Sizing)
	}
	if m.Verdict != "pass" || m.Pages["item search"].Due.N != 1000 {
		t.Fatalf("verdict %s %v", m.Verdict, m.Reasons)
	}
}

// A load call sends the tenant and the user's own address, is never retried on
// a 429 or 5xx, and signs in again once after a 401.
func TestLoadCallHeadersAndRelogin(t *testing.T) {
	logins := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") != "198.18.0.1" || r.Header.Get("X-Tenant-ID") != "t1" {
			w.WriteHeader(400)
			return
		}
		switch r.URL.Path {
		case "/api/v1/login":
			logins++
			_, _ = w.Write([]byte(`{"token":"fresh"}`))
		case "/busy":
			w.WriteHeader(503)
		default:
			if r.Header.Get("Authorization") != "Bearer fresh" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()
	L := &loadRun{base: srv.URL, tenant: "t1", xffOn: true, hc: srv.Client(), res: &LoadResult{Pages: map[string]*PageStat{}}}
	u := &loadUser{name: "jl-load-00001", xff: loadXFF(0), token: "stale"}
	a := L.call(u, "GET", "/api/v1/doc/Item", nil, nil)
	if a.status != 200 || a.js["ok"] != true || u.token != "fresh" || L.relogins.Load() != 1 || logins != 1 {
		t.Fatalf("relogin: status %d token %q relogins %d logins %d", a.status, u.token, L.relogins.Load(), logins)
	}
	before := L.reqs.Load()
	if why := failed(L.call(u, "GET", "/busy", nil, nil)); why != "503" {
		t.Fatalf("a 503 is a failure named by its code, got %q", why)
	}
	if L.reqs.Load()-before != 1 {
		t.Fatalf("a 503 was retried: %d requests", L.reqs.Load()-before)
	}
	if failed(answer{status: 304}, 304) != "" || failed(answer{status: 304}) != "304" {
		t.Fatal("304 is a success only where the page allows it")
	}
}
