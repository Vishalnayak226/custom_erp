package releasegate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var today = time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)

const (
	sha1 = "5b4932f59a71344c12f248360e4cd2a73db9f1c2"
	sha2 = "0123456789abcdef0123456789abcdef01234567"
)

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func sign(e Exception, priv ed25519.PrivateKey) Exception {
	e.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, e.Payload()))
	return e
}

func okExc(sev string) Exception {
	return Exception{ID: "D-1", Severity: sev, Title: "slow report", Approver: "owner", ApprovedOn: "2026-11-01", Expires: "2026-12-01"}
}

func status(f ExceptionFile, pub ed25519.PublicKey) (Status, string) {
	r := CheckExceptions(f, pub, today)
	return r.Check.Status, r.Check.Detail
}

func TestEmptyRegisterPasses(t *testing.T) {
	if s, d := status(ExceptionFile{}, nil); s != Pass {
		t.Fatalf("%s %s", s, d)
	}
}

func TestP0AlwaysFailsEvenSigned(t *testing.T) {
	pub, priv := newKey(t)
	e := sign(okExc("P0"), priv)
	s, d := status(ExceptionFile{Exceptions: []Exception{e}}, pub)
	if s != Fail || !strings.Contains(d, "P0 cannot be carried") {
		t.Fatalf("%s %s", s, d)
	}
	// "critical" is the same thing
	e.Severity = "critical"
	if s, _ := status(ExceptionFile{Exceptions: []Exception{e}}, pub); s != Fail {
		t.Fatal("critical must fail like P0")
	}
}

func TestP1NeedsSignature(t *testing.T) {
	pub, priv := newKey(t)
	unsigned := okExc("P1")
	s, d := status(ExceptionFile{Exceptions: []Exception{unsigned}}, pub)
	if s != Fail || !strings.Contains(d, "releasesign sign") || !strings.Contains(d, "releasegate payload -id D-1") {
		t.Fatalf("unsigned P1 must fail and say what to run: %s %s", s, d)
	}
	signed := sign(unsigned, priv)
	if s, d := status(ExceptionFile{Exceptions: []Exception{signed}}, pub); s != Pass {
		t.Fatalf("signed P1: %s %s", s, d)
	}
	// "high" is P1
	h := okExc("high")
	if s, _ := status(ExceptionFile{Exceptions: []Exception{h}}, pub); s != Fail {
		t.Fatal("unsigned high must fail")
	}
}

func TestSignatureBreaksWhenEntryChangesOrKeyDiffers(t *testing.T) {
	pub, priv := newKey(t)
	signed := sign(okExc("P1"), priv)
	tampered := signed
	tampered.Expires = "2026-12-05" // extended after signing
	if s, d := status(ExceptionFile{Exceptions: []Exception{tampered}}, pub); s != Fail || !strings.Contains(d, "does not verify") {
		t.Fatalf("tampered: %s %s", s, d)
	}
	other, _ := newKey(t)
	if s, _ := status(ExceptionFile{Exceptions: []Exception{signed}}, other); s != Fail {
		t.Fatal("another key must fail")
	}
	if s, _ := status(ExceptionFile{Exceptions: []Exception{signed}}, nil); s != Fail {
		t.Fatal("no public key must fail a P1")
	}
}

func TestExpiredExceptionFails(t *testing.T) {
	pub, priv := newKey(t)
	for _, sev := range []string{"P1", "P2", "P3"} {
		e := okExc(sev)
		e.ApprovedOn, e.Expires = "2026-10-01", "2026-11-05" // yesterday
		e = sign(e, priv)
		s, d := status(ExceptionFile{Exceptions: []Exception{e}}, pub)
		if s != Fail || !strings.Contains(d, "EXPIRED") {
			t.Fatalf("%s: %s %s", sev, s, d)
		}
	}
	// the last day still counts
	e := okExc("P2")
	e.ApprovedOn, e.Expires = "2026-10-01", "2026-11-06"
	if s, d := status(ExceptionFile{Exceptions: []Exception{e}}, pub); s != Pass {
		t.Fatalf("expiry day: %s %s", s, d)
	}
}

func TestLowerRiskNeedsApproverAndExpiry(t *testing.T) {
	e := okExc("P2")
	e.Approver = ""
	if s, d := status(ExceptionFile{Exceptions: []Exception{e}}, nil); s != Fail || !strings.Contains(d, "no approver") {
		t.Fatalf("%s %s", s, d)
	}
	e = okExc("P3")
	e.Expires = ""
	if s, _ := status(ExceptionFile{Exceptions: []Exception{e}}, nil); s != Fail {
		t.Fatal("no expiry must fail")
	}
	if s, d := status(ExceptionFile{Exceptions: []Exception{okExc("P2")}}, nil); s != Pass {
		t.Fatalf("complete P2 must pass unsigned: %s %s", s, d)
	}
}

func TestExceptionLengthLimitsAndOddEntries(t *testing.T) {
	pub, priv := newKey(t)
	long := okExc("P1")
	long.Expires = "2027-03-01"
	if s, d := status(ExceptionFile{Exceptions: []Exception{sign(long, priv)}}, pub); s != Fail || !strings.Contains(d, "at most 30") {
		t.Fatalf("%s %s", s, d)
	}
	bad := okExc("P9")
	if s, _ := status(ExceptionFile{Exceptions: []Exception{bad}}, nil); s != Fail {
		t.Fatal("unknown severity must fail")
	}
	dup := okExc("P2")
	if s, d := status(ExceptionFile{Exceptions: []Exception{dup, dup}}, nil); s != Fail || !strings.Contains(d, "duplicate") {
		t.Fatalf("%s %s", s, d)
	}
	closed := okExc("P0")
	closed.Status = "closed"
	if s, _ := status(ExceptionFile{Exceptions: []Exception{closed}}, nil); s != Pass {
		t.Fatal("a closed P0 is resolved")
	}
}

func TestPayloadCoversEveryField(t *testing.T) {
	a := okExc("P1")
	for name, mut := range map[string]func(*Exception){
		"title":    func(e *Exception) { e.Title = "other" },
		"approver": func(e *Exception) { e.Approver = "x" },
		"expires":  func(e *Exception) { e.Expires = "2026-12-02" },
		"severity": func(e *Exception) { e.Severity = "P2" },
		"covers":   func(e *Exception) { e.Covers = []string{"browser_budget"} },
		"id":       func(e *Exception) { e.ID = "D-2" },
	} {
		b := a
		mut(&b)
		if string(a.Payload()) == string(b.Payload()) {
			t.Errorf("payload ignores %s", name)
		}
	}
}

func TestParsePublicKeyReadsReleasesignFormat(t *testing.T) {
	// release/signing.pub is the committed key; the same parse must read it.
	b, err := os.ReadFile(filepath.Join("..", "..", "release", "signing.pub"))
	if err != nil {
		t.Skip("no release/signing.pub in this tree")
	}
	if _, err := ParsePublicKey(b); err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePublicKey([]byte("junk")); err == nil {
		t.Fatal("junk must not parse")
	}
}

// ---------------------------------------------------------------- evidence

func ev(kind string) *Evidence {
	return &Evidence{Kind: kind, Date: "2026-11-01", Commit: sha1, Result: "pass", By: "ops"}
}

func ancestorOf(ok ...string) func(string) bool {
	return func(c string) bool {
		for _, o := range ok {
			if o == c {
				return true
			}
		}
		return false
	}
}

func TestEvidenceRules(t *testing.T) {
	warn := EvidencePolicy{Mode: ModeWarn, MaxAgeDays: 35}
	block := EvidencePolicy{Mode: ModeBlock, MaxAgeDays: 35}
	anc := ancestorOf(sha1)
	cases := []struct {
		name string
		ev   *Evidence
		pol  EvidencePolicy
		want Status
		has  string
	}{
		{"good", ev("backup_restore"), block, Pass, "passed"},
		{"missing warns", nil, warn, Warn, "no evidence"},
		{"missing blocks", nil, block, Fail, "releasegate record -kind backup_restore"},
		{"stale blocks", &Evidence{Kind: "backup_restore", Date: "2026-09-01", Commit: sha1, Result: "pass", By: "ops"}, block, Fail, "stale"},
		{"stale warns", &Evidence{Kind: "backup_restore", Date: "2026-09-01", Commit: sha1, Result: "pass", By: "ops"}, warn, Warn, "stale"},
		{"commit mismatch blocks", &Evidence{Kind: "backup_restore", Date: "2026-11-01", Commit: sha2, Result: "pass", By: "ops"}, block, Fail, "not in this release's history"},
		{"no commit", &Evidence{Kind: "backup_restore", Date: "2026-11-01", Result: "pass", By: "ops"}, block, Fail, "no commit"},
		{"failed run", &Evidence{Kind: "backup_restore", Date: "2026-11-01", Commit: sha1, Result: "fail", By: "ops"}, block, Fail, "did not pass"},
		{"future date", &Evidence{Kind: "backup_restore", Date: "2027-01-01", Commit: sha1, Result: "pass", By: "ops"}, block, Fail, "future"},
		{"wrong kind", ev("rollback"), block, Fail, "kind"},
		{"nobody ran it", &Evidence{Kind: "backup_restore", Date: "2026-11-01", Commit: sha1, Result: "pass"}, block, Fail, "who ran"},
	}
	for _, c := range cases {
		got := CheckEvidence("backup_restore", c.ev, c.pol, today, anc, false)
		if got.Status != c.want || !strings.Contains(got.Detail, c.has) {
			t.Errorf("%s: %s %q", c.name, got.Status, got.Detail)
		}
	}
	// a valid covering exception turns a known miss into "excepted", never into pass
	miss := &Evidence{Kind: "browser_budget", Date: "2026-11-01", Commit: sha1, Result: "fail", By: "ops"}
	got := CheckEvidence("browser_budget", miss, EvidencePolicy{Mode: ModeBlock, MaxAgeDays: 14}, today, anc, true)
	if got.Status != Excepted || !strings.Contains(got.Detail, "did not pass") {
		t.Fatalf("%s %s", got.Status, got.Detail)
	}
}

func TestExceptionCoversEvidenceOnlyWhenValid(t *testing.T) {
	pub, priv := newKey(t)
	in := baseInput(t)
	in.Policy.Evidence["browser_budget"] = EvidencePolicy{Mode: ModeBlock, MaxAgeDays: 14}
	in.Evidence["browser_budget"] = &Evidence{Kind: "browser_budget", Date: "2026-11-02", Commit: sha1, Result: "fail", By: "ops", Note: "LCP over budget"}
	in.PublicKey = pub

	// no exception: the known miss blocks (block mode)
	if rec := Evaluate(in); rec.Verdict != "fail" {
		t.Fatal("a known miss with no exception must fail in block mode")
	}
	// an unsigned P1 covering it is not valid, so it blocks twice
	e := okExc("P1")
	e.Covers = []string{"browser_budget"}
	in.Exceptions = &ExceptionFile{Exceptions: []Exception{e}}
	if rec := Evaluate(in); rec.Verdict != "fail" {
		t.Fatal("unsigned P1 must not cover")
	}
	// a P2 with approver and expiry covers it
	p2 := okExc("P2")
	p2.Covers = []string{"browser_budget"}
	in.Exceptions = &ExceptionFile{Exceptions: []Exception{p2}}
	rec := Evaluate(in)
	if rec.Verdict != "pass" {
		t.Fatalf("covered miss: %v", rec.Failures())
	}
	if got := find(rec, "evidence.browser_budget"); got.Status != Excepted {
		t.Fatalf("want excepted, got %s", got.Status)
	}
	// once it expires the same file blocks again
	in.Now = time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)
	if rec := Evaluate(in); rec.Verdict != "fail" {
		t.Fatal("expired exception must fail the gate")
	}
	_ = priv
}

// ------------------------------------------------------ whole-record rules

func baseInput(t *testing.T) Input {
	t.Helper()
	pol, err := ParsePolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	return Input{
		Now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), Version: "0.24.0", BaseCommit: sha1, Tree: sha2,
		Migrations: []Migration{{Name: "a.sql", SHA256: strings.Repeat("a", 64)}},
		SBOM:       SBOMRef{File: "m.json", SHA256: strings.Repeat("b", 64), Dependencies: 3},
		Config:     ConfigRef{EnvNames: []string{"JWT_SECRET"}, SHA256: strings.Repeat("c", 64)},
		Deps:       DepsRef{Licenses: map[string]int{"MIT": 1}},
		Surface:    Surface{Available: true},
		Results:    map[string]string{"build": "pass", "vet": "pass", "tests": "pass (10)"},
		Vuln:       Vuln{Ran: true},
		Policy:     pol, Exceptions: &ExceptionFile{}, Evidence: map[string]*Evidence{}, IsAncestor: ancestorOf(sha1), LimitsExist: true,
	}
}

func find(r Record, id string) Check {
	for _, c := range r.Checks {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func TestDefaultsWarnAndDoNotBlock(t *testing.T) {
	rec := Evaluate(baseInput(t))
	if rec.Verdict != "pass" {
		t.Fatalf("a candidate with no run evidence must still pass by default: %v", rec.Failures())
	}
	if rec.Warnings != len(EvidenceKinds) {
		t.Fatalf("every missing evidence kind must be a recorded warning, got %d", rec.Warnings)
	}
	if find(rec, "artifact").Status != Deferred {
		t.Fatal("artifact is deferred to Deploy")
	}
}

func TestFailFirstRules(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Input)
		id   string
	}{
		{"commit not a sha", func(i *Input) { i.BaseCommit = "HEAD" }, "commit"},
		{"tree missing", func(i *Input) { i.Tree = "" }, "commit"},
		{"missing migration checksum", func(i *Input) { i.Migrations = append(i.Migrations, Migration{Name: "b.sql"}) }, "migrations"},
		{"no migrations", func(i *Input) { i.Migrations = nil }, "migrations"},
		{"sbom failed", func(i *Input) { i.SBOM = SBOMRef{Error: "ledger unreadable"} }, "sbom"},
		{"licence missing", func(i *Input) { i.Deps.NoLicense = []string{"x/y"} }, "dependency_licenses"},
		{"vet not reported", func(i *Input) { delete(i.Results, "vet") }, "results"},
		{"tests failed", func(i *Input) { i.Results["tests"] = "fail" }, "results"},
		{"P0 present", func(i *Input) { i.Exceptions = &ExceptionFile{Exceptions: []Exception{okExc("P0")}} }, "exceptions"},
		{"register missing", func(i *Input) { i.Exceptions = nil }, "exceptions"},
		{"no config names", func(i *Input) { i.Config.EnvNames = nil }, "config_contract"},
	}
	for _, c := range tests {
		in := baseInput(t)
		c.mut(&in)
		rec := Evaluate(in)
		if rec.Verdict != "fail" || find(rec, c.id).Status != Fail {
			t.Errorf("%s: verdict %s, %s=%s", c.name, rec.Verdict, c.id, find(rec, c.id).Status)
		}
	}
	// and the unchanged input is green, so each failure above is the mutation's doing
	if rec := Evaluate(baseInput(t)); rec.Verdict != "pass" {
		t.Fatalf("control: %v", rec.Failures())
	}
}

// Item 177.2: with the tests on GitHub, the click reports build, vet and tests as
// "github ..."; that passes the gate, and anything else that is not "pass" still fails.
func TestResultsRunOnGitHubPass(t *testing.T) {
	in := baseInput(t)
	in.Results = map[string]string{"build": "github (release.yml)", "vet": "github (release.yml)", "tests": "github (16 shards)"}
	rec := Evaluate(in)
	if c := find(rec, "results"); rec.Verdict != "pass" || c.Status != Pass || !strings.Contains(c.Detail, "on GitHub") {
		t.Fatalf("github results: verdict %s, results=%s (%s)", rec.Verdict, c.Status, c.Detail)
	}
	in.Results["tests"] = "skipped"
	if c := find(Evaluate(in), "results"); c.Status != Fail {
		t.Fatalf("tests=skipped must fail, got %s", c.Status)
	}
}

func TestOptionalChecksFollowPolicy(t *testing.T) {
	in := baseInput(t)
	in.Vuln = Vuln{Ran: true, Findings: 2, Detail: "GO-2026-1, GO-2026-2"}
	in.Surface = Surface{Available: true, High: []string{"new open route"}}
	rec := Evaluate(in)
	if rec.Verdict != "pass" || find(rec, "dependency_scan").Status != Warn || find(rec, "surface_diff").Status != Warn {
		t.Fatal("default mode must warn, not block")
	}
	in.Policy.Govulncheck, in.Policy.SurfaceHighRisk = ModeBlock, ModeBlock
	rec = Evaluate(in)
	if rec.Verdict != "fail" || find(rec, "dependency_scan").Status != Fail || find(rec, "surface_diff").Status != Fail {
		t.Fatal("block mode must fail")
	}
	in = baseInput(t)
	in.Vuln = Vuln{Detail: "govulncheck is not installed on this machine"}
	if c := find(Evaluate(in), "dependency_scan"); c.Status != NotRun || !strings.Contains(c.Detail, "go install") {
		t.Fatalf("%s %s", c.Status, c.Detail)
	}
}

func TestPolicyParsing(t *testing.T) {
	p, err := ParsePolicy([]byte(`{"evidence":{"journey":{"mode":"block"}},"govulncheck":"block"}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Evidence["journey"].Mode != ModeBlock || p.Evidence["journey"].MaxAgeDays != 14 || p.Evidence["race"].Mode != ModeWarn || p.Govulncheck != ModeBlock {
		t.Fatalf("%+v", p)
	}
	if _, err := ParsePolicy([]byte(`{"govulncheck":"maybe"}`)); err == nil {
		t.Fatal("a mode other than warn/block must be refused")
	}
	if _, err := ParsePolicy([]byte(`{nope`)); err == nil {
		t.Fatal("bad JSON must be refused")
	}
}

func TestRecordBindsToCommitAndTree(t *testing.T) {
	rec := Evaluate(baseInput(t))
	if err := VerifyBinding(rec, sha1, sha2); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBinding(rec, sha2, sha2); err == nil || !strings.Contains(err.Error(), "base commit") {
		t.Fatalf("commit mismatch must be refused: %v", err)
	}
	if err := VerifyBinding(rec, sha1, sha1); err == nil || !strings.Contains(err.Error(), "tree") {
		t.Fatalf("tree mismatch must be refused: %v", err)
	}
	failed := baseInput(t)
	failed.Results["tests"] = "fail"
	if err := VerifyBinding(Evaluate(failed), sha1, sha2); err == nil {
		t.Fatal("a failed record is not evidence")
	}
	if js, err := rec.JSON(); err != nil || !strings.Contains(string(js), sha2) {
		t.Fatalf("json: %v", err)
	}
	if md := rec.Markdown(); !strings.Contains(md, "| exceptions |") || !strings.Contains(md, sha1) {
		t.Fatal("markdown must list the checks and the commit")
	}
}

// ---------------------------------------------------------------- collectors

func TestParseImpact(t *testing.T) {
	s := ParseImpact("HIGH-RISK (1):\n  ! new open route /x\nOther changes (2):\n  - a\n  - b\n", true)
	if !s.Available || len(s.High) != 1 || s.Other != 2 || s.High[0] != "new open route /x" {
		t.Fatalf("%+v", s)
	}
	if ParseImpact("", false).Available {
		t.Fatal("no file means not available")
	}
}

func TestScanEnv(t *testing.T) {
	got := map[string]bool{}
	scanEnv(`a := os.Getenv("JWT_SECRET"); v, ok := os.LookupEnv("DATABASE_URL"); x := envOr("PORT_NUMBER", "1"); y := os.Getenv("lower")`, got)
	if !got["JWT_SECRET"] || !got["DATABASE_URL"] || !got["PORT_NUMBER"] || got["lower"] {
		t.Fatalf("%v", got)
	}
}

func TestCollectMigrationsChecksumsAreLFNormalised(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "db", "migrations"), 0o755)
	os.WriteFile(filepath.Join(dir, "db", "migrations", "a.sql"), []byte("SELECT 1;\r\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "db", "migrations", "b.sql"), []byte("SELECT 1;\n"), 0o644)
	ms, set, _, err := CollectMigrations(dir, "")
	if err != nil || len(ms) != 2 || set == "" {
		t.Fatalf("%v %v", ms, err)
	}
	if ms[0].SHA256 != ms[1].SHA256 {
		t.Fatal("CRLF and LF copies must hash the same, as db.checksumOf does")
	}
	if c := CheckMigrations(ms, ""); c.Status != Pass {
		t.Fatal(c.Detail)
	}
}

func TestIsAncestorOnARealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	os.WriteFile(filepath.Join(dir, "f"), []byte("1"), 0o644)
	run("add", "f")
	run("commit", "-q", "-m", "one")
	first := run("rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "f"), []byte("2"), 0o644)
	run("commit", "-q", "-am", "two")
	second := run("rev-parse", "HEAD")
	if !IsAncestor(dir, second)(first) || !IsAncestor(dir, second)(second) {
		t.Fatal("history and self are ancestors")
	}
	if IsAncestor(dir, first)(second) {
		t.Fatal("a later commit is not in an earlier one's history")
	}
	if IsAncestor(dir, second)(sha2) {
		t.Fatal("an unknown commit is not an ancestor")
	}
}

func TestRunGovulncheckMissingToolIsNotRun(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	v := RunGovulncheck("", t.TempDir())
	if v.Ran {
		t.Fatal("no tool, nothing ran")
	}
}
