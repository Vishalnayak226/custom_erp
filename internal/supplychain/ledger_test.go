package supplychain

import (
	"testing"
)

// repoRoot mirrors internal/securityscan's convention: tests run with the
// package directory as the working directory, two levels below repo root.
const repoRoot = "../.."

const ledgerPath = "docs/security/dependency-ledger.json"

func loadRepoLedger(t *testing.T) *Ledger {
	t.Helper()
	l, err := LoadLedger(repoRoot + "/" + ledgerPath)
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	return l
}

// TestDependencyLedgerCoversGoModules is Stage 49.9.4's automatic gate: a Go
// module added or bumped in go.mod without a matching, fully-filled,
// checksum-correct ledger entry fails here. This is what makes "review new
// dependency source/maintainer/update surface" happen by construction rather
// than by someone remembering to update a document.
func TestDependencyLedgerCoversGoModules(t *testing.T) {
	ledger := loadRepoLedger(t)
	_, _, modules, err := ParseGoMod(repoRoot)
	if err != nil {
		t.Fatalf("ParseGoMod: %v", err)
	}
	if len(modules) == 0 {
		t.Fatalf("go.mod parsed with zero require entries - the parser is broken, not the dependency tree")
	}
	sums, err := ParseGoSum(repoRoot)
	if err != nil {
		t.Fatalf("ParseGoSum: %v", err)
	}
	if problems := ValidateGoModuleCoverage(ledger, modules, sums); len(problems) > 0 {
		t.Fatalf("dependency ledger is out of date - update %s:\n- %s", ledgerPath, joinProblems(problems))
	}
}

// TestNoWorkflowActionIsUnpinned is 49.9.8's containment control: every
// third-party GitHub Action referenced from any workflow must be pinned to a
// full commit SHA, not a mutable tag or branch - applied to every action in
// every workflow file, present or future, not just the three fixed for
// Stage 49.9.
func TestNoWorkflowActionIsUnpinned(t *testing.T) {
	actions, err := ParsePinnedActions(repoRoot)
	if err != nil {
		t.Fatalf("ParsePinnedActions: %v", err)
	}
	if len(actions) == 0 {
		t.Fatalf("found zero `uses:` lines in .github/workflows - the parser is broken, not the workflow set")
	}
	if problems := ValidateActionsArePinned(actions); len(problems) > 0 {
		t.Fatalf("unpinned GitHub Action(s) - pin each to the full commit SHA behind its release tag:\n- %s", joinProblems(problems))
	}
}

// TestDependencyLedgerCoversWorkflowActions is the ledger-side counterpart:
// every action referenced anywhere in .github/workflows needs an owner,
// purpose, license and provenance row, independent of whether it happens to
// be pinned yet.
func TestDependencyLedgerCoversWorkflowActions(t *testing.T) {
	ledger := loadRepoLedger(t)
	actions, err := ParsePinnedActions(repoRoot)
	if err != nil {
		t.Fatalf("ParsePinnedActions: %v", err)
	}
	if problems := ValidateActionLedgerCoverage(ledger, actions); len(problems) > 0 {
		t.Fatalf("dependency ledger is missing GitHub Action(s) - add an entry to %s:\n- %s", ledgerPath, joinProblems(problems))
	}
}

// TestLedgerEntriesAreWellFormed is a basic shape guard on the hand-authored
// file: every entry names a real component type and has no entirely-blank
// required field, independent of cross-referencing go.mod/workflows.
func TestLedgerEntriesAreWellFormed(t *testing.T) {
	ledger := loadRepoLedger(t)
	if len(ledger.Entries) == 0 {
		t.Fatalf("%s has zero entries", ledgerPath)
	}
	seen := map[string]bool{}
	validTypes := map[ComponentType]bool{
		ComponentGoModuleDirect: true, ComponentGoModuleIndirect: true, ComponentGoToolchain: true,
		ComponentCIAction: true, ComponentCITool: true, ComponentOSRunner: true, ComponentOSImage: true,
	}
	for _, e := range ledger.Entries {
		if seen[e.Name] {
			t.Errorf("duplicate ledger entry %q", e.Name)
		}
		seen[e.Name] = true
		if !validTypes[e.Type] {
			t.Errorf("%s: unrecognised component type %q", e.Name, e.Type)
		}
		if e.Version == "" {
			t.Errorf("%s: empty version", e.Name)
		}
		if e.Owner == "" || e.Purpose == "" || e.Provenance == "" {
			t.Errorf("%s: owner/purpose/provenance must not be empty", e.Name)
		}
	}
}

func joinProblems(problems []string) string {
	out := ""
	for i, p := range problems {
		if i > 0 {
			out += "\n- "
		}
		out += p
	}
	return out
}
