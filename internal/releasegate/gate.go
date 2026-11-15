// Package releasegate is the release-candidate gate (Stage 47.17.3 and
// 49.17.2): the rules that decide whether one exact candidate may ship, and
// the evidence record that says why. cmd/releasegate is the thin front end the
// release script calls.
//
// Principles, in the order they matter:
//
//   - Nothing passes silently. A check that was not run says "not-run", a
//     check with no evidence says so, and every record lists both.
//   - Anything new that could stop the existing release flow starts as a
//     warning (release/gate-policy.json says "warn"); switching a check to
//     "block" is one word in that file and a decision someone makes.
//   - A defect is accepted only through release/exceptions.json: P0 never, P1
//     only with an Ed25519 signature from the release key (the one
//     cmd/releasesign makes and release/signing.pub pins), lower-risk ones with
//     an approver and an expiry. An expired exception fails the gate.
package releasegate

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"custom_erp/internal/supplychain"
)

// Status of one check.
type Status string

const (
	Pass     Status = "pass"
	Warn     Status = "warn"     // recorded, does not stop the release
	Fail     Status = "fail"     // stops the release
	NotRun   Status = "not-run"  // could not be run here; the detail says why
	Deferred Status = "deferred" // happens later in the flow (Deploy); the detail says where
	Excepted Status = "excepted" // a valid exception covers it
)

// Check is one line of the evidence record.
type Check struct {
	ID          string `json:"id"`
	Requirement string `json:"requirement"`
	Gate        string `json:"gate,omitempty"` // the release-script gate that also covers it, if any
	Status      Status `json:"status"`
	Detail      string `json:"detail"`
}

// Mode says what a failing optional check does.
type Mode string

const (
	ModeWarn  Mode = "warn"
	ModeBlock Mode = "block"
	// ModeBlockAfterPass (162.20) is for run evidence only: it warns until the
	// kind has passed once, then blocks like ModeBlock. The first pass is the
	// evidence file's first_pass, which "releasegate record" carries forward.
	ModeBlockAfterPass Mode = "block_after_pass"
)

func (m Mode) bad() Status {
	if m == ModeBlock {
		return Fail
	}
	return Warn
}

// ---------------------------------------------------------------- policy

// EvidencePolicy is the rule for one kind of run evidence.
type EvidencePolicy struct {
	Mode       Mode `json:"mode"`
	MaxAgeDays int  `json:"max_age_days"`
}

// Policy is release/gate-policy.json. A missing file means everything warns.
type Policy struct {
	SchemaVersion   int                       `json:"schema_version"`
	Evidence        map[string]EvidencePolicy `json:"evidence"`
	Govulncheck     Mode                      `json:"govulncheck"`
	SurfaceHighRisk Mode                      `json:"surface_high_risk"`
	SupportedLimits Mode                      `json:"supported_limits"`
}

// EvidenceKinds are the run evidence the gate reads, with how to produce each.
var EvidenceKinds = []struct{ Kind, Need, How string }{
	{"backup_restore", "backup/restore drill", "run deploy/restore_drill.sh on the server (or Invoke-Deploy's -verify-restore)"},
	{"outbox_drill", "worker/outbox drill", "stop the worker mid-batch, restart, and confirm the outbox drains with no duplicate and no loss"},
	{"rollback", "rollback / roll-forward drill", "deploy the previous build back and forward on a scratch host (deploy/deploy.ps1 keeps the rollback copy)"},
	{"browser_budget", "performance / accessibility budgets", "run the browser budget suite (group d's perf suite) against the candidate"},
	{"journey", "journey evidence", "run the journey lab (cmd/journeylab) against the candidate"},
	{"race", "race test run", "go test -race on the concurrency packages (needs cgo)"},
	{"dynamic", "dynamic security test run", "the dynamic suite of the guide inspector against a scratch instance"},
}

// DefaultMaxAgeDays is how fresh run evidence must be when the policy is silent.
var DefaultMaxAgeDays = map[string]int{
	"backup_restore": 35, "outbox_drill": 35, "rollback": 35,
	"browser_budget": 14, "journey": 14, "race": 14, "dynamic": 35,
}

// ParsePolicy reads the policy; an empty input is the all-warn default.
func ParsePolicy(data []byte) (Policy, error) {
	var p Policy
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &p); err != nil {
			return p, fmt.Errorf("release/gate-policy.json: %w", err)
		}
	}
	norm := func(m Mode, name string) (Mode, error) {
		switch m {
		case "":
			return ModeWarn, nil
		case ModeWarn, ModeBlock:
			return m, nil
		}
		return m, fmt.Errorf("release/gate-policy.json: %s mode %q is not warn or block", name, m)
	}
	var err error
	if p.Govulncheck, err = norm(p.Govulncheck, "govulncheck"); err != nil {
		return p, err
	}
	if p.SurfaceHighRisk, err = norm(p.SurfaceHighRisk, "surface_high_risk"); err != nil {
		return p, err
	}
	if p.SupportedLimits, err = norm(p.SupportedLimits, "supported_limits"); err != nil {
		return p, err
	}
	if p.Evidence == nil {
		p.Evidence = map[string]EvidencePolicy{}
	}
	for _, k := range EvidenceKinds {
		e := p.Evidence[k.Kind]
		if e.Mode != ModeBlockAfterPass {
			if e.Mode, err = norm(e.Mode, "evidence."+k.Kind); err != nil {
				return p, err
			}
		}
		if e.MaxAgeDays <= 0 {
			e.MaxAgeDays = DefaultMaxAgeDays[k.Kind]
		}
		p.Evidence[k.Kind] = e
	}
	return p, nil
}

// ------------------------------------------------------------ exceptions

// Exception is one open defect in release/exceptions.json, and the approval
// that lets the release carry it.
type Exception struct {
	ID         string   `json:"id"`
	Severity   string   `json:"severity"` // P0..P3 (critical/high/medium/low accepted)
	Title      string   `json:"title"`
	Status     string   `json:"status,omitempty"` // open (default) or closed
	Approver   string   `json:"approver,omitempty"`
	ApprovedOn string   `json:"approved_on,omitempty"` // YYYY-MM-DD
	Expires    string   `json:"expires,omitempty"`     // YYYY-MM-DD, last day it counts
	Covers     []string `json:"covers,omitempty"`      // evidence kinds this accepts (e.g. browser_budget)
	Signature  string   `json:"signature,omitempty"`   // base64 Ed25519 over Payload()
}

// ExceptionFile is release/exceptions.json.
type ExceptionFile struct {
	SchemaVersion int         `json:"schema_version"`
	Exceptions    []Exception `json:"exceptions"`
}

// MaxExceptionDays is the longest an exception may run from its approval.
func MaxExceptionDays(sev string) int {
	if sev == "P1" {
		return 30
	}
	return 180
}

// NormalizeSeverity maps critical/high/medium/low and p0..p3 to P0..P3.
func NormalizeSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "p0", "critical":
		return "P0"
	case "p1", "high":
		return "P1"
	case "p2", "medium":
		return "P2"
	case "p3", "low":
		return "P3"
	}
	return ""
}

// Payload is the exact text a signature covers: change any field after
// signing and verification fails. Sign it with
//
//	releasegate payload -id <id> -out <file>
//	releasesign sign -key %USERPROFILE%\.erp-release\signing.key -in <file>
func (e Exception) Payload() []byte {
	clean := func(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), "\n", " ") }
	covers := append([]string(nil), e.Covers...)
	sort.Strings(covers)
	return []byte(fmt.Sprintf("erp-release-exception v1\nid=%s\nseverity=%s\ntitle=%s\napprover=%s\napproved_on=%s\nexpires=%s\ncovers=%s\n",
		clean(e.ID), NormalizeSeverity(e.Severity), clean(e.Title), clean(e.Approver), clean(e.ApprovedOn), clean(e.Expires), strings.Join(covers, ",")))
}

// ParseExceptions reads the register.
func ParseExceptions(data []byte) (ExceptionFile, error) {
	var f ExceptionFile
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("release/exceptions.json: %w", err)
	}
	return f, nil
}

// ParsePublicKey reads the PKIX PEM cmd/releasesign prints (release/signing.pub).
func ParsePublicKey(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("not a PUBLIC KEY PEM")
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("not an Ed25519 key")
	}
	return pub, nil
}

// SignedBy reports whether the exception carries a signature the key accepts.
func (e Exception) SignedBy(pub ed25519.PublicKey) bool {
	if len(pub) == 0 || e.Signature == "" {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(e.Signature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, e.Payload(), sig)
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func parseDay(s string) (time.Time, bool) {
	if !dateRe.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// ExceptionResult is the verdict for the register.
type ExceptionResult struct {
	Check Check
	// Valid is the id -> exception of every open exception that is in order
	// (approved, unexpired, signed where required): these may cover evidence.
	Valid map[string]Exception
	Views []ExceptionView
}

// ExceptionView is what the record shows of one exception.
type ExceptionView struct {
	ID, Severity, Title, Expires, Verdict string
}

// CheckExceptions applies every exceptions rule. today is a UTC day.
func CheckExceptions(f ExceptionFile, pub ed25519.PublicKey, today time.Time) ExceptionResult {
	res := ExceptionResult{Valid: map[string]Exception{}}
	var problems []string
	open, accepted := 0, 0
	seen := map[string]bool{}
	for _, e := range f.Exceptions {
		sev := NormalizeSeverity(e.Severity)
		view := ExceptionView{ID: e.ID, Severity: sev, Title: e.Title, Expires: e.Expires}
		st := strings.ToLower(strings.TrimSpace(e.Status))
		if st == "closed" {
			view.Verdict = "closed"
			res.Views = append(res.Views, view)
			continue
		}
		open++
		bad := func(format string, a ...interface{}) {
			msg := fmt.Sprintf(format, a...)
			problems = append(problems, fmt.Sprintf("%s (%s): %s", e.ID, sev, msg))
			view.Verdict = "FAIL: " + msg
		}
		switch {
		case strings.TrimSpace(e.ID) == "":
			problems = append(problems, "an exception has no id")
			view.Verdict = "FAIL: no id"
		case seen[e.ID]:
			bad("duplicate id")
		case st != "" && st != "open":
			bad("status %q is not open or closed", e.Status)
		case sev == "":
			bad("severity %q is not P0, P1, P2 or P3", e.Severity)
		case strings.TrimSpace(e.Title) == "":
			bad("no title")
		case sev == "P0":
			bad("a P0 cannot be carried by an exception - fix it, or do not release")
		default:
			problems = append(problems, exceptionProblems(e, sev, pub, today, &view)...)
		}
		seen[e.ID] = true
		if view.Verdict == "" {
			view.Verdict = "accepted"
			accepted++
			res.Valid[e.ID] = e
		}
		res.Views = append(res.Views, view)
	}
	switch {
	case len(problems) > 0:
		res.Check = Check{ID: "exceptions", Status: Fail, Detail: fmt.Sprintf("%d open, %d accepted; %s", open, accepted, strings.Join(problems, "; "))}
	default:
		res.Check = Check{ID: "exceptions", Status: Pass, Detail: fmt.Sprintf("%d open, all accepted (approver + expiry%s); P0/P1 unresolved: 0", open, ", P1 signed")}
		if open == 0 {
			res.Check.Detail = "no open defects in release/exceptions.json; P0/P1 unresolved: 0"
		}
	}
	res.Check.Requirement = "zero P0 / zero unresolved P1 (critical/high); lower-risk exceptions approved and expiring"
	return res
}

// exceptionProblems checks approver, dates, signature for a P1..P3 entry.
// It returns the problems and records a verdict on view when there are any.
func exceptionProblems(e Exception, sev string, pub ed25519.PublicKey, today time.Time, view *ExceptionView) []string {
	var out []string
	add := func(format string, a ...interface{}) {
		msg := fmt.Sprintf(format, a...)
		out = append(out, fmt.Sprintf("%s (%s): %s", e.ID, sev, msg))
		if view.Verdict == "" {
			view.Verdict = "FAIL: " + msg
		}
	}
	if strings.TrimSpace(e.Approver) == "" {
		add("no approver")
	}
	approved, okA := parseDay(e.ApprovedOn)
	expires, okE := parseDay(e.Expires)
	switch {
	case !okA:
		add("approved_on must be YYYY-MM-DD")
	case !okE:
		add("expires must be YYYY-MM-DD")
	case expires.Before(today):
		add("EXPIRED on %s - renew it or fix the defect", e.Expires)
	case expires.Sub(approved) > time.Duration(MaxExceptionDays(sev))*24*time.Hour:
		add("runs %d days from approval; %s may run at most %d", int(expires.Sub(approved).Hours()/24), sev, MaxExceptionDays(sev))
	case approved.After(today.Add(24 * time.Hour)):
		add("approved_on %s is in the future", e.ApprovedOn)
	}
	if sev == "P1" && !e.SignedBy(pub) {
		if e.Signature == "" {
			add("a P1 needs a release-key signature. Run: releasegate payload -id %s -out p.txt ; releasesign sign -key %%USERPROFILE%%\\.erp-release\\signing.key -in p.txt  - and paste the line into this entry's \"signature\"", e.ID)
		} else {
			add("signature does not verify against release/signing.pub (an entry changed after signing, or another key) - sign it again")
		}
	}
	return out
}

// ----------------------------------------------------------- run evidence

// Evidence is release/evidence/<kind>.json: proof that a run happened.
type Evidence struct {
	Kind   string `json:"kind"`
	Date   string `json:"date"`   // YYYY-MM-DD the run finished
	Commit string `json:"commit"` // commit the run tested
	Result string `json:"result"` // pass or fail
	By     string `json:"by"`
	Note   string `json:"note,omitempty"`
	// FirstPass is the day this kind first passed (162.20); a later failed
	// run keeps it, which is what turns block_after_pass into a block.
	FirstPass string `json:"first_pass,omitempty"`
}

// CarryFirstPass gives a new run the first-pass day of the run it replaces,
// or its own day when it is the first pass.
func CarryFirstPass(prev *Evidence, ev Evidence) Evidence {
	if prev != nil && prev.Kind == ev.Kind {
		ev.FirstPass = prev.FirstPass
		if ev.FirstPass == "" && prev.Result == "pass" {
			ev.FirstPass = prev.Date
		}
	}
	if ev.FirstPass == "" && ev.Result == "pass" {
		ev.FirstPass = ev.Date
	}
	return ev
}

// CheckEvidence judges one kind. ev is nil when the file is missing.
// isAncestor says whether a commit is the release base or in its history;
// covered is true when a valid exception lists this kind in "covers".
func CheckEvidence(kind string, ev *Evidence, pol EvidencePolicy, today time.Time, isAncestor func(string) bool, covered bool) Check {
	need, how := kind, ""
	for _, k := range EvidenceKinds {
		if k.Kind == kind {
			need, how = k.Need, k.How
		}
	}
	c := Check{ID: "evidence." + kind, Requirement: need}
	mode := pol.Mode
	if mode == ModeBlockAfterPass {
		mode = ModeWarn
		if ev != nil && ev.Kind == kind && (ev.FirstPass != "" || ev.Result == "pass") {
			mode = ModeBlock
		}
	}
	problem := ""
	switch {
	case ev == nil:
		problem = fmt.Sprintf("no evidence. Produce it: %s; then record it: releasegate record -kind %s -result pass -by <name>", how, kind)
	case ev.Kind != kind:
		problem = fmt.Sprintf("file says kind %q", ev.Kind)
	default:
		d, ok := parseDay(ev.Date)
		switch {
		case !ok:
			problem = "date must be YYYY-MM-DD"
		case d.After(today.Add(24 * time.Hour)):
			problem = "date " + ev.Date + " is in the future"
		case int(today.Sub(d).Hours()/24) > pol.MaxAgeDays:
			problem = fmt.Sprintf("stale: run on %s, older than %d days", ev.Date, pol.MaxAgeDays)
		case strings.TrimSpace(ev.By) == "":
			problem = "no 'by' (who ran it)"
		case !isHexCommit(ev.Commit):
			problem = "no commit recorded"
		case !isAncestor(ev.Commit):
			problem = "commit " + short(ev.Commit) + " is not in this release's history (evidence for another commit)"
		case ev.Result != "pass":
			problem = fmt.Sprintf("the run on %s did not pass (result %q)", ev.Date, ev.Result)
		}
	}
	if problem == "" {
		c.Status = Pass
		c.Detail = fmt.Sprintf("passed %s by %s at %s", ev.Date, ev.By, short(ev.Commit))
		return c
	}
	if covered {
		c.Status = Excepted
		c.Detail = problem + " - covered by an accepted exception"
		return c
	}
	c.Status = mode.bad()
	c.Detail = problem
	if mode != ModeBlock {
		if pol.Mode == ModeBlockAfterPass {
			c.Detail += " (policy: warn until the first pass, then block)"
		} else {
			c.Detail += " (policy: warn)"
		}
	}
	return c
}

// ------------------------------------------------------------ small rules

func isHexCommit(s string) bool {
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func short(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// CheckCommit: the record names the exact base commit and the exact tree.
func CheckCommit(base, tree string) Check {
	c := Check{ID: "commit", Requirement: "exact commit", Gate: "G11"}
	switch {
	case !isHexCommit(base):
		c.Status, c.Detail = Fail, fmt.Sprintf("base commit %q is not a 40-character sha", base)
	case !isHexCommit(tree):
		c.Status, c.Detail = Fail, fmt.Sprintf("tree %q is not a 40-character sha", tree)
	default:
		c.Status = Pass
		c.Detail = fmt.Sprintf("candidate = tree %s on base %s (G11 commits only this tree; the new commit sha exists after G11)", short(tree), short(base))
	}
	return c
}

// Migration is one db/migrations/*.sql file and its LF-normalised sha256 (the value
// db.checksumOf stores in schema_migrations.checksum).
type Migration struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// CheckMigrations: every migration has a checksum.
func CheckMigrations(ms []Migration, changed string) Check {
	c := Check{ID: "migrations", Requirement: "exact migrations with checksums", Gate: "G10"}
	if len(ms) == 0 {
		c.Status, c.Detail = Fail, "no migration files found under db/"
		return c
	}
	var missing []string
	for _, m := range ms {
		if !sha256Re.MatchString(m.SHA256) {
			missing = append(missing, m.Name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		c.Status, c.Detail = Fail, fmt.Sprintf("%d migration(s) without a valid checksum: %s", len(missing), strings.Join(firstN(missing, 5), ", "))
		return c
	}
	c.Status = Pass
	c.Detail = fmt.Sprintf("%d migrations, each with a sha256. %s", len(ms), changed)
	return c
}

// CheckResults: build, vet and tests as the release already ran them. A result
// that starts with "github" (item 177.2) runs after the push, in
// .github/workflows/release.yml, which moves main to the commit only when it
// passes; the Deploy click deploys only what main holds.
func CheckResults(results map[string]string) Check {
	c := Check{ID: "results", Requirement: "clean build / vet / tests", Gate: "G8, G10"}
	var bad, ok []string
	onGitHub := false
	for _, k := range []string{"build", "vet", "tests"} {
		v, has := results[k]
		switch {
		case !has:
			bad = append(bad, k+" not reported")
		case strings.HasPrefix(v, "pass"):
			ok = append(ok, k+"="+v)
		case strings.HasPrefix(v, "github"):
			ok, onGitHub = append(ok, k+"="+v), true
		default:
			bad = append(bad, k+"="+v)
		}
	}
	if len(bad) > 0 {
		c.Status, c.Detail = Fail, strings.Join(bad, "; ")
		return c
	}
	c.Status, c.Detail = Pass, strings.Join(ok, "; ")+" (taken from the release's own run, not re-run)"
	if onGitHub {
		c.Detail = strings.Join(ok, "; ") + " (run on GitHub before main moves to this commit)"
	}
	return c
}

// Surface is the read of securityimpact.
type Surface struct {
	Available bool
	High      []string
	Other     int
}

// CheckSurface: route / permission / surface diff against the previous release.
func CheckSurface(s Surface, mode Mode) Check {
	c := Check{ID: "surface_diff", Requirement: "route / permission / surface diff", Gate: "G6, G6b"}
	switch {
	case !s.Available:
		c.Status, c.Detail = NotRun, "no previous attack_surface.json to compare, or no impact output was given"
	case len(s.High) > 0:
		c.Status = mode.bad()
		c.Detail = fmt.Sprintf("%d high-risk change(s), %d other; a security owner reads each in the G6b report", len(s.High), s.Other)
		if mode != ModeBlock {
			c.Detail += " (policy: warn)"
		}
	default:
		c.Status, c.Detail = Pass, fmt.Sprintf("no high-risk change; %d other change(s)", s.Other)
	}
	return c
}

// Vuln is the outcome of dependency scanning.
type Vuln struct {
	Ran      bool
	Findings int
	Detail   string
}

// CheckVuln: dependency vulnerability scan (govulncheck).
func CheckVuln(v Vuln, mode Mode) Check {
	c := Check{ID: "dependency_scan", Requirement: "dependency / secret / static results"}
	switch {
	case !v.Ran:
		c.Status = NotRun
		c.Detail = v.Detail + " - install: go install golang.org/x/vuln/cmd/govulncheck@latest"
	case v.Findings > 0:
		c.Status = mode.bad()
		c.Detail = fmt.Sprintf("govulncheck: %d vulnerabilit%s reachable from this code. %s", v.Findings, map[bool]string{true: "y", false: "ies"}[v.Findings == 1], v.Detail)
		if mode != ModeBlock {
			c.Detail += " (policy: warn; file each as an exception or fix it)"
		}
	default:
		c.Status, c.Detail = Pass, "govulncheck: no vulnerability reachable from this code"
	}
	return c
}

// CheckLimits: the supported-limits statement exists.
func CheckLimits(present bool, mode Mode) Check {
	c := Check{ID: "supported_limits", Requirement: "supported limits stated"}
	if present {
		c.Status, c.Detail = Pass, "docs/operations/service-operations.md, section Availability model (limits read from the code, with proposed targets)"
		return c
	}
	c.Status, c.Detail = mode.bad(), "docs/operations/service-operations.md has no \"Availability model\" section (the supported-limits statement)"
	return c
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// VerifyBinding: a record is only evidence for the commit and tree it names.
func VerifyBinding(rec Record, base, tree string) error {
	if rec.BaseCommit != base {
		return fmt.Errorf("record is for base commit %s, not %s", short(rec.BaseCommit), short(base))
	}
	if tree != "" && rec.Tree != tree {
		return fmt.Errorf("record is for tree %s, not %s", short(rec.Tree), short(tree))
	}
	if rec.Verdict != "pass" {
		return fmt.Errorf("record verdict is %q", rec.Verdict)
	}
	return nil
}

// Short is the 7-character form of a sha.
func Short(s string) string { return short(s) }

// HashArtifact hashes one build output for the record.
func HashArtifact(path string) (Artifact, error) {
	h, err := supplychain.HashFile(path)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{SHA256: h.SHA256, Size: h.SizeBytes}, nil
}
