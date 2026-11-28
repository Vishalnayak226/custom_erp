package main

// load.go - the load mode (297.5). N named users - four in five cashiers, the
// rest store managers - sign in through the real login limiter, each from its
// own address, then each does one page every few seconds on a fixed schedule.
// A slow reply never moves the next page's due time, and every page is timed
// from when it was due, so a queue on the server shows up as latency instead
// of as fewer pages (coordinated omission). Page times go into log-bucketed
// histograms that add up across generators. The run ends with the books check
// and the CPU milliseconds each request cost the server and the database,
// which is what sizes the machines for the full run.

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---- histogram ----

const histBase, histRatio = 0.1, 1.05 // ms; each bucket is 5% wider than the one before

// Hist is a log-bucketed latency histogram. Histograms from several generators
// add up bucket by bucket, so a multi-machine run reads its percentiles from
// the sum (an average of p95s is not a p95).
type Hist struct {
	Counts map[int]int64 `json:"counts"`
	N      int64         `json:"n"`
	SumMS  float64       `json:"sum_ms"`
	MaxMS  float64       `json:"max_ms"`
}

func histIndex(ms float64) int {
	if ms <= histBase {
		return 0
	}
	return int(math.Log(ms/histBase)/math.Log(histRatio)) + 1
}

func histUpper(i int) float64 { return histBase * math.Pow(histRatio, float64(i)) }

// Add records one value in milliseconds.
func (h *Hist) Add(ms float64) {
	if h.Counts == nil {
		h.Counts = map[int]int64{}
	}
	h.Counts[histIndex(ms)]++
	h.N++
	h.SumMS += ms
	if ms > h.MaxMS {
		h.MaxMS = ms
	}
}

// Merge adds another histogram into this one.
func (h *Hist) Merge(o *Hist) {
	if o == nil || o.N == 0 {
		return
	}
	if h.Counts == nil {
		h.Counts = map[int]int64{}
	}
	for i, n := range o.Counts {
		h.Counts[i] += n
	}
	h.N += o.N
	h.SumMS += o.SumMS
	if o.MaxMS > h.MaxMS {
		h.MaxMS = o.MaxMS
	}
}

// Pct is the nearest-rank percentile, read as the upper edge of its bucket: at
// most 5% high, never low, and never above the largest value seen.
func (h *Hist) Pct(p float64) float64 {
	if h == nil || h.N == 0 {
		return 0
	}
	rank := int64(math.Ceil(p / 100 * float64(h.N)))
	if rank < 1 {
		rank = 1
	}
	idx := make([]int, 0, len(h.Counts))
	for i := range h.Counts {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	var seen int64
	for _, i := range idx {
		seen += h.Counts[i]
		if seen >= rank {
			return round2(math.Min(histUpper(i), h.MaxMS))
		}
	}
	return round2(h.MaxMS)
}

// ---- results ----

// PageStat is one kind of page: how often it ran and failed, why, and how long
// it took from when it was due (Due, the figure the budgets judge) and from
// when it started (Svc).
type PageStat struct {
	Kind   string           `json:"kind"` // read or write
	OK     int64            `json:"ok"`
	Failed int64            `json:"failed"`
	Why    map[string]int64 `json:"why,omitempty"` // status code, timeout, no answer, check
	Due    Hist             `json:"due"`
	Svc    Hist             `json:"service"`
}

// GenCPU is the generator's own CPU, as a share of all this machine's cores.
type GenCPU struct {
	Cores   int     `json:"cores"`
	AvgPct  float64 `json:"avg_pct"`
	PeakPct float64 `json:"peak_pct"` // the busiest 5-second reading
}

// SizeAt is the cores a target rate needs, at 60% busy.
type SizeAt struct {
	Label       string  `json:"label"`
	RPS         float64 `json:"rps"`
	ServerCores float64 `json:"server_cores"`
	DBCores     float64 `json:"db_cores"`
}

// Sizing turns the steady part of the run into cost per request.
type Sizing struct {
	RPS            float64  `json:"rps"` // steady requests a second in this run
	RPSPerUser     float64  `json:"rps_per_user"`
	ServerCPUMSReq float64  `json:"server_cpu_ms_per_request"`
	DBMSReq        float64  `json:"db_ms_per_request"`
	SignInCPUMS    float64  `json:"server_cpu_ms_per_sign_in"` // upper bound: the sign-in window's CPU over its sign-ins
	For            []SizeAt `json:"for"`
	Note           string   `json:"note,omitempty"`
}

// LoadResult is one load run, or several generators' runs merged.
type LoadResult struct {
	Generators []string             `json:"generators"`
	Base       string               `json:"base_url"`
	DB         string               `json:"database"`
	Tenant     string               `json:"tenant"`
	Users      int                  `json:"users"`
	Cashiers   int                  `json:"cashiers"`
	Managers   int                  `json:"managers"`
	Started    string               `json:"started"`
	RampS      float64              `json:"sign_in_window_s"`
	SteadyS    float64              `json:"steady_s"`
	EveryS     [2]float64           `json:"every_s"` // a user's pause between pages, shortest and longest
	Pages      map[string]*PageStat `json:"pages"`
	Requests   int64                `json:"requests"`
	SteadyReqs int64                `json:"steady_requests"` // after the last sign-in
	Late       int64                `json:"late_starts"`     // pages that began more than 1 s after they were due
	MaxLateMS  float64              `json:"max_late_ms"`
	Relogins   int64                `json:"relogins"`      // a 401 mid-run: the user signed in again, inside the page's time
	Unknown    int64                `json:"unknown_sales"` // checkouts that got no answer: may or may not have posted
	Sold       int64                `json:"units_sold"`
	Retries    int64                `json:"retries"` // always 0: a load call is never retried, a 429 or 5xx is a failure
	ServerAll  ProcDelta            `json:"server_all"`
	Server     ProcDelta            `json:"server_steady"`
	SignInCPU  float64              `json:"server_sign_in_window_cpu_ms"`
	DBSteady   DBDelta              `json:"db_steady"`
	Generator  GenCPU               `json:"generator_cpu"`
	Recon      []ReconCheck         `json:"reconciliation"`
	Sizing     Sizing               `json:"sizing"`
	Verdict    string               `json:"verdict"` // pass, fail, or invalid (the generator was the bottleneck)
	Reasons    []string             `json:"reasons,omitempty"`
}

// Budgets (297.5): under 0.5% failed pages, reads p95 under 500 ms, writes
// under 1 s, the books tie, and the generator under 70% CPU.
const (
	loadFailBudgetPct = 0.5
	loadReadP95MS     = 500
	loadWriteP95MS    = 1000
	loadGenCPUPct     = 70
	loadTargetBusy    = 0.6 // size the machine to run at 60% at the target rate
	loadFullUsers     = 10000
)

// kindHist merges the due-time histograms of every page of one kind.
func (res *LoadResult) kindHist(kind string) *Hist {
	h := &Hist{}
	for _, p := range res.Pages {
		if p.Kind == kind {
			h.Merge(&p.Due)
		}
	}
	return h
}

func (res *LoadResult) totals() (pages, failed int64) {
	for _, p := range res.Pages {
		pages += p.OK + p.Failed
		failed += p.Failed
	}
	return
}

// coresFor is the cores a rate needs when each request costs ms of CPU.
func coresFor(rps, ms float64) float64 { return round2(rps * ms / 1000 / loadTargetBusy) }

// size fills the sizing from the steady window's figures.
func (res *LoadResult) size() {
	s := Sizing{}
	if res.SteadyS > 0 {
		s.RPS = round2(float64(res.SteadyReqs) / res.SteadyS)
	}
	if res.Users > 0 {
		s.RPSPerUser = math.Round(s.RPS/float64(res.Users)*10000) / 10000
	}
	if res.SteadyReqs > 0 {
		s.ServerCPUMSReq = round2(res.Server.CPUMS / float64(res.SteadyReqs))
		s.DBMSReq = round2(res.DBSteady.DBTimeMS / float64(res.SteadyReqs))
	}
	if res.Users > 0 {
		s.SignInCPUMS = round2(res.SignInCPU / float64(res.Users))
	}
	if !res.Server.Available {
		s.Note = "server CPU not measured: pass -server-pid when the server runs on this machine"
	}
	for _, t := range []struct {
		label string
		rps   float64
	}{
		{"500 requests/s", 500},
		{"1,000 requests/s", 1000},
		{fmt.Sprintf("%d users at this run's pace", loadFullUsers), s.RPSPerUser * loadFullUsers},
	} {
		s.For = append(s.For, SizeAt{Label: t.label, RPS: round2(t.rps), ServerCores: coresFor(t.rps, s.ServerCPUMSReq), DBCores: coresFor(t.rps, s.DBMSReq)})
	}
	res.Sizing = s
}

// judge sets the verdict against the budgets.
func (res *LoadResult) judge() {
	res.Reasons = nil
	pages, failed := res.totals()
	if pages == 0 {
		res.Reasons = append(res.Reasons, "no pages ran")
	} else if pct := float64(failed) * 100 / float64(pages); pct >= loadFailBudgetPct {
		res.Reasons = append(res.Reasons, fmt.Sprintf("%.2f%% of pages failed (budget under %.1f%%)", pct, loadFailBudgetPct))
	}
	if p := res.kindHist("read").Pct(95); p > loadReadP95MS {
		res.Reasons = append(res.Reasons, fmt.Sprintf("reads p95 %.0f ms (budget %d ms)", p, loadReadP95MS))
	}
	if p := res.kindHist("write").Pct(95); p > loadWriteP95MS {
		res.Reasons = append(res.Reasons, fmt.Sprintf("writes p95 %.0f ms (budget %d ms)", p, loadWriteP95MS))
	}
	if !ReconPassed(res.Recon) {
		res.Reasons = append(res.Reasons, "the books do not tie")
	}
	res.Verdict = "pass"
	if len(res.Reasons) > 0 {
		res.Verdict = "fail"
	}
	if res.Generator.PeakPct > loadGenCPUPct {
		res.Verdict = "invalid"
		res.Reasons = append(res.Reasons, fmt.Sprintf("the generator reached %.0f%% CPU (limit %d%%): the figures measure the generator, not the server", res.Generator.PeakPct, loadGenCPUPct))
	}
}

// mergeLoad adds several generators' results into one. Generators share the
// server and database, so its CPU and database time are taken from the
// generator that saw the most, not summed.
func mergeLoad(rs []*LoadResult) *LoadResult {
	out := &LoadResult{Pages: map[string]*PageStat{}}
	for i, r := range rs {
		if i == 0 {
			out.Base, out.DB, out.Tenant, out.Started, out.EveryS = r.Base, r.DB, r.Tenant, r.Started, r.EveryS
		}
		out.Generators = append(out.Generators, r.Generators...)
		out.Users += r.Users
		out.Cashiers += r.Cashiers
		out.Managers += r.Managers
		out.RampS = math.Max(out.RampS, r.RampS)
		out.SteadyS = math.Max(out.SteadyS, r.SteadyS)
		for name, p := range r.Pages {
			m := out.Pages[name]
			if m == nil {
				m = &PageStat{Kind: p.Kind}
				out.Pages[name] = m
			}
			m.OK += p.OK
			m.Failed += p.Failed
			for k, n := range p.Why {
				if m.Why == nil {
					m.Why = map[string]int64{}
				}
				m.Why[k] += n
			}
			m.Due.Merge(&p.Due)
			m.Svc.Merge(&p.Svc)
		}
		out.Requests += r.Requests
		out.SteadyReqs += r.SteadyReqs
		out.Late += r.Late
		out.MaxLateMS = math.Max(out.MaxLateMS, r.MaxLateMS)
		out.Relogins += r.Relogins
		out.Unknown += r.Unknown
		out.Sold += r.Sold
		if r.Server.CPUMS > out.Server.CPUMS {
			out.Server = r.Server
		}
		if r.ServerAll.CPUMS > out.ServerAll.CPUMS {
			out.ServerAll = r.ServerAll
		}
		out.SignInCPU = math.Max(out.SignInCPU, r.SignInCPU)
		if r.DBSteady.DBTimeMS > out.DBSteady.DBTimeMS {
			out.DBSteady = r.DBSteady
		}
		out.Generator.Cores = r.Generator.Cores
		out.Generator.AvgPct = math.Max(out.Generator.AvgPct, r.Generator.AvgPct)
		out.Generator.PeakPct = math.Max(out.Generator.PeakPct, r.Generator.PeakPct)
		out.Recon = append(out.Recon, r.Recon...)
	}
	out.size()
	out.judge()
	return out
}

// ---- the users ----

// loadUser is one named user. Its pages run one after another, as a person's do.
type loadUser struct {
	n          int
	name, role string
	loc        string // a cashier's own shop; a manager's warehouse
	xff        string
	mu         sync.Mutex // guards token: a page's parallel calls share it
	token      string
	etag       string // the report catalog's last ETag, sent back as If-None-Match
	rng        *rand.Rand
}

func (u *loadUser) tok() string { u.mu.Lock(); defer u.mu.Unlock(); return u.token }

// loadShop is user i's shop code.
func loadShop(i int) string { return fmt.Sprintf("JL-LS%05d", i+1) }

// loadXFF gives user i its own public-looking address in 198.18.0.0/15, the
// range set aside for benchmark tests. The server skips private and loopback
// hops in X-Forwarded-For, so these must not be private addresses.
func loadXFF(i int) string {
	ip := uint32(198)<<24 | uint32(18)<<16 + uint32(i+1)
	return fmt.Sprintf("%d.%d.%d.%d", ip>>24, ip>>16&255, ip>>8&255, ip&255)
}

// answer is one HTTP reply. Body keeps only its start, for error messages.
type answer struct {
	status int
	js     map[string]interface{}
	hdr    http.Header
	head   string
	err    error
}

type loadRun struct {
	r        *Runner
	base     string
	tenant   string
	hc       *http.Client
	xffOn    bool
	mu       sync.Mutex
	res      *LoadResult
	reqs     atomic.Int64
	relogins atomic.Int64
	sold     atomic.Int64
	unknown  atomic.Int64
}

// send makes one request. No retry: a load call that fails is a failure.
func (L *loadRun) send(xff, token, method, path string, body interface{}, hdr map[string]string) answer {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return answer{err: err}
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, L.base+path, rd)
	if err != nil {
		return answer{err: err}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if L.tenant != "" && L.tenant != "default" {
		req.Header.Set("X-Tenant-ID", L.tenant)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if L.xffOn && xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	L.reqs.Add(1)
	resp, err := L.hc.Do(req)
	if err != nil {
		return answer{err: err}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	a := answer{status: resp.StatusCode, hdr: resp.Header, head: trunc(string(b), 200), err: err}
	if len(b) > 0 && b[0] == '{' {
		_ = json.Unmarshal(b, &a.js)
	}
	return a
}

// call sends as the user. A 401 mid-run (an expired token) signs the user in
// again and repeats the request once; the page's time includes it.
func (L *loadRun) call(u *loadUser, method, path string, body interface{}, hdr map[string]string) answer {
	tok := u.tok()
	a := L.send(u.xff, tok, method, path, body, hdr)
	if a.status != 401 {
		return a
	}
	u.mu.Lock()
	if u.token == tok {
		lr := L.send(u.xff, "", "POST", "/api/v1/login", map[string]string{"username": u.name, "password": userPass2}, nil)
		if t := jstr(lr.js, "token"); lr.status == 200 && t != "" {
			u.token = t
			L.relogins.Add(1)
		}
	}
	tok = u.token
	u.mu.Unlock()
	return L.send(u.xff, tok, method, path, body, hdr)
}

// failed names why an answer is a failure, or returns "".
func failed(a answer, ok ...int) string {
	if a.err != nil {
		var ne net.Error
		if errors.As(a.err, &ne) && ne.Timeout() {
			return "timeout"
		}
		return "no answer"
	}
	for _, s := range ok {
		if a.status == s {
			return ""
		}
	}
	if a.status >= 200 && a.status < 300 {
		return ""
	}
	return strconv.Itoa(a.status)
}

// together runs a page's independent calls at once, as a browser does; the page
// is as slow as its slowest call.
func together(fns ...func() string) string {
	out := make([]string, len(fns))
	var wg sync.WaitGroup
	for i, f := range fns {
		wg.Add(1)
		go func(i int, f func() string) { defer wg.Done(); out[i] = f() }(i, f)
	}
	wg.Wait()
	for _, w := range out {
		if w != "" {
			return w
		}
	}
	return ""
}

// ---- pages ----

type loadPage struct {
	name, kind string
	weight     int
	run        func(L *loadRun, u *loadUser) string
}

var searchTerms = []string{"JL", "item", "shirt", "1", "blue"}

func pageSignIn(L *loadRun, u *loadUser) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	a := L.send(u.xff, "", "POST", "/api/v1/login", map[string]string{"username": u.name, "password": userPass2}, nil)
	if why := failed(a); why != "" {
		return why
	}
	t := jstr(a.js, "token")
	if t == "" {
		return "check: no token"
	}
	u.token = t
	return ""
}

func pageItemList(L *loadRun, u *loadUser) string {
	return failed(L.call(u, "GET", "/api/v1/doc/Item?limit=50", nil, nil))
}

func pageItemSearch(L *loadRun, u *loadUser) string {
	q := searchTerms[u.rng.Intn(len(searchTerms))]
	return failed(L.call(u, "GET", "/api/v1/doc/Item?limit=20&q="+q, nil, nil))
}

func pageItemPick(L *loadRun, u *loadUser) string {
	return together(
		func() string {
			return failed(L.call(u, "GET", "/api/v1/doc/Item?limit=1&code="+skuMain, nil, nil))
		},
		func() string {
			return failed(L.call(u, "GET", "/api/v1/pos/session/current?location="+u.loc, nil, nil))
		})
}

func pageSalesHistory(L *loadRun, u *loadUser) string {
	return failed(L.call(u, "GET", "/api/v1/pos/sales-history?limit=20", nil, nil))
}

func pageApprovals(L *loadRun, u *loadUser) string {
	return failed(L.call(u, "GET", "/api/v1/approval/pending", nil, nil))
}

func pageCatalog(L *loadRun, u *loadUser) string {
	var h map[string]string
	if u.etag != "" {
		h = map[string]string{"If-None-Match": u.etag}
	}
	a := L.call(u, "GET", "/api/v1/reports/catalog", nil, h)
	if why := failed(a, 304); why != "" {
		return why
	}
	if a.status == 200 {
		u.etag = a.hdr.Get("ETag")
	}
	return ""
}

// pageSale sells one unit for cash. A checkout with no answer may still have
// posted, so it is counted as unknown and the stock check allows for it.
func pageSale(L *loadRun, u *loadUser) string {
	cart := fmt.Sprintf("JLL-%d-%d", u.n, time.Now().UnixNano())
	items := []map[string]interface{}{{"sku": skuMain, "qty": 1}}
	q := L.call(u, "POST", "/api/v1/pos/quote", map[string]interface{}{"cart_number": cart, "location": u.loc, "items": items}, nil)
	if why := failed(q); why != "" {
		return why
	}
	c := L.call(u, "POST", "/api/v1/checkout", map[string]interface{}{"cart_number": cart, "location": u.loc, "payment_mode": "Cash", "amount_paid": 500, "items": items}, nil)
	if c.err != nil {
		L.unknown.Add(1)
		return failed(c)
	}
	if why := failed(c); why != "" {
		return why
	}
	if jstr(c.js, "status") != "completed" || jstr(c.js, "bill_number") == "" {
		L.unknown.Add(1)
		return "check: checkout not completed"
	}
	L.sold.Add(1)
	return ""
}

func pageTransferDraft(L *loadRun, u *loadUser) string {
	return failed(L.call(u, "POST", "/api/v1/doc/TransferOrder", map[string]interface{}{
		"transfer_number": "x", "from_warehouse": locWH, "to_warehouse": locStore, "status": "Draft",
		"items": j2s([]map[string]interface{}{{"sku": skuMain, "qty": 1}})}, nil))
}

// The mix: a cashier mostly finds items and sells; a manager reads lists,
// approvals and reports and raises the odd draft.
var (
	signInPage   = loadPage{"sign in", "write", 0, pageSignIn}
	cashierPages = []loadPage{
		{"item search", "read", 30, pageItemSearch},
		{"item pick", "read", 20, pageItemPick},
		{"sale", "write", 30, pageSale},
		{"sales history", "read", 15, pageSalesHistory},
		{"report catalog", "read", 5, pageCatalog},
	}
	managerPages = []loadPage{
		{"item list", "read", 25, pageItemList},
		{"item search", "read", 20, pageItemSearch},
		{"approvals", "read", 15, pageApprovals},
		{"transfer draft", "write", 15, pageTransferDraft},
		{"sales history", "read", 15, pageSalesHistory},
		{"report catalog", "read", 10, pageCatalog},
	}
)

func pickPage(pages []loadPage, rng *rand.Rand) loadPage {
	total := 0
	for _, p := range pages {
		total += p.weight
	}
	n := rng.Intn(total)
	for _, p := range pages {
		if n < p.weight {
			return p
		}
		n -= p.weight
	}
	return pages[len(pages)-1]
}

// nextDue is a user's next due time: the last due time plus a pause, never the
// time the last page finished, so a slow reply cannot slow the arrival rate.
func nextDue(prev time.Time, minE, maxE time.Duration, rng *rand.Rand) time.Time {
	d := minE
	if maxE > minE {
		d += time.Duration(rng.Int63n(int64(maxE-minE) + 1))
	}
	return prev.Add(d)
}

func (L *loadRun) record(p loadPage, why string, due, t0, t1 time.Time) {
	ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	L.mu.Lock()
	defer L.mu.Unlock()
	ps := L.res.Pages[p.name]
	if ps == nil {
		ps = &PageStat{Kind: p.kind}
		L.res.Pages[p.name] = ps
	}
	if why == "" {
		ps.OK++
	} else {
		ps.Failed++
		if ps.Why == nil {
			ps.Why = map[string]int64{}
		}
		ps.Why[why]++
	}
	ps.Due.Add(ms(t1.Sub(due)))
	ps.Svc.Add(ms(t1.Sub(t0)))
	if late := ms(t0.Sub(due)); late > 1000 {
		L.res.Late++
		if late > L.res.MaxLateMS {
			L.res.MaxLateMS = round2(late)
		}
	}
}

func (L *loadRun) runPage(u *loadUser, p loadPage, due time.Time) {
	if d := time.Until(due); d > 0 {
		time.Sleep(d)
	}
	t0 := time.Now()
	why := p.run(L, u)
	L.record(p, why, due, t0, time.Now())
}

// ---- setup ----

// adminSend is a setup call as the minted admin; a 429 waits and tries again.
func (L *loadRun) adminSend(method, path string, body interface{}) answer {
	admin := L.r.Cl.Sess["admin"]
	for try := 0; ; try++ {
		a := L.send("", admin.Token, method, path, body, nil)
		if a.status == 401 && try == 0 {
			admin.Token = admin.Mint()
			continue
		}
		if a.status != 429 || try >= 20 {
			return a
		}
		wait := 5 * time.Second
		if s, err := strconv.Atoi(a.hdr.Get("Retry-After")); err == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
		time.Sleep(wait)
	}
}

var errNoProxyTrust = errors.New("sign-in refused with 429: start the server with TRUST_PROXY=1, so each load user signs in from its own address (-xff)")

// ensureLoadUser signs the user in, creating it first if needed (create, sign
// in, change the first password, sign in again: as a new person does).
func (L *loadRun) ensureLoadUser(u *loadUser) error {
	login := func(pass string) answer {
		return L.send(u.xff, "", "POST", "/api/v1/login", map[string]string{"username": u.name, "password": pass}, nil)
	}
	a := login(userPass2)
	if a.status == 429 {
		return errNoProxyTrust
	}
	if t := jstr(a.js, "token"); a.status == 200 && t != "" {
		u.token = t
		return nil
	}
	c := L.adminSend("POST", "/api/v1/admin/users", map[string]string{"username": u.name, "password": userPass, "email": u.name + "@example.invalid", "role": u.role, "location_code": u.loc, "department": "JL-OPS"})
	if c.status >= 300 && !strings.Contains(c.head, "exist") && !strings.Contains(c.head, "already taken") {
		return fmt.Errorf("create %s: %d %s %v", u.name, c.status, c.head, c.err)
	}
	a = login(userPass)
	if a.status == 429 {
		return errNoProxyTrust
	}
	first := jstr(a.js, "token")
	if a.status != 200 || first == "" {
		return fmt.Errorf("first sign-in %s: %d %s", u.name, a.status, a.head)
	}
	cp := L.send(u.xff, first, "POST", "/api/v1/me/change-password", map[string]string{"current_password": userPass, "new_password": userPass2}, nil)
	if cp.status != 200 {
		return fmt.Errorf("change password %s: %d %s", u.name, cp.status, cp.head)
	}
	time.Sleep(1100 * time.Millisecond) // a token must be newer than the password change
	a = login(userPass2)
	if t := jstr(a.js, "token"); a.status == 200 && t != "" {
		u.token = t
		return nil
	}
	return fmt.Errorf("sign-in %s: %d %s", u.name, a.status, a.head)
}

// openTill opens the cashier's till, or finds the one already open.
func (L *loadRun) openTill(u *loadUser) error {
	o := L.call(u, "POST", "/api/v1/pos/session/open", map[string]interface{}{"location": u.loc, "opening_cash": 0}, nil)
	if firstNonEmpty(jstr(o.js, "session_id"), jstr(o.js, "id")) != "" {
		return nil
	}
	cur := L.call(u, "GET", "/api/v1/pos/session/current?location="+u.loc, nil, nil)
	if firstNonEmpty(jstr(cur.js, "session_id"), jstr(cur.js, "id")) != "" {
		return nil
	}
	return fmt.Errorf("till for %s: open %d %s; current %d %s", u.name, o.status, o.head, cur.status, cur.head)
}

// parallelDo runs fn over the users with a few workers and returns the first error.
func parallelDo(users []*loadUser, workers int, fn func(u *loadUser) error) error {
	ch := make(chan *loadUser)
	var first error
	var mu sync.Mutex
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range ch {
				if err := fn(u); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
				}
			}
		}()
	}
	for _, u := range users {
		mu.Lock()
		stop := first != nil
		mu.Unlock()
		if stop {
			break
		}
		ch <- u
	}
	close(ch)
	wg.Wait()
	return first
}

// ---- the command ----

func cmdLoad(args []string) int {
	fs := flag.NewFlagSet("load", flag.ExitOnError)
	var c commonFlags
	c.register(fs)
	users := fs.Int("users", 200, "named users; every fifth is a store manager, the rest cashiers")
	dur := fs.Duration("duration", 5*time.Minute, "how long everyone works after the sign-in window")
	ramp := fs.Duration("ramp", time.Minute, "the sign-in window: users sign in spread over it (the morning login spike)")
	minE := fs.Duration("every-min", 10*time.Second, "shortest pause between a user's pages")
	maxE := fs.Duration("every-max", 30*time.Second, "longest pause between a user's pages")
	xff := fs.Bool("xff", true, "send each user's own address in X-Forwarded-For (the server needs TRUST_PROXY=1)")
	timeout := fs.Duration("timeout", 30*time.Second, "a call with no answer in this long is a failure")
	seed := fs.Int64("seed", 1, "seed for the page mix and pauses")
	gen := fs.String("generator", "", "this generator's name in a merged result (default: the host name)")
	merge := fs.String("merge", "", "add up load-*.json files from several generators (comma separated) and judge them; drives nothing")
	_ = fs.Parse(args)

	if *merge != "" {
		return loadMerge(strings.Split(*merge, ","), c.out)
	}
	if *users < 1 || *maxE < *minE || *minE <= 0 {
		fmt.Fprintln(os.Stderr, "journeylab load: -users must be at least 1 and 0 < -every-min <= -every-max")
		return 2
	}
	if *gen == "" {
		*gen, _ = os.Hostname()
	}
	r, err := bootstrap(&c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journeylab load:", err)
		return 2
	}
	r.Cfg.Settle, r.St.Settle = 0, 0
	fmt.Println("setup: master data and role users ...")
	if err := r.Setup(); err != nil {
		fmt.Fprintln(os.Stderr, "journeylab load: setup:", err)
		return 2
	}

	L := &loadRun{r: r, base: strings.TrimRight(c.baseURL, "/"), tenant: c.tenant, xffOn: *xff,
		hc: &http.Client{Timeout: *timeout, Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			MaxIdleConnsPerHost: *users + 16,
			IdleConnTimeout:     90 * time.Second,
		}},
		res: &LoadResult{Generators: []string{*gen}, Base: c.baseURL, DB: DBNameFromURL(c.dbURL), Tenant: c.tenant, Users: *users,
			EveryS: [2]float64{minE.Seconds(), maxE.Seconds()}, Pages: map[string]*PageStat{}}}

	var us []*loadUser
	for i := 0; i < *users; i++ {
		// A shop has one open till, so each cashier works its own shop (a chain
		// of one-till shops); managers sit at the warehouse, as jl-sm does.
		u := &loadUser{n: i, name: fmt.Sprintf("jl-load-%05d", i+1), role: "Cashier", loc: loadShop(i), xff: loadXFF(i), rng: rand.New(rand.NewSource(*seed + int64(i)))}
		if i%5 == 4 {
			u.role, u.loc = "Store Manager", locWH
			L.res.Managers++
		} else {
			L.res.Cashiers++
		}
		us = append(us, u)
	}
	var shops []string
	for _, u := range us {
		if u.role == "Cashier" {
			shops = append(shops, u.loc)
		}
	}

	// Each shop gets stock for every sale it can make in the run, with room to
	// spare: a purchase order and goods receipt straight into the shop.
	r.Cl.Retries = 40 // the setup burst outruns the per-minute limit; wait it out
	fmt.Printf("setup: %d shops ...\n", len(shops))
	for _, s := range shops {
		if err := r.ensureDoc("Location", "code", s, map[string]interface{}{"code": s, "name": "JL load shop " + s, "type": "Store", "legal_entity": "JLLE", "status": "Active", "sellable": "Yes", "state": "Karnataka", "city": "Bengaluru", "pincode": "560001"}); err != nil {
			fmt.Fprintln(os.Stderr, "journeylab load: setup:", err)
			return 2
		}
	}
	mean := (*minE + *maxE).Seconds() / 2
	need := int(math.Ceil((dur.Seconds()/mean+1)*0.30*1.5)) + 5
	j0 := r.NewJourney("L0", "Stock the shops for the load run")
	short := 0
	for _, s := range shops {
		if have := r.onHand(skuMain, s); have < float64(need) {
			j0.receiveStockAt(s, need-int(have), s+": ")
			short++
		}
	}
	if short > 0 {
		fmt.Printf("setup: stocked %d shops with up to %d units each\n", short, need)
		if rec := j0.Finish(nil); rec.Status != "pass" {
			for _, s := range rec.Steps {
				for _, e := range s.Errors {
					fmt.Fprintf(os.Stderr, "    step %q: %s\n", s.Name, e)
				}
			}
			fmt.Fprintln(os.Stderr, "journeylab load: could not stock the shops")
			return 2
		}
	}
	r.Cl.Retries = 3

	fmt.Printf("setup: %d load users (create or sign in, open the tills) ...\n", *users)
	if err := parallelDo(us, 8, func(u *loadUser) error {
		if err := L.ensureLoadUser(u); err != nil {
			return err
		}
		if u.role == "Cashier" {
			return L.openTill(u)
		}
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "journeylab load: setup:", err)
		return 2
	}

	shopStock := func() float64 {
		var t float64
		for _, s := range shops {
			t += r.onHand(skuMain, s)
		}
		return t
	}
	base := shopStock()
	pid := r.pid()
	self := os.Getpid()
	cores := runtime.NumCPU()
	L.res.Generator.Cores = cores
	L.reqs.Store(0)

	start := time.Now()
	L.res.Started = start.UTC().Format(time.RFC3339)
	end := start.Add(*ramp + *dur)
	s0, g0 := ReadProc(pid), ReadProc(self)
	fmt.Printf("run: %d users sign in over %s, then work for %s (one page every %s-%s each) ...\n", *users, *ramp, *dur, *minE, *maxE)

	// The steady window starts when the last sign-in is done.
	var signedIn atomic.Int64
	steady := make(chan struct{})
	var steadyAt time.Time
	var s1 ProcSnap
	var d1 DBSnap
	var req1 int64
	steadyTaken := make(chan struct{})
	go func() {
		<-steady
		req1, s1, steadyAt = L.reqs.Load(), ReadProc(pid), time.Now()
		d1 = r.St.Snap()
		close(steadyTaken)
	}()

	// The generator's own CPU, every 5 seconds.
	done := make(chan struct{})
	var peak float64
	var samplerWG sync.WaitGroup
	samplerWG.Add(1)
	go func() {
		defer samplerWG.Done()
		prev, prevAt := g0, time.Now()
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-t.C:
				cur := ReadProc(self)
				if cur.OK && prev.OK {
					if pct := (cur.CPUMS - prev.CPUMS) * 100 / (float64(now.Sub(prevAt).Milliseconds()) * float64(cores)); pct > peak {
						peak = pct
					}
				}
				prev, prevAt = cur, now
			}
		}
	}()

	var wg sync.WaitGroup
	for i, u := range us {
		wg.Add(1)
		go func(i int, u *loadUser) {
			defer wg.Done()
			due := start.Add(time.Duration(int64(*ramp) * int64(i) / int64(len(us))))
			L.runPage(u, signInPage, due)
			if signedIn.Add(1) == int64(len(us)) {
				close(steady)
			}
			pages := cashierPages
			if u.role != "Cashier" {
				pages = managerPages
			}
			for {
				due = nextDue(due, *minE, *maxE, u.rng)
				if due.After(end) {
					return
				}
				L.runPage(u, pickPage(pages, u.rng), due)
			}
		}(i, u)
	}
	wg.Wait()
	finished := time.Now()
	close(done)
	samplerWG.Wait()
	<-steadyTaken
	s2, g2 := ReadProc(pid), ReadProc(self)
	time.Sleep(1100 * time.Millisecond) // statistics reach pg_stat a moment after the work
	d2 := r.St.Snap()

	res := L.res
	res.Requests = L.reqs.Load()
	res.SteadyReqs = res.Requests - req1
	res.RampS = round2(steadyAt.Sub(start).Seconds())
	res.SteadyS = round2(finished.Sub(steadyAt).Seconds())
	res.ServerAll = ProcDiff(s0, s2)
	res.Server = ProcDiff(s1, s2)
	res.SignInCPU = ProcDiff(s0, s1).CPUMS
	res.DBSteady = Delta(d1, d2)
	res.Relogins, res.Unknown, res.Sold = L.relogins.Load(), L.unknown.Load(), L.sold.Load()
	if g := ProcDiff(g0, g2); g.Available {
		res.Generator.AvgPct = round2(g.CPUMS * 100 / (float64(finished.Sub(start).Milliseconds()) * float64(cores)))
	}
	res.Generator.PeakPct = round2(peak)

	// The books: every ledger check, and the shop's stock fell by exactly the
	// units sold (allowing for checkouts that got no answer).
	fmt.Println("books: reconciling ...")
	j := r.NewJourney("L1", "Load run")
	res.Recon = j.Reconcile()
	got := shopStock() - base
	lo, hi := -float64(res.Sold+res.Unknown), -float64(res.Sold)
	res.Recon = append(res.Recon, ReconCheck{Kind: "stock", Name: "the shops' stock fell by the units sold", Pass: got >= lo-0.001 && got <= hi+0.001,
		Diff: round2(got - hi), Detail: fmt.Sprintf("sold %d, no answer %d, stock moved %.0f", res.Sold, res.Unknown, got)})
	res.size()
	res.judge()

	jp, mp, err := writeLoad(c.out, res)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journeylab load: write:", err)
		return 2
	}
	fmt.Print(loadSummary(res))
	fmt.Printf("evidence: %s\nsummary:  %s\n", jp, mp)
	if res.Verdict != "pass" {
		return 1
	}
	return 0
}

func loadMerge(files []string, out string) int {
	var rs []*LoadResult
	for _, f := range files {
		b, err := os.ReadFile(strings.TrimSpace(f))
		if err != nil {
			fmt.Fprintln(os.Stderr, "journeylab load:", err)
			return 2
		}
		var r LoadResult
		if err := json.Unmarshal(b, &r); err != nil {
			fmt.Fprintf(os.Stderr, "journeylab load: %s: %v\n", f, err)
			return 2
		}
		rs = append(rs, &r)
	}
	res := mergeLoad(rs)
	jp, mp, err := writeLoad(out, res)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journeylab load: write:", err)
		return 2
	}
	fmt.Print(loadSummary(res))
	fmt.Printf("evidence: %s\nsummary:  %s\n", jp, mp)
	if res.Verdict != "pass" {
		return 1
	}
	return 0
}

func writeLoad(out string, res *LoadResult) (string, string, error) {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", "", err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	jp := filepath.Join(out, "load-"+stamp+".json")
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jp, b, 0o644); err != nil {
		return "", "", err
	}
	mp := strings.TrimSuffix(jp, ".json") + ".md"
	return jp, mp, os.WriteFile(mp, []byte(loadMarkdown(res)), 0o644)
}

// loadSummary is the few lines printed at the end of a run.
func loadSummary(res *LoadResult) string {
	pages, failedN := res.totals()
	var b strings.Builder
	fmt.Fprintf(&b, "verdict: %s\n", strings.ToUpper(res.Verdict))
	for _, why := range res.Reasons {
		fmt.Fprintf(&b, "  - %s\n", why)
	}
	fmt.Fprintf(&b, "pages %d, failed %d; reads p95 %.0f ms, writes p95 %.0f ms\n", pages, failedN, res.kindHist("read").Pct(95), res.kindHist("write").Pct(95))
	fmt.Fprintf(&b, "steady %.1f requests/s; server %.2f CPU ms and database %.2f ms a request\n", res.Sizing.RPS, res.Sizing.ServerCPUMSReq, res.Sizing.DBMSReq)
	return b.String()
}

func loadMarkdown(res *LoadResult) string {
	var b strings.Builder
	b.WriteString(recordHeader("Load run", res.Started))
	pages, failedN := res.totals()
	fmt.Fprintf(&b, "**Verdict: %s.** %d users (%d cashiers, %d managers) on %s, database %s, generator %s.\n\n",
		strings.ToUpper(res.Verdict), res.Users, res.Cashiers, res.Managers, res.Base, res.DB, strings.Join(res.Generators, ", "))
	for _, why := range res.Reasons {
		fmt.Fprintf(&b, "- %s\n", why)
	}
	if len(res.Reasons) > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Sign-in window %.0f s, then %.0f s of work, one page every %.0f-%.0f s per user. Times are from when each page was due.\n\n",
		res.RampS, res.SteadyS, res.EveryS[0], res.EveryS[1])
	b.WriteString("| Page | Kind | Runs | Failed | p50 ms | p95 ms | p99 ms | Max ms | Why it failed |\n|---|---|---|---|---|---|---|---|---|\n")
	names := make([]string, 0, len(res.Pages))
	for n := range res.Pages {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := res.Pages[n]
		var why []string
		for k, v := range p.Why {
			why = append(why, fmt.Sprintf("%s ×%d", k, v))
		}
		sort.Strings(why)
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %.0f | %.0f | %.0f | %.0f | %s |\n", n, p.Kind, p.OK+p.Failed, p.Failed,
			p.Due.Pct(50), p.Due.Pct(95), p.Due.Pct(99), p.Due.MaxMS, strings.Join(why, ", "))
	}
	rh, wh := res.kindHist("read"), res.kindHist("write")
	fmt.Fprintf(&b, "| **all reads** | | %d | | %.0f | %.0f | %.0f | %.0f | |\n", rh.N, rh.Pct(50), rh.Pct(95), rh.Pct(99), rh.MaxMS)
	fmt.Fprintf(&b, "| **all writes** | | %d | | %.0f | %.0f | %.0f | %.0f | |\n\n", wh.N, wh.Pct(50), wh.Pct(95), wh.Pct(99), wh.MaxMS)
	fmt.Fprintf(&b, "Pages %d, failed %d. Requests %d (%d after the last sign-in). Late starts %d (worst %.0f ms). Signed in again after a 401: %d. Retries: none by design. Units sold %d; checkouts with no answer %d.\n\n",
		pages, failedN, res.Requests, res.SteadyReqs, res.Late, res.MaxLateMS, res.Relogins, res.Sold, res.Unknown)
	s := res.Sizing
	b.WriteString("## Cost and size\n\n")
	fmt.Fprintf(&b, "Steady %.1f requests a second (%.4f a user). Each request cost the server %.2f CPU ms and the database %.2f ms (%s). A sign-in cost at most %.1f CPU ms. Generator CPU: %.1f%% average, %.1f%% at its busiest, of %d cores.\n\n",
		s.RPS, s.RPSPerUser, s.ServerCPUMSReq, s.DBMSReq, res.DBSteady.Source, s.SignInCPUMS, res.Generator.AvgPct, res.Generator.PeakPct, res.Generator.Cores)
	if s.Note != "" {
		fmt.Fprintf(&b, "%s.\n\n", s.Note)
	}
	b.WriteString("Cores = requests a second × ms a request ÷ 1000 ÷ 0.6 (60% busy at the target).\n\n| Target | Requests/s | Server cores | Database cores |\n|---|---|---|---|\n")
	for _, t := range s.For {
		fmt.Fprintf(&b, "| %s | %.0f | %.1f | %.1f |\n", t.Label, t.RPS, t.ServerCores, t.DBCores)
	}
	b.WriteString("\n## Books\n\n")
	for _, k := range res.Recon {
		fmt.Fprintf(&b, "- %s %s: %s\n", mark(map[bool]string{true: "pass", false: "fail"}[k.Pass || k.Warn]), k.Name, k.Detail)
	}
	return b.String()
}
