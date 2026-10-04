package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGzipMeasurementIsDeterministicAndPerResource(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"a.js": "const a = 1;", "b.css": "body { color: red; }"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{"a.js", "b.css"}
	first, err := sumGzipFiles(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sumGzipFiles(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	if first <= 0 || first != second {
		t.Fatalf("gzip measurements are not reproducible: %d then %d", first, second)
	}
}

func TestBudgetThresholdUsesByteUnits(t *testing.T) {
	report := Report{Metrics: map[string]Metric{}}
	setLimit(&report, "sample", 1024, KiB, "unit test")
	if got := report.Metrics["sample"].Status; got != "within_budget" {
		t.Fatalf("equal-to-limit artifact status=%q", got)
	}
	setLimit(&report, "sample", 1025, KiB, "unit test")
	if got := report.Metrics["sample"].Status; got != "over_budget" {
		t.Fatalf("over-budget artifact status=%q", got)
	}
}

// TestProfilesPlaceTheFirstPOSChunkCorrectly pins where the first authorized
// lazy screen counts and where it does not.
//
// It belongs in the cold profile (a cold first-load-to-usable measurement) and
// in the explicit shell-plus-first-screen observation. It must NOT be in
// jsFiles: that is NFR-COST-001's "initial core JS", and view-pos.js loads
// only after authentication and a route-entitlement check. Counting it there
// conflated a lazy module with the initial payload and made the 120 KiB gate
// read as breached while the shell itself measured 102.5 KiB.
func TestProfilesPlaceTheFirstPOSChunkCorrectly(t *testing.T) {
	const pos = "public/view-pos.js"
	has := func(profile []string) bool {
		for _, path := range profile {
			if path == pos {
				return true
			}
		}
		return false
	}
	if !has(coreFiles) {
		t.Error("the cold profile must include the first authorized POS view module")
	}
	if !has(jsPlusFirstScreenFiles) {
		t.Error("the shell-plus-first-screen observation must include the POS view module")
	}
	if has(jsFiles) {
		t.Error("initial core JS must not include a lazily-loaded view module; that is a separate, unbudgeted observation")
	}
	// The two JS profiles must otherwise be the same shell.
	if len(jsPlusFirstScreenFiles) != len(jsFiles)+1 {
		t.Errorf("the observation profile must be the shell plus exactly one screen, got %d vs %d", len(jsPlusFirstScreenFiles), len(jsFiles))
	}
}

// TestFirstScreenObservationCarriesNoInventedLimit guards the honesty of the
// split: separating the metrics must not become a way to hide the number.
func TestFirstScreenObservationCarriesNoInventedLimit(t *testing.T) {
	report := measureRepo(t)
	m, ok := report.Metrics["initial_js_plus_first_screen_gzip_bytes"]
	if !ok {
		t.Fatal("the shell-plus-first-screen cost must still be reported")
	}
	if m.Limit != nil {
		t.Errorf("NFR-COST-001 defines no budget for this set; reporting limit %d bytes would be a fabricated threshold", *m.Limit)
	}
	shell := report.Metrics["initial_js_gzip_bytes"]
	if m.Value <= shell.Value {
		t.Errorf("the observation (%d) must exceed the shell-only measurement (%d)", m.Value, shell.Value)
	}
}

// --- BLD-048: threshold-failure tests ---------------------------------------
//
// The tests above cover the measurement mechanics. The ones below are the
// threshold-failure tests the BLD-048 Done bar asks for: they run the real
// measurement against the real repository, so an artifact crossing its
// canonical NFR limit fails `go test ./...` and not only the CI budget step.
// Every assertion is in bytes; KiB/MiB are 1024-based throughout (Report.Units
// records this in the emitted JSON as well).

// canonicalBudgets maps each metric that must carry a hard threshold to the
// NFR clause it comes from. Kept as data so a budget cannot quietly become
// observation-only: TestEveryCanonicalBudgetCarriesAHardThreshold fails if a
// name here stops reporting a limit.
var canonicalBudgets = map[string]struct {
	limit  int64
	clause string
}{
	"cold_core_gzip_bytes":       {180 * KiB, "NFR-COST-001 cold core <=180 KiB compressed"},
	"initial_js_gzip_bytes":      {120 * KiB, "NFR-COST-001 initial core JS <=120 KiB gzip"},
	"embedded_kb_raw_bytes":      {2 * MiB, "NFR-DOC-001 KB <=2 MiB"},
	"kb_search_index_raw_bytes":  {250 * KiB, "NFR-DOC-001 search <=250 KiB"},
	"kb_largest_topic_raw_bytes": {120 * KiB, "NFR-DOC-001 ordinary topic <=120 KiB"},
}

// repoRoot resolves the repository root from this package's own directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the repository root at %s: %v", root, err)
	}
	return root
}

// measureRepo runs the real measurement with no database, no backup dir and
// no binary - the three inputs that depend on an environment rather than on
// the committed tree, so this test is reproducible anywhere.
func measureRepo(t *testing.T) Report {
	t.Helper()
	report, err := measure(repoRoot(t), "", "", "", "logs")
	if err != nil {
		t.Fatalf("measure repository: %v", err)
	}
	return report
}

func TestCommittedArtifactsAreWithinCanonicalBudgets(t *testing.T) {
	report := measureRepo(t)
	for name, budget := range canonicalBudgets {
		metric, ok := report.Metrics[name]
		if !ok {
			t.Errorf("%s: not measured at all; %s is unenforced", name, budget.clause)
			continue
		}
		if metric.Limit == nil {
			t.Errorf("%s: no hard limit reported; %s is unenforced", name, budget.clause)
			continue
		}
		if *metric.Limit != budget.limit {
			t.Errorf("%s: limit is %d bytes, canonical %s requires %d bytes",
				name, *metric.Limit, budget.clause, budget.limit)
		}
		if metric.Status != "within_budget" {
			t.Errorf("%s: %d bytes of %d (%s) - status %q. This budget is a release gate: shrink the artifact rather than raising the limit.",
				name, metric.Value, *metric.Limit, budget.clause, metric.Status)
		}
	}
}

func TestEveryCanonicalBudgetCarriesAHardThreshold(t *testing.T) {
	report := measureRepo(t)
	for name := range canonicalBudgets {
		if metric := report.Metrics[name]; metric.Limit == nil || metric.Unit != "bytes" {
			t.Errorf("%s must report a byte-unit hard limit, got unit=%q limit=%v", name, metric.Unit, metric.Limit)
		}
	}
	// The binary budget is environment-dependent (it needs a built artifact),
	// so it is absent-but-declared rather than silently missing.
	binary, ok := report.Metrics["binary_stripped_bytes"]
	if !ok {
		t.Fatal("binary_stripped_bytes must always appear in the report, measured or not")
	}
	if binary.Status != "not_measured" {
		t.Fatalf("with no -binary supplied the binary metric must say so, got %q", binary.Status)
	}
}

func TestBinaryBudgetFailsOverTwentyFiveMiB(t *testing.T) {
	// Proves the 25 MiB threshold actually bites, without building a real
	// oversized binary: the same setLimit path the real measurement uses.
	report := Report{Metrics: map[string]Metric{}}
	setLimit(&report, "binary_stripped_bytes", 25*MiB, 25*MiB, "boundary")
	if got := report.Metrics["binary_stripped_bytes"].Status; got != "within_budget" {
		t.Fatalf("exactly 25 MiB must pass, got %q", got)
	}
	setLimit(&report, "binary_stripped_bytes", 25*MiB+1, 25*MiB, "one byte over")
	if got := report.Metrics["binary_stripped_bytes"].Status; got != "over_budget" {
		t.Fatalf("25 MiB + 1 byte must fail, got %q", got)
	}
}

func TestEveryCanonicalBudgetFailsOneByteOverItsLimit(t *testing.T) {
	for name, budget := range canonicalBudgets {
		report := Report{Metrics: map[string]Metric{}}
		setLimit(&report, name, budget.limit+1, budget.limit, budget.clause)
		if got := report.Metrics[name].Status; got != "over_budget" {
			t.Errorf("%s: one byte over %d must report over_budget, got %q", name, budget.limit, got)
		}
	}
}

func TestGrowthAndRetentionCostsAreCaptured(t *testing.T) {
	report := measureRepo(t)
	// DB growth, logs, audit archives and backups are all required BLD-048
	// cost surfaces. They are deliberately observations rather than gates -
	// NFR-DATA-001 names accountable owners but no approved numeric cap, and
	// inventing one here would be a fabricated limit. What is asserted is
	// that each surface is actually reported, with an honest status.
	honest := map[string]bool{"measured_no_numeric_policy": true, "not_measured": true}
	for _, name := range []string{
		"application_log_files_bytes",
		"backup_artifacts_bytes",
		"postgres_database_bytes",
	} {
		metric, ok := report.Metrics[name]
		if !ok {
			t.Errorf("%s: required cost surface is not reported at all", name)
			continue
		}
		if !honest[metric.Status] {
			t.Errorf("%s: status %q claims a policy that NFR-DATA-001 does not define", name, metric.Status)
		}
		if metric.Limit != nil {
			t.Errorf("%s: reports limit %d bytes, but no approved capacity cap exists to enforce", name, *metric.Limit)
		}
	}
	if _, ok := report.Retention["whole_database_backups"]; !ok {
		t.Error("backup retention window must be reported alongside backup size")
	}
}

func TestReportDocumentsItsMeasurementUnits(t *testing.T) {
	report := measureRepo(t)
	if report.SchemaVersion == 0 {
		t.Error("report must carry a schema version so a stored artifact is interpretable later")
	}
	for _, want := range []string{"byte", "KiB=1024", "MiB=1024*1024", "gzip"} {
		if !strings.Contains(report.Units, want) {
			t.Errorf("Units must document %q so the recorded numbers are unambiguous; got %q", want, report.Units)
		}
	}
	for name, metric := range report.Metrics {
		if metric.Unit != "bytes" {
			t.Errorf("%s: every metric is a byte count, got unit %q", name, metric.Unit)
		}
	}
}

func TestMeasurementIsReproducibleAcrossRuns(t *testing.T) {
	// The Done bar asks for reproducible artifacts. Everything measured from
	// the committed tree must be identical run to run; only the
	// environment-dependent surfaces (database, backups, logs) may move.
	first, second := measureRepo(t), measureRepo(t)
	for name := range canonicalBudgets {
		if first.Metrics[name].Value != second.Metrics[name].Value {
			t.Errorf("%s is not reproducible: %d then %d", name, first.Metrics[name].Value, second.Metrics[name].Value)
		}
	}
}
