package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"custom_erp/internal/releasegate"
)

// journeyIDs are the seven journeys a complete run drives.
var journeyIDs = []string{"J1", "J2", "J3", "J4", "J5", "J6", "J7"}

// GateEvidence turns a finished journey run into the release gate's `journey`
// evidence (47.17.2 -> 47.17.3). It returns ok=false, with the reason, for a
// run that must not stand for the candidate: a subset of the journeys, or the
// fail-first run that injects a ledger mismatch on purpose. Anything else is
// pass when every journey and reconciliation passed, otherwise fail - a red
// run is recorded, never dropped, so the gate can see it.
func GateEvidence(ev Evidence, injected bool, commit, evidenceFile string, now time.Time) (releasegate.Evidence, bool, string) {
	if injected {
		return releasegate.Evidence{}, false, "the fail-first run (-inject-mismatch) is not evidence for a candidate"
	}
	ran := map[string]bool{}
	for _, j := range ev.Journeys {
		ran[strings.ToUpper(j.ID)] = true
	}
	for _, id := range journeyIDs {
		if !ran[id] {
			return releasegate.Evidence{}, false, "only a run of all seven journeys counts as release evidence (missing " + id + ")"
		}
	}
	result := "pass"
	var bad []string
	for _, j := range ev.Journeys {
		if j.Status != "pass" {
			bad = append(bad, j.ID+"="+j.Status)
		}
	}
	if ev.Overall != "pass" || len(bad) > 0 {
		result = "fail"
	}
	note := fmt.Sprintf("journeylab journey, %d journeys, overall %s, host %s; evidence %s", len(ev.Journeys), ev.Overall, ev.Host, evidenceFile)
	if len(bad) > 0 {
		note += "; not passing: " + strings.Join(bad, ", ")
	}
	return releasegate.Evidence{Kind: "journey", Date: now.UTC().Format("2006-01-02"), Commit: commit, Result: result, By: "journeylab", Note: note}, true, ""
}

func gitHead(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
