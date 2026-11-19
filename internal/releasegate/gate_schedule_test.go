package releasegate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBlockAfterPass (162.20): a kind on block_after_pass warns until it has
// passed once, then blocks - even when the latest run failed or went stale.
func TestBlockAfterPass(t *testing.T) {
	pol := EvidencePolicy{Mode: ModeBlockAfterPass, MaxAgeDays: 35}
	anc := ancestorOf(sha1)
	cases := []struct {
		name string
		ev   *Evidence
		want Status
		has  string
	}{
		{"never run warns", nil, Warn, "warn until the first pass"},
		{"first run failed warns", &Evidence{Kind: "rollback", Date: "2026-11-01", Commit: sha1, Result: "fail", By: "ops"}, Warn, "did not pass"},
		{"a pass passes", &Evidence{Kind: "rollback", Date: "2026-11-01", Commit: sha1, Result: "pass", By: "ops", FirstPass: "2026-11-01"}, Pass, "passed"},
		{"failed after a pass blocks", &Evidence{Kind: "rollback", Date: "2026-11-05", Commit: sha1, Result: "fail", By: "ops", FirstPass: "2026-10-20"}, Fail, "did not pass"},
		{"stale pass blocks", &Evidence{Kind: "rollback", Date: "2026-09-01", Commit: sha1, Result: "pass", By: "ops"}, Fail, "stale"},
	}
	for _, c := range cases {
		got := CheckEvidence("rollback", c.ev, pol, today, anc, false)
		if got.Status != c.want || !strings.Contains(got.Detail, c.has) {
			t.Errorf("%s: %s %q", c.name, got.Status, got.Detail)
		}
	}

	// The policy file accepts the mode for evidence, and only there.
	p, err := ParsePolicy([]byte(`{"evidence":{"rollback":{"mode":"block_after_pass"}}}`))
	if err != nil || p.Evidence["rollback"].Mode != ModeBlockAfterPass || p.Evidence["rollback"].MaxAgeDays != 35 {
		t.Fatalf("parse: %v %+v", err, p.Evidence["rollback"])
	}
	if _, err := ParsePolicy([]byte(`{"govulncheck":"block_after_pass"}`)); err == nil {
		t.Error("govulncheck accepted block_after_pass")
	}

	// record carries the first pass forward through a later failure.
	first := CarryFirstPass(nil, Evidence{Kind: "rollback", Date: "2026-10-20", Result: "pass"})
	if first.FirstPass != "2026-10-20" {
		t.Fatalf("first pass %q", first.FirstPass)
	}
	failed := CarryFirstPass(&first, Evidence{Kind: "rollback", Date: "2026-11-05", Result: "fail"})
	if failed.FirstPass != "2026-10-20" {
		t.Fatalf("a failed run lost the first pass: %q", failed.FirstPass)
	}
	if never := CarryFirstPass(nil, Evidence{Kind: "rollback", Date: "2026-11-05", Result: "fail"}); never.FirstPass != "" {
		t.Fatalf("a failed first run has a first pass %q", never.FirstPass)
	}
	// A file written before first_pass existed still counts its pass.
	old := &Evidence{Kind: "rollback", Date: "2026-10-01", Result: "pass"}
	if got := CarryFirstPass(old, Evidence{Kind: "rollback", Date: "2026-11-05", Result: "fail"}); got.FirstPass != "2026-10-01" {
		t.Fatalf("an older pass was not carried: %q", got.FirstPass)
	}
}

// TestShippedPolicyFollowsTheApprovedSchedule (162.20): the drills block after
// one pass, browser budgets and journeys once their evidence first passes;
// race and dynamic stay warn. govulncheck blocks since 162.1 (Go 1.27.4).
func TestShippedPolicyFollowsTheApprovedSchedule(t *testing.T) {
	b, err := os.ReadFile("../../release/gate-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePolicy(b)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Mode{
		"backup_restore": ModeBlockAfterPass, "outbox_drill": ModeBlockAfterPass, "rollback": ModeBlockAfterPass,
		"browser_budget": ModeBlockAfterPass, "journey": ModeBlockAfterPass,
		"race": ModeWarn, "dynamic": ModeWarn,
	}
	for kind, m := range want {
		if got := p.Evidence[kind].Mode; got != m {
			t.Errorf("%s: %q, want %q", kind, got, m)
		}
	}
	if p.Govulncheck != ModeBlock {
		t.Errorf("govulncheck: %q, want %q (162.1 landed)", p.Govulncheck, ModeBlock)
	}
}

// WriteEvidence is the one writer behind `releasegate record`, journeylab and
// the other producers: it validates, and a failed run keeps the first-pass day.
func TestWriteEvidenceKeepsFirstPassAndRejectsBadInput(t *testing.T) {
	root := t.TempDir()
	ev := Evidence{Kind: "journey", Date: "2026-11-01", Commit: "abc", Result: "pass", By: "journeylab"}
	p, err := WriteEvidence(root, ev)
	if err != nil {
		t.Fatal(err)
	}
	ev.Date, ev.Result = "2026-11-10", "fail"
	if _, err := WriteEvidence(root, ev); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadEvidence(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	got := loaded["journey"]
	if got == nil || got.Result != "fail" || got.FirstPass != "2026-11-01" {
		t.Fatalf("a failed later run must keep the first pass day: %+v", got)
	}
	for _, bad := range []Evidence{
		{Kind: "nope", Date: "2026-11-01", Result: "pass", By: "x"},
		{Kind: "journey", Date: "2026-11-01", Result: "ok", By: "x"},
		{Kind: "journey", Date: "2026-11-01", Result: "pass"},
	} {
		if _, err := WriteEvidence(root, bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

// The restore drill writes its record on the server with no commit; ingesting
// it names the deployed build, and the result keeps its failures.
func TestParseEvidenceFileFillsTheCommitAndKeepsAFailure(t *testing.T) {
	failed := []byte(`{"kind":"backup_restore","date":"2026-11-12","commit":"","result":"fail","by":"restore_drill.sh@app","note":"checksum mismatch"}`)
	ev, err := ParseEvidenceFile(failed, "abc1234")
	if err != nil || ev.Commit != "abc1234" || ev.Result != "fail" {
		t.Fatalf("%+v %v", ev, err)
	}
	if _, err := ParseEvidenceFile(failed, ""); err == nil {
		t.Error("an evidence file with no commit and no default must be refused")
	}
	if _, err := ParseEvidenceFile([]byte("not json"), "abc"); err == nil {
		t.Error("garbage accepted")
	}
	root := t.TempDir()
	pass := ev
	pass.Result, pass.Date = "pass", "2026-11-01"
	if _, err := WriteEvidence(root, pass); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteEvidence(root, ev); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadEvidence(filepath.Join(root, "release", "evidence"))
	if g := got["backup_restore"]; g.Result != "fail" || g.FirstPass != "2026-11-01" {
		t.Fatalf("a failed drill after a pass must stay a fail with the first pass kept: %+v", g)
	}
}

// TestGitHubKindsAreDeferredAndJudged (47.9.4, 47.17.2): the kinds release.yml's
// evidence job makes are deferred in the click's record, never read there, and
// the job's judge applies the click's own rules, policy and exceptions - so it
// fails only when the gate would block.
func TestGitHubKindsAreDeferredAndJudged(t *testing.T) {
	in := baseInput(t)
	for _, k := range []string{"browser_budget", "journey"} {
		in.Policy.Evidence[k] = EvidencePolicy{Mode: ModeBlockAfterPass, MaxAgeDays: 14}
	}
	missAfterPass := &Evidence{Kind: "browser_budget", Date: "2026-11-05", Commit: sha1, Result: "fail", By: "perf-budget.cjs", FirstPass: "2026-11-01"}
	in.Evidence["browser_budget"] = missAfterPass
	if c := find(Evaluate(in), "evidence.browser_budget"); c.Status != Fail {
		t.Fatalf("read by the click, a miss after a pass must block: %s %q", c.Status, c.Detail)
	}
	in.GitHubKinds = map[string]bool{"browser_budget": true, "journey": true}
	rec := Evaluate(in)
	for _, k := range []string{"browser_budget", "journey"} {
		if c := find(rec, "evidence."+k); c.Status != Deferred || !strings.Contains(c.Detail, "release.yml") {
			t.Errorf("%s made on GitHub must be deferred to that job: %s %q", k, c.Status, c.Detail)
		}
	}
	if rec.Verdict != "pass" {
		t.Fatalf("deferred kinds must not fail the click: %v", rec.Failures())
	}
	if c := find(rec, "evidence.rollback"); c.Status != Warn {
		t.Fatalf("every other kind is still read from release/evidence: %s", c.Status)
	}

	cases := []struct {
		name string
		ev   *Evidence
		want Status
	}{
		{"no run yet warns", nil, Warn},
		{"a first run that missed warns", &Evidence{Kind: "browser_budget", Date: "2026-11-05", Commit: sha1, Result: "fail", By: "perf-budget.cjs"}, Warn},
		{"a pass passes", &Evidence{Kind: "browser_budget", Date: "2026-11-05", Commit: sha1, Result: "pass", By: "perf-budget.cjs", FirstPass: "2026-11-05"}, Pass},
		{"a miss after a pass blocks", missAfterPass, Fail},
		{"a pass for another commit blocks once it has passed", &Evidence{Kind: "browser_budget", Date: "2026-11-05", Commit: sha2, Result: "pass", By: "perf-budget.cjs", FirstPass: "2026-11-01"}, Fail},
	}
	for _, c := range cases {
		in.Evidence = map[string]*Evidence{}
		if c.ev != nil {
			in.Evidence["browser_budget"] = c.ev
		}
		got := JudgeEvidence(in, []string{"browser_budget"})
		if len(got) != 1 || got[0].ID != "evidence.browser_budget" || got[0].Status != c.want {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
	// A valid exception covering the kind excepts a known miss in the judge too.
	p2 := okExc("P2")
	p2.Covers = []string{"browser_budget"}
	in.Exceptions = &ExceptionFile{Exceptions: []Exception{p2}}
	in.Evidence = map[string]*Evidence{"browser_budget": missAfterPass}
	if got := JudgeEvidence(in, []string{"browser_budget", "journey"}); got[0].Status != Excepted || got[1].Status != Warn {
		t.Fatalf("covered miss and missing journey: %+v", got)
	}
}
