package supplychain

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Stage 47.18.4: the architecture exception gate. A dependency or process
// added to this build after GateOpened has to earn its place in the ledger
// with evidence - the bottleneck it was measured to relieve, what failure it
// makes better, what it costs to run, secure and license, how to roll it
// back and how to take it out again. "Enterprise systems normally use it" is
// not evidence.

// GateOpened is the day the gate started applying.
const GateOpened = "2026-10-27"

// ArchitectureGate is the case for one dependency or process.
type ArchitectureGate struct {
	MeasuredBottleneck string `json:"measured_bottleneck"`
	FailureModeBenefit string `json:"failure_mode_benefit"`
	ResourceBudget     string `json:"resource_budget"`
	OpsBudget          string `json:"ops_budget"`
	SecurityBudget     string `json:"security_budget"`
	LicenseBudget      string `json:"license_budget"`
	MigrationRollback  string `json:"migration_rollback"`
	RemovalPlan        string `json:"removal_plan"`
	Date               string `json:"date"`
}

// grandfatheredAtGateOpening is every ledger entry that existed on
// GateOpened, frozen here so the marker cannot be copied onto a newcomer: an
// entry claiming to be grandfathered that is not on this list fails. A
// version bump of one of these keeps its standing; a new name never gets it.
var grandfatheredAtGateOpening = map[string]bool{
	"github.com/lib/pq":                      true,
	"golang.org/x/crypto":                    true,
	"go":                                     true,
	"actions/checkout":                       true,
	"actions/setup-go":                       true,
	"actions/upload-artifact":                true,
	"actions/cache":                          true,
	"gitleaks/gitleaks-action":               true,
	"golang.org/x/vuln/cmd/govulncheck":      true,
	"ubuntu runner image":                    true,
	"postgres (CI service container)":        true,
	"golang (Dockerfile builder base image)": true,
	"gcr.io/distroless/static-debian12":      true,
	"systemd:erp.service":                    true,
	"systemd:postgresql.service":             true,
	"caddy":                                  true,
	"compose:postgres":                       true,
	"compose:app":                            true,
	"cron:install_backup_cron.sh":            true,
	"cron:install_cleanup_cron.sh":           true,
	// public/qrcode.min.js has been in the tree since Stage 20 (commit 7f2ca14),
	// long before the gate opened; it joined the ledger when vendored assets did.
	"public/qrcode.min.js": true,
}

// appealToCustom catches a gate field that argues from habit, not evidence.
var appealToCustom = regexp.MustCompile(`(?i)normally use|industry standard|best practice|everyone uses|commonly used|enterprise systems`)

var hasDigit = regexp.MustCompile(`[0-9]`)

// ValidateArchitectureGate returns one problem per ledger entry that has
// neither a complete gate nor a genuine grandfathered marker.
func ValidateArchitectureGate(ledger *Ledger) []string {
	var problems []string
	for _, e := range ledger.Entries {
		frozen := grandfatheredAtGateOpening[e.Name]
		marked := strings.TrimSpace(e.Grandfathered) != ""
		switch {
		case e.Gate != nil:
			if marked {
				problems = append(problems, fmt.Sprintf("%s: carries both a grandfathered marker and an architecture_gate - keep one", e.Name))
			}
			problems = append(problems, gateProblems(e.Name, e.Gate)...)
		case marked && !frozen:
			problems = append(problems, fmt.Sprintf("%s: marked grandfathered, but it was not in the ledger when the gate opened on %s - fill architecture_gate instead", e.Name, GateOpened))
		case frozen && !marked:
			problems = append(problems, fmt.Sprintf("%s: existed before the gate but has lost its grandfathered marker - restore it or fill architecture_gate", e.Name))
		case !frozen:
			problems = append(problems, fmt.Sprintf("%s: added since the gate opened on %s with no architecture_gate - record the measured bottleneck, failure-mode benefit, resource/ops/security/license budget, migration/rollback and removal plan", e.Name, GateOpened))
		}
	}
	return problems
}

func gateProblems(name string, g *ArchitectureGate) []string {
	var problems []string
	fields := []struct{ key, val string }{
		{"measured_bottleneck", g.MeasuredBottleneck},
		{"failure_mode_benefit", g.FailureModeBenefit},
		{"resource_budget", g.ResourceBudget},
		{"ops_budget", g.OpsBudget},
		{"security_budget", g.SecurityBudget},
		{"license_budget", g.LicenseBudget},
		{"migration_rollback", g.MigrationRollback},
		{"removal_plan", g.RemovalPlan},
		{"date", g.Date},
	}
	for _, f := range fields {
		v := strings.TrimSpace(f.val)
		switch {
		case v == "":
			problems = append(problems, fmt.Sprintf("%s: architecture_gate.%s is empty", name, f.key))
		case appealToCustom.MatchString(v):
			problems = append(problems, fmt.Sprintf("%s: architecture_gate.%s argues from habit (%q) - give evidence", name, f.key, appealToCustom.FindString(v)))
		}
	}
	// A bottleneck nobody put a number on was not measured.
	if v := strings.TrimSpace(g.MeasuredBottleneck); v != "" && !hasDigit.MatchString(v) {
		problems = append(problems, fmt.Sprintf("%s: architecture_gate.measured_bottleneck has no measurement in it", name))
	}
	return problems
}

// Process is one process the deployment definition starts, and the file
// that starts it.
type Process struct {
	Name   string
	Source string
}

var (
	unitDependency = regexp.MustCompile(`^\s*(?:Wants|Requires|BindsTo)\s*=\s*(.+)$`)
	composeService = regexp.MustCompile(`^  ([A-Za-z0-9_.\-]+):\s*$`)
)

// ParseProcesses finds every process the deployment definition starts:
// deploy/*.service and *.timer units and the units they want or require,
// the Caddy reverse proxy, docker-compose.yml's services and every cron job
// a deploy/install_*_cron.sh script installs. A new one of any of these
// needs a process entry in the ledger before it ships.
func ParseProcesses(root string) ([]Process, error) {
	seen := map[string]bool{}
	var out []Process
	add := func(name, source string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, Process{Name: name, Source: source})
		}
	}
	deploy := filepath.Join(root, "deploy")
	entries, err := os.ReadDir(deploy)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", deploy, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		switch {
		case strings.HasSuffix(name, ".service") || strings.HasSuffix(name, ".timer"):
			add("systemd:"+name, "deploy/"+name)
			data, err := os.ReadFile(filepath.Join(deploy, name))
			if err != nil {
				return nil, err
			}
			for _, line := range strings.Split(string(data), "\n") {
				if m := unitDependency.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
					for _, unit := range strings.Fields(m[1]) {
						if strings.HasSuffix(unit, ".service") {
							add("systemd:"+unit, "deploy/"+name)
						}
					}
				}
			}
		case name == "Caddyfile":
			add("caddy", "deploy/"+name)
		case strings.HasPrefix(name, "install_") && strings.HasSuffix(name, "_cron.sh"):
			add("cron:"+name, "deploy/"+name)
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "docker-compose.yml")); err == nil {
		inServices := false
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimRight(line, "\r")
			switch {
			case strings.HasPrefix(line, "services:"):
				inServices = true
			case line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
				inServices = false
			case inServices:
				if m := composeService.FindStringSubmatch(line); m != nil {
					add("compose:"+m[1], "docker-compose.yml")
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ValidateProcessCoverage checks that every process the deployment starts
// has a process ledger entry, and that no process entry names one that is
// gone.
func ValidateProcessCoverage(ledger *Ledger, procs []Process) []string {
	var problems []string
	live := map[string]bool{}
	for _, p := range procs {
		live[p.Name] = true
		e := ledger.findEntry(p.Name)
		switch {
		case e == nil:
			problems = append(problems, fmt.Sprintf("%s (from %s) has no ledger entry - a new process needs a process entry with an architecture_gate", p.Name, p.Source))
		case e.Type != ComponentProcess:
			problems = append(problems, fmt.Sprintf("%s: ledger type %q, want %q", p.Name, e.Type, ComponentProcess))
		}
	}
	for _, e := range ledger.EntriesByType(ComponentProcess) {
		if !live[e.Name] {
			problems = append(problems, fmt.Sprintf("%s: ledger names a process the deployment no longer starts - remove the entry", e.Name))
		}
	}
	return problems
}
