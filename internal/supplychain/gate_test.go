package supplychain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStage47184ArchitectureExceptionGate is the gate on the real ledger and
// the real deployment definition: every entry is either one of the frozen
// pre-gate entries, marked so, or carries a complete architecture_gate; and
// every process the deployment starts has a process entry.
func TestStage47184ArchitectureExceptionGate(t *testing.T) {
	ledger := loadRepoLedger(t)
	if ledger.SchemaVersion != LedgerSchemaVersion {
		t.Errorf("%s schema_version %d, want %d", ledgerPath, ledger.SchemaVersion, LedgerSchemaVersion)
	}
	if problems := ValidateArchitectureGate(ledger); len(problems) > 0 {
		t.Fatalf("architecture exception gate (47.18.4) - fix %s:\n- %s", ledgerPath, joinProblems(problems))
	}
	procs, err := ParseProcesses(repoRoot)
	if err != nil {
		t.Fatalf("ParseProcesses: %v", err)
	}
	names := map[string]bool{}
	for _, p := range procs {
		names[p.Name] = true
	}
	// The scan must still see what is known to be there, or it is broken.
	for _, want := range []string{"systemd:erp.service", "systemd:postgresql.service", "caddy", "cron:install_backup_cron.sh"} {
		if !names[want] {
			t.Errorf("ParseProcesses did not find %s - the scan is broken", want)
		}
	}
	if problems := ValidateProcessCoverage(ledger, procs); len(problems) > 0 {
		t.Fatalf("process ledger is out of date - update %s:\n- %s", ledgerPath, joinProblems(problems))
	}
}

func completeGate() *ArchitectureGate {
	return &ArchitectureGate{
		MeasuredBottleneck: "p95 stock report 4.2 s at 20,000 items on the 2-vCPU host",
		FailureModeBenefit: "a slow report no longer holds a pooled connection",
		ResourceBudget:     "64 MB RAM, no disk",
		OpsBudget:          "one systemd unit, journald logs",
		SecurityBudget:     "loopback only, no credentials of its own",
		LicenseBudget:      "MIT",
		MigrationRollback:  "stop the unit; the server falls back to direct queries",
		RemovalPlan:        "delete the unit and its ledger entry",
		Date:               "2026-11-01",
	}
}

// TestStage47184GateRefusesUnjustifiedNewcomers proves each way past the
// gate fails: no gate, a copied grandfathered marker, a pre-gate entry that
// lost its marker, an empty field, an appeal to habit and an unmeasured
// bottleneck. A complete gate passes.
func TestStage47184GateRefusesUnjustifiedNewcomers(t *testing.T) {
	marker := "in the ledger when the gate opened"
	vague := completeGate()
	vague.FailureModeBenefit = "enterprise systems normally use it"
	unmeasured := completeGate()
	unmeasured.MeasuredBottleneck = "reports feel slow"
	missing := completeGate()
	missing.RemovalPlan = " "

	cases := []struct {
		name  string
		entry LedgerEntry
		want  string
	}{
		{"new with no gate", LedgerEntry{Name: "github.com/redis/go-redis"}, "no architecture_gate"},
		{"copied marker", LedgerEntry{Name: "github.com/redis/go-redis", Grandfathered: marker}, "was not in the ledger when the gate opened"},
		{"pre-gate entry lost its marker", LedgerEntry{Name: "github.com/lib/pq"}, "lost its grandfathered marker"},
		{"empty field", LedgerEntry{Name: "x", Gate: missing}, "removal_plan is empty"},
		{"appeal to habit", LedgerEntry{Name: "x", Gate: vague}, "argues from habit"},
		{"unmeasured bottleneck", LedgerEntry{Name: "x", Gate: unmeasured}, "no measurement"},
		{"both marker and gate", LedgerEntry{Name: "github.com/lib/pq", Grandfathered: marker, Gate: completeGate()}, "keep one"},
	}
	for _, c := range cases {
		problems := ValidateArchitectureGate(&Ledger{Entries: []LedgerEntry{c.entry}})
		if !strings.Contains(strings.Join(problems, "\n"), c.want) {
			t.Errorf("%s: problems %q, want one containing %q", c.name, problems, c.want)
		}
	}

	ok := &Ledger{Entries: []LedgerEntry{
		{Name: "systemd:report-cache.service", Type: ComponentProcess, Gate: completeGate()},
		{Name: "github.com/lib/pq", Grandfathered: marker},
	}}
	if problems := ValidateArchitectureGate(ok); len(problems) > 0 {
		t.Errorf("a complete gate and a genuine pre-gate entry were refused: %q", problems)
	}
}

// TestStage47184NewProcessNeedsALedgerEntry: a unit file, a unit it wants, a
// compose service and a cron installer each count as a process.
func TestStage47184NewProcessNeedsALedgerEntry(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("deploy/erp.service", "[Unit]\r\nWants=postgresql.service\r\nAfter=network.target\r\n")
	write("deploy/report-cache.service", "[Unit]\nDescription=new\n")
	write("deploy/Caddyfile", ":443\n")
	write("deploy/install_backup_cron.sh", "#!/bin/sh\n")
	write("docker-compose.yml", "# c\nservices:\n  postgres:\n    image: postgres:16\n  redis:\n    image: redis\nvolumes:\n  data:\n")

	procs, err := ParseProcesses(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range procs {
		got = append(got, p.Name)
	}
	want := "caddy,compose:postgres,compose:redis,cron:install_backup_cron.sh,systemd:erp.service,systemd:postgresql.service,systemd:report-cache.service"
	if strings.Join(got, ",") != want {
		t.Fatalf("processes %v, want %s", got, want)
	}

	ledger := &Ledger{Entries: []LedgerEntry{
		{Name: "systemd:erp.service", Type: ComponentProcess},
		{Name: "systemd:postgresql.service", Type: ComponentProcess},
		{Name: "caddy", Type: ComponentProcess},
		{Name: "compose:postgres", Type: ComponentProcess},
		{Name: "cron:install_backup_cron.sh", Type: ComponentProcess},
		{Name: "cron:install_cleanup_cron.sh", Type: ComponentProcess},
	}}
	problems := strings.Join(ValidateProcessCoverage(ledger, procs), "\n")
	for _, want := range []string{"systemd:report-cache.service (from deploy/report-cache.service) has no ledger entry", "compose:redis (from docker-compose.yml) has no ledger entry", "cron:install_cleanup_cron.sh: ledger names a process the deployment no longer starts"} {
		if !strings.Contains(problems, want) {
			t.Errorf("problems %q, want one containing %q", problems, want)
		}
	}
}
