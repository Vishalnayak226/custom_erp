package releasegate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WriteEvidence stores one run as <root>/release/evidence/<kind>.json, the
// file LoadEvidence and CheckEvidence read. It keeps the day the kind first
// passed through later runs (162.20), so a failed run never erases it. Every
// producer (releasegate record, journeylab, the restore drill's ingest, the
// browser budget run) writes through here.
func WriteEvidence(root string, ev Evidence) (string, error) {
	known := false
	for _, k := range EvidenceKinds {
		known = known || k.Kind == ev.Kind
	}
	if !known {
		return "", fmt.Errorf("unknown evidence kind %q", ev.Kind)
	}
	if ev.Result != "pass" && ev.Result != "fail" {
		return "", fmt.Errorf("result must be pass or fail, not %q", ev.Result)
	}
	if ev.By == "" || ev.Date == "" {
		return "", fmt.Errorf("evidence needs who ran it and the day")
	}
	dir := filepath.Join(root, "release", "evidence")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, ev.Kind+".json")
	var prev *Evidence
	if old, err := os.ReadFile(p); err == nil {
		var e Evidence
		if json.Unmarshal(old, &e) == nil {
			prev = &e
		}
	}
	ev = CarryFirstPass(prev, ev)
	b, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return "", err
	}
	return p, os.WriteFile(p, append(b, '\n'), 0o644)
}

// ParseEvidenceFile reads an evidence record another program wrote (the
// restore drill writes one on the server, where there is no git checkout and
// so no commit to name). An empty commit takes defaultCommit: the caller says
// which build the run tested.
func ParseEvidenceFile(b []byte, defaultCommit string) (Evidence, error) {
	var ev Evidence
	if err := json.Unmarshal(b, &ev); err != nil {
		return ev, fmt.Errorf("evidence file: %w", err)
	}
	if ev.Commit == "" {
		ev.Commit = defaultCommit
	}
	if ev.Commit == "" {
		return ev, fmt.Errorf("evidence file names no commit and none was given")
	}
	return ev, nil
}

// FullCommit returns c as the full 40-character hash the gate checks
// (isHexCommit). A short hash, such as the ERP_COMMIT the restore drill
// writes on the server, is expanded by git in the first of dirs that knows it.
// A commit git cannot expand is refused, so `releasegate record` never writes
// evidence the gate would turn down as "no commit recorded" (ask 327).
func FullCommit(c string, dirs ...string) (string, error) {
	c = strings.TrimSpace(c)
	if isHexCommit(c) {
		return c, nil
	}
	if c != "" {
		for _, d := range dirs {
			out, err := exec.Command("git", "-C", d, "rev-parse", "--verify", "--quiet", c+"^{commit}").Output()
			if s := strings.TrimSpace(string(out)); err == nil && isHexCommit(s) {
				return s, nil
			}
		}
	}
	return "", fmt.Errorf("commit %q is not a full hash and git cannot expand it here; the release gate needs the full 40-character hash", c)
}
