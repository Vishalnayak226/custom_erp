package main

import (
	"database/sql"
	"fmt"
	"time"
)

// Config is everything a run needs.
type Config struct {
	BaseURL     string
	DBURL       string
	Tenant      string
	Schema      string
	ServerPID   func() int // current server pid (it changes across restarts); nil when unknown
	Settle      time.Duration
	InjectFault bool // fail-first: add an unbalanced ledger row before reconciliation
	AdminToken  string
	Quick       bool   // reconcile only the cheap SQL checks (soak cycles); the full set runs at the end
	PGBin       string // folder holding pg_dump, pg_restore, createdb, dropdb
	ServerBin   string // the server binary, for -verify-restore
}

// Runner drives journeys and records evidence.
type Runner struct {
	Cfg Config
	Cl  *Client
	DB  *sql.DB
	St  *DBStats
}

func (r *Runner) pid() int {
	if r.Cfg.ServerPID == nil {
		return 0
	}
	return r.Cfg.ServerPID()
}

// Journey collects the steps of one journey.
type Journey struct {
	R      *Runner
	Rec    JourneyRecord
	calls  []Call
	failed bool
	Vars   map[string]string
	start  time.Time
	roles  map[string]bool

	stockBase map[string]float64
	expects   []StockExpect
}

// StepCtx is handed to a step body.
type StepCtx struct {
	J      *Journey
	errs   []string
	status []int
}

// NewJourney starts a journey record.
func (r *Runner) NewJourney(id, title string) *Journey {
	return &Journey{R: r, Rec: JourneyRecord{ID: id, Title: title, Status: "pass"}, Vars: map[string]string{}, start: time.Now(), roles: map[string]bool{}}
}

// Step runs fn as a measured step. A returned error or a failed assertion marks
// the step failed and stops the journey (later steps depend on earlier ones).
func (j *Journey) Step(name, role string, fn func(s *StepCtx) error) bool {
	if j.failed {
		return false
	}
	j.roles[role] = true
	r := j.R
	r.Cl.SetStep(name)
	r.Cl.Take()
	d0 := r.St.Snap()
	p0 := ReadProc(r.pid())
	t0 := time.Now()
	sc := &StepCtx{J: j}
	err := fn(sc)
	wall := float64(time.Since(t0).Microseconds()) / 1000
	calls := r.Cl.Take()
	p1 := ReadProc(r.pid())
	d1 := r.St.Snap()
	rec := StepRecord{Name: name, Role: role, WallMS: round2(wall), HTTP: SummarizeCalls(calls), DB: Delta(d0, d1), Proc: ProcDiff(p0, p1), Errors: sc.errs}
	for _, c := range calls {
		rec.Status = append(rec.Status, c.Status)
		if c.Err != "" {
			rec.Errors = append(rec.Errors, fmt.Sprintf("%s %s: %s", c.Method, c.Path, c.Err))
		} else if c.IsError() {
			rec.Errors = append(rec.Errors, fmt.Sprintf("%s %s -> %d", c.Method, c.Path, c.Status))
		}
	}
	if err != nil {
		rec.Errors = append(rec.Errors, err.Error())
	}
	j.calls = append(j.calls, calls...)
	j.Rec.Steps = append(j.Rec.Steps, rec)
	if len(rec.Errors) > 0 {
		j.failed = true
		return false
	}
	return true
}

// Call sends a request as role and fails the step on an unexpected status.
func (s *StepCtx) Call(role, method, path string, body interface{}, expect ...int) *Resp {
	resp, err := s.J.R.Cl.Do(role, method, path, body, CallOpts{Expect: expect})
	if err != nil {
		s.errs = append(s.errs, err.Error())
		return &Resp{}
	}
	return resp
}

// SetupCall is a call that is not a business write.
func (s *StepCtx) SetupCall(role, method, path string, body interface{}, expect ...int) *Resp {
	resp, err := s.J.R.Cl.Do(role, method, path, body, CallOpts{Expect: expect, Setup: true})
	if err != nil {
		s.errs = append(s.errs, err.Error())
		return &Resp{}
	}
	return resp
}

// Check records a failed assertion as a step error.
func (s *StepCtx) Check(ok bool, format string, a ...interface{}) bool {
	if !ok {
		s.errs = append(s.errs, "check failed: "+fmt.Sprintf(format, a...))
	}
	return ok
}

// Finish closes the journey: reconciliation, rollup, verdict.
func (j *Journey) Finish(checks []ReconCheck) JourneyRecord {
	j.Rec.WallMS = round2(float64(time.Since(j.start).Microseconds()) / 1000)
	for r := range j.roles {
		j.Rec.Roles = append(j.Rec.Roles, r)
	}
	sortStrings(j.Rec.Roles)
	j.Rec.Recon = checks
	j.Rec.Summary = Summarize(j.Rec.Steps, j.calls)
	errs := 0
	for _, s := range j.Rec.Steps {
		errs += len(s.Errors)
	}
	j.Rec.Status = Verdict(errs, j.failed, checks)
	if j.Rec.Status == "fail" && j.Rec.Reason == "" {
		if j.failed {
			j.Rec.Reason = "a step failed"
		} else {
			j.Rec.Reason = "reconciliation did not tie"
		}
	}
	return j.Rec
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for k := i; k > 0 && s[k] < s[k-1]; k-- {
			s[k], s[k-1] = s[k-1], s[k]
		}
	}
}
