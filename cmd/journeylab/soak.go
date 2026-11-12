package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"custom_erp/db"
)

// Soak: a light journey repeated for a long time, with disturbances. It starts
// its own private Postgres (so restarting the database is safe) and its own
// server (so restarting the server is safe), runs the repeating cycle in the
// background, and fires each disturbance once at a planned moment.

// newSoakSecret is a fresh signing secret for one soak run. It lives only in
// the environment of the processes the run starts, so nothing fixed is shared.
func newSoakSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type serverProc struct {
	bin, dir string
	port     int
	env      []string
	cmd      *exec.Cmd
	logf     *os.File
	pid      atomic.Int64
}

func (s *serverProc) Start() error {
	lf, err := os.OpenFile(filepath.Join(s.dir, "server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	s.logf = lf
	cmd := exec.Command(s.bin)
	cmd.Env = append(os.Environ(), s.env...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("PORT=%d", s.port))
	cmd.Stdout, cmd.Stderr = lf, lf
	if err := cmd.Start(); err != nil {
		return err
	}
	s.cmd = cmd
	s.pid.Store(int64(cmd.Process.Pid))
	return nil
}

// Kill ends the server hard, as a crash would.
func (s *serverProc) Kill() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
	if s.logf != nil {
		s.logf.Close()
	}
	s.pid.Store(0)
}

func healthy(base string) bool {
	c := http.Client{Timeout: 3 * time.Second}
	r, err := c.Get(base + "/api/v1/health")
	if err != nil {
		return false
	}
	r.Body.Close()
	return r.StatusCode == 200
}

func waitHealthy(base string, max time.Duration) (time.Duration, bool) {
	t0 := time.Now()
	for time.Since(t0) < max {
		if healthy(base) {
			return time.Since(t0), true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return time.Since(t0), false
}

// CycleResult is one pass of the repeating journey, kept small.
type CycleResult struct {
	At          float64 `json:"at_seconds"`
	Status      string  `json:"status"`
	WallMS      float64 `json:"wall_ms"`
	P95MS       float64 `json:"api_p95_ms"`
	InDisturbed bool    `json:"during_disturbance"`
	FirstError  string  `json:"first_error,omitempty"`
}

// Disturbance is the verdict on one planned disturbance.
type Disturbance struct {
	Name     string   `json:"name"`
	Status   string   `json:"status"` // pass, fail, not_exercised
	Detail   string   `json:"detail"`
	Measures []string `json:"measures,omitempty"`
}

type SoakEvidence struct {
	SchemaVersion  int            `json:"schema_version"`
	Kind           string         `json:"kind"`
	GeneratedAt    string         `json:"generated_at"`
	DurationPlan   string         `json:"duration_planned"`
	DurationActual float64        `json:"duration_actual_seconds"`
	DBStatsSource  string         `json:"db_stats_source"`
	Cycles         int            `json:"cycles"`
	CyclesPass     int            `json:"cycles_pass"`
	CyclesFailOut  int            `json:"cycles_failed_outside_disturbances"`
	CyclesFailIn   int            `json:"cycles_failed_during_disturbances"`
	P95FirstQuart  float64        `json:"api_p95_ms_first_quarter"`
	P95LastQuart   float64        `json:"api_p95_ms_last_quarter"`
	RSSKBStart     int64          `json:"server_rss_kb_start"`
	RSSKBEnd       int64          `json:"server_rss_kb_end"`
	RSSKBPeak      int64          `json:"server_rss_kb_peak"`
	Disturbances   []Disturbance  `json:"disturbances"`
	Disk           DiskProjection `json:"disk_projection_24h"`
	Final          []ReconCheck   `json:"final_reconciliation"`
	Overall        string         `json:"overall"`
	Command24h     string         `json:"command_for_24h_run"`
	Limits         []string       `json:"limits"`
	CycleSample    []CycleResult  `json:"cycle_sample"`
}

func cmdSoak(args []string) int {
	fs := flag.NewFlagSet("soak", flag.ExitOnError)
	var c commonFlags
	c.register(fs)
	dur := fs.Duration("duration", 30*time.Minute, "how long to run the repeating journey")
	work := fs.String("work-dir", "", "scratch folder for the private cluster and server log (default: a new folder in the temp dir)")
	keep := fs.Bool("keep", false, "leave the private cluster running and its folder in place afterwards")
	_ = fs.Parse(args)
	if os.Getenv("JWT_SECRET") == "" {
		// The engines read the signing secret when the process starts, so the
		// only way to share one with the server we launch is to start again with it set.
		self, _ := os.Executable()
		cmd := exec.Command(self, append([]string{"soak"}, args...)...)
		cmd.Env = append(os.Environ(), "JWT_SECRET="+newSoakSecret())
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return ee.ExitCode()
			}
			return 1
		}
		return 0
	}
	if c.serverBin == "" || c.pgBin == "" {
		fmt.Fprintln(os.Stderr, "journeylab soak: -server-bin and -pg-bin are required")
		return 2
	}
	if *dur < 6*time.Minute {
		fmt.Fprintln(os.Stderr, "journeylab soak: -duration must be at least 6m (the disturbances need room to be seen)")
		return 2
	}
	wd := *work
	if wd == "" {
		wd = filepath.Join(os.TempDir(), fmt.Sprintf("journeylab-soak-%d", time.Now().Unix()))
	}
	if err := os.MkdirAll(wd, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	port, err := FreePort(5600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cl := Cluster{BinDir: c.pgBin, Dir: filepath.Join(wd, "pg"), Port: port}
	const dbname = "erp_soak_scratch"
	if err := cl.Up(); err != nil {
		fmt.Fprintln(os.Stderr, "journeylab soak: cluster:", err)
		return 1
	}
	if !*keep {
		defer func() { _ = cl.Stop() }()
	}
	time.Sleep(500 * time.Millisecond)
	if err := cl.CreateDB(dbname); err != nil {
		fmt.Fprintln(os.Stderr, "journeylab soak:", err)
		return 1
	}
	dbURL := cl.URL(dbname)
	secret := os.Getenv("JWT_SECRET")
	env := []string{"DATABASE_URL=" + dbURL, "JWT_SECRET=" + secret, "ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=1"}
	if out, err := runCmd(append(os.Environ(), env...), c.serverBin, "-migrate"); err != nil {
		fmt.Fprintln(os.Stderr, "journeylab soak: migrate:", err, trunc(out, 400))
		return 1
	}
	spPort, err := FreePort(8731)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	sp := &serverProc{bin: c.serverBin, dir: wd, port: spPort, env: env}
	if err := sp.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "journeylab soak: server:", err)
		return 1
	}
	defer sp.Kill()
	base := fmt.Sprintf("http://127.0.0.1:%d", spPort)
	if _, ok := waitHealthy(base, 90*time.Second); !ok {
		fmt.Fprintln(os.Stderr, "journeylab soak: the server did not come up; see", filepath.Join(wd, "server.log"))
		return 1
	}

	c.baseURL, c.dbURL, c.allowDB = base, dbURL, dbname
	c.serverPID = 0
	if err := CheckAll(c.baseURL, c.dbURL, c.allowDB, false); err != nil {
		fmt.Fprintln(os.Stderr, "journeylab soak:", err)
		return 2
	}
	db.InitDB(dbURL)
	schema, _ := db.GetTenantSchema(c.tenant)
	r, err := newRunner(&c, db.DB, schema)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journeylab soak:", err)
		return 1
	}
	r.Cfg.ServerPID = func() int { return int(sp.pid.Load()) }
	r.Cfg.Settle, r.St.Settle, r.Cfg.Quick = 0, 0, true
	if err := r.Setup(); err != nil {
		fmt.Fprintln(os.Stderr, "journeylab soak: setup:", err)
		return 1
	}
	ev := runSoak(r, sp, cl, base, *dur, &c)
	jp := filepath.Join(c.out, fmt.Sprintf("soak-%s.json", time.Now().UTC().Format("20060102-150405")))
	_ = os.MkdirAll(c.out, 0o755)
	b, _ := json.MarshalIndent(ev, "", "  ")
	_ = os.WriteFile(jp, b, 0o644)
	_ = os.WriteFile(strings.TrimSuffix(jp, ".json")+".md", []byte(SoakMarkdown(ev)), 0o644)
	fmt.Printf("overall: %s\nevidence: %s\n", strings.ToUpper(ev.Overall), jp)
	if ev.Overall != "pass" {
		return 1
	}
	return 0
}

func runSoak(r *Runner, sp *serverProc, cl Cluster, base string, dur time.Duration, c *commonFlags) SoakEvidence {
	start := time.Now()
	var mu sync.Mutex
	var cycles []CycleResult
	var disturbed atomic.Bool
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() { // the repeating journey
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rec := r.JourneyFirstSale()
			cr := CycleResult{At: round2(time.Since(start).Seconds()), Status: rec.Status, WallMS: rec.WallMS, P95MS: rec.Summary.HTTP.P95MS, InDisturbed: disturbed.Load()}
			for _, s := range rec.Steps {
				if len(s.Errors) > 0 {
					cr.FirstError = trunc(s.Name+": "+s.Errors[0], 160)
					break
				}
			}
			mu.Lock()
			cycles = append(cycles, cr)
			mu.Unlock()
			if rec.Status != "pass" {
				time.Sleep(3 * time.Second) // do not hammer a server that is down
			} else {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}()

	var rssStart, rssPeak, rssEnd int64
	var samples sync.Mutex
	wg.Add(1)
	go func() { // sample the server's memory
		defer wg.Done()
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p := ReadProc(int(sp.pid.Load()))
				if p.RSSKB <= 0 {
					continue
				}
				samples.Lock()
				if rssStart == 0 {
					rssStart = p.RSSKB
				}
				if p.RSSKB > rssPeak {
					rssPeak = p.RSSKB
				}
				rssEnd = p.RSSKB
				samples.Unlock()
			}
		}
	}()

	d0 := r.St.Snap()
	var ds []Disturbance
	at := func(frac float64) {
		target := start.Add(time.Duration(float64(dur) * frac))
		if w := time.Until(target); w > 0 {
			time.Sleep(w)
		}
	}
	recover := func(name string) (string, bool) {
		// after a disturbance: the next cycle must pass without help
		deadline := time.Now().Add(150 * time.Second)
		n0 := func() int { mu.Lock(); defer mu.Unlock(); return len(cycles) }()
		for time.Now().Before(deadline) {
			mu.Lock()
			for _, cr := range cycles[n0:] {
				if cr.Status == "pass" {
					mu.Unlock()
					return "", true
				}
			}
			mu.Unlock()
			time.Sleep(2 * time.Second)
		}
		return name + ": no passing cycle within 150 s", false
	}

	// 1. server restart mid-load
	at(0.15)
	disturbed.Store(true)
	pend0 := pendingOutbox(r)
	sp.Kill()
	t1 := time.Now()
	err := sp.Start()
	hd, ok := waitHealthy(base, 120*time.Second)
	if err != nil || !ok {
		ds = append(ds, Disturbance{Name: "server restart mid-load", Status: "fail", Detail: fmt.Sprintf("restart failed: %v healthy=%v", err, ok)})
	} else {
		msg, rec := recover("server restart")
		st := "pass"
		if !rec {
			st = "fail"
		}
		ds = append(ds, Disturbance{Name: "server restart mid-load", Status: st, Detail: "server killed hard while the journey was running; " + firstNonEmpty(msg, "the next cycle passed without help"),
			Measures: []string{fmt.Sprintf("healthy again after %.1f s", hd.Seconds()), fmt.Sprintf("back in service after %.1f s", time.Since(t1).Seconds())}})
	}
	disturbed.Store(false)

	// 2. outbox drains after the restart
	at(0.25)
	ds = append(ds, outboxDrain(r, pend0))

	// 3. token and limiter key churn
	at(0.35)
	ds = append(ds, tokenChurn(r, sp, base, c))

	// 4. database restart
	at(0.5)
	disturbed.Store(true)
	t2 := time.Now()
	if err := cl.Restart(); err != nil {
		ds = append(ds, Disturbance{Name: "database restart", Status: "fail", Detail: err.Error()})
	} else {
		msg, rec := recover("database restart")
		st := "pass"
		if !rec {
			st = "fail"
		}
		ds = append(ds, Disturbance{Name: "database restart", Status: st, Detail: "Postgres restarted (fast) under load, server kept running; " + firstNonEmpty(msg, "service resumed without a server restart"),
			Measures: []string{fmt.Sprintf("service resumed after %.1f s", time.Since(t2).Seconds())}})
	}
	disturbed.Store(false)

	// 5. month boundary and time zone
	at(0.65)
	ds = append(ds, monthBoundary(r))

	at(1.0)
	close(stop)
	wg.Wait()
	d1 := r.St.Snap()
	secs := time.Since(start).Seconds()

	ev := SoakEvidence{SchemaVersion: evidenceSchemaVersion, Kind: "journeylab.soak", GeneratedAt: time.Now().UTC().Format(time.RFC3339), DurationPlan: dur.String(),
		DurationActual: round2(secs), DBStatsSource: r.St.Source, Disturbances: ds}
	grown := Delta(d0, d1)
	ev.Disk = ProjectDisk(secs, grown.DBGrowthBytes, 24)
	mu.Lock()
	for _, cr := range cycles {
		ev.Cycles++
		switch {
		case cr.Status == "pass":
			ev.CyclesPass++
		case cr.InDisturbed:
			ev.CyclesFailIn++
		default:
			ev.CyclesFailOut++
		}
	}
	ev.P95FirstQuart, ev.P95LastQuart = quartP95(cycles)
	step := len(cycles)/40 + 1
	for i := 0; i < len(cycles); i += step {
		ev.CycleSample = append(ev.CycleSample, cycles[i])
	}
	mu.Unlock()
	samples.Lock()
	ev.RSSKBStart, ev.RSSKBEnd, ev.RSSKBPeak = rssStart, rssEnd, rssPeak
	samples.Unlock()

	// full reconciliation on whatever the load and the disturbances left behind
	r.Cfg.Quick = false
	j := r.NewJourney("SOAK", "final reconciliation")
	ev.Final = j.Reconcile()

	ev.Overall = "pass"
	if !ReconPassed(ev.Final) || ev.CyclesFailOut > 0 || ev.CyclesPass == 0 {
		ev.Overall = "fail"
	}
	for _, d := range ds {
		if d.Status == "fail" {
			ev.Overall = "fail"
		}
	}
	ev.Command24h = "journeylab soak -duration 24h -server-bin <server> -pg-bin <pg bin dir> -out <dir>   (needs the same JWT_SECRET exported as the soak uses; see the guide)"
	ev.Limits = []string{
		"the time-zone check is a database check: there is no application clock seam, so the app itself cannot be run at a faked date",
		"the limiter check watches the server's memory; the limiter's own table size is not exposed",
		"the soak runs on one machine, so server and database share its CPU and disk",
		"disk projection is linear from this run's measured growth at this load",
	}
	return ev
}

func quartP95(cs []CycleResult) (float64, float64) {
	var ok []CycleResult
	for _, c := range cs {
		if c.Status == "pass" {
			ok = append(ok, c)
		}
	}
	if len(ok) < 8 {
		return 0, 0
	}
	q := len(ok) / 4
	med := func(xs []CycleResult) float64 {
		v := make([]float64, 0, len(xs))
		for _, x := range xs {
			v = append(v, x.P95MS)
		}
		sort.Float64s(v)
		return v[len(v)/2]
	}
	return med(ok[:q]), med(ok[len(ok)-q:])
}

func pendingOutbox(r *Runner) int64 {
	var n int64
	_ = r.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.integration_event_outbox WHERE status = 'Pending'`, r.Cfg.Schema)).Scan(&n)
	return n
}
