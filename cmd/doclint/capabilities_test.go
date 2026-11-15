package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProductionClaimRequiresCompleteEvidenceAndApprovedRegister(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirement.md"), []byte("# Requirement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := CapabilityRegister{SchemaVersion: 1, Status: "draft", Owner: "product-owner", Configurations: []Configuration{{ID: "retail"}}, Capabilities: []Capability{{ID: "CAP-POS", Owner: "store-owner", Maturity: "Production", Configurations: []string{"retail"}, Requirements: []string{"requirement.md"}, Stages: []string{"47.3"}, LastVerified: "2026-09-07", ReviewBy: "2026-10-07"}}}
	findings := validateCapabilities(root, r, time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC))
	var messages []string
	for _, f := range findings {
		messages = append(messages, f.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "requires approver") || !strings.Contains(joined, "draft register") {
		t.Fatal(joined)
	}
}

func TestPreviewReferencesAreValidatedWithoutInventingApproval(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte("# Requirements\n\n## FR-POS-001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := CapabilityRegister{SchemaVersion: 1, Status: "draft", Owner: "product-owner", Configurations: []Configuration{{ID: "retail"}}, Capabilities: []Capability{{ID: "CAP-POS", Owner: "store-owner", Maturity: "Preview", Configurations: []string{"retail"}, Requirements: []string{"requirements.md#fr-pos-001"}, Stages: []string{"47.3"}, LastVerified: "2026-09-07", ReviewBy: "2026-10-07"}}}
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	if got := validateCapabilities(root, r, now); len(got) != 0 {
		t.Fatal(got)
	}
	r.Capabilities[0].Requirements = []string{"requirements.md#absent", "../escape.md"}
	r.Capabilities[0].Configurations = []string{"unknown"}
	if got := validateCapabilities(root, r, now); len(got) < 3 {
		t.Fatal(got)
	}
}

func TestExistingFileCannotMasqueradeAsReleaseApproval(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("# A file is not approval\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := CapabilityRegister{SchemaVersion: 1, Status: "approved", Owner: "product-owner", Release: "1.0", Configurations: []Configuration{{ID: "retail"}}, Capabilities: []Capability{{ID: "CAP-POS", Owner: "store-owner", Maturity: "Production", Configurations: []string{"retail"}, Requirements: []string{"readme.md"}, Design: []string{"readme.md"}, Tests: []string{"readme.md"}, Help: []string{"readme.md"}, Stages: []string{"47.3"}, LastVerified: "2026-09-07", ReviewBy: "2026-10-07", Approver: "qa-owner", ReleaseEvidence: []string{"readme.md"}}}}
	findings := validateCapabilities(root, r, time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC))
	if len(findings) < 2 {
		t.Fatalf("accepted fabricated release evidence: %v", findings)
	}
}

func TestNewRequirementCannotDisappearFromReverseTrace(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs/requirements"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/requirements/example.md"), []byte("---\ntype: normative\n---\n# Example\n\n## FR-POS-001\n\n## FR-POS-002\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := CapabilityRegister{SchemaVersion: 2, Capabilities: []Capability{{ID: "CAP-POS", Requirements: []string{"docs/requirements/example.md#fr-pos-001"}}}}
	findings := validateReverseTrace(root, r)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "FR-POS-002") {
		t.Fatal(findings)
	}
	r.Capabilities[0].Requirements = append(r.Capabilities[0].Requirements, "docs/requirements/example.md#fr-pos-002")
	if findings = validateReverseTrace(root, r); len(findings) != 0 {
		t.Fatal(findings)
	}
}

func TestModuleMappingSchemaKeepsTraceAndIdentityRequirements(t *testing.T) {
	r := CapabilityRegister{SchemaVersion: 3, Status: "draft", Owner: "product", Capabilities: []Capability{{ID: "CAP-POS", ModuleKeys: []string{"sales", "sales", "Unknown module"}}}}
	findings := validateCapabilities(t.TempDir(), r, time.Now())
	var messages []string
	for _, f := range findings {
		messages = append(messages, f.Message)
	}
	joined := strings.Join(messages, "\n")
	for _, required := range []string{"module mapping version/release is required", "invalid or duplicate module key: sales", "invalid or duplicate module key: Unknown module", "business, persona, process, control and shared requirement links are required", "owner, configuration, requirements and work item are required"} {
		if !strings.Contains(joined, required) {
			t.Errorf("missing validation %q: %s", required, joined)
		}
	}
	r.Capabilities[0].ModuleKeys = nil
	findings = validateCapabilities(t.TempDir(), r, time.Now())
	for _, f := range findings {
		if strings.Contains(f.Message, "module_keys are required") {
			return
		}
	}
	t.Fatal("schema 3 accepted missing module mapping")
}

// 73.0.2: the blueprint rows filed under a capability keep a known status and
// priority, an acceptance test, real evidence and real checklist items, and
// render into the catalogue.
func TestCapabilityChecksAreValidatedAndRendered(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "micro_checklist.md"), []byte("- [ ] **73.4.2 OMS / logistics:** open\n  - [x] **49.5.5b.1 Done.** sub-item\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "archive", "micro_checklist_closed_stages.md"), []byte("- [x] **73.2.5 Hold and resume.** done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok := CapabilityCheck{ID: "BP-126", Capability: "Promise/ATP", Target: "Promised date", Status: "Missing / benchmark gap", Priority: "P1/P2", AcceptanceTest: "Promise tied to supply", ChecklistRef: []string{"73.4.2", "73.2.5", "49.5.5b.1"}}
	r := CapabilityRegister{ChecksSource: "the blueprint.", Capabilities: []Capability{{ID: "CAP-OMS", Title: "Order management", Checks: []CapabilityCheck{ok}}}}
	if got := validateCapabilityChecks(root, r); len(got) != 0 {
		t.Fatalf("a good row was refused: %v", got)
	}
	bad := []CapabilityCheck{
		{ID: "BP-126", Capability: "Duplicate", Status: "Partial", Priority: "P1", AcceptanceTest: "x"},
		{ID: "ROW-1", Capability: "Bad id", Status: "Partial", Priority: "P1", AcceptanceTest: "x"},
		{ID: "BP-200", Capability: "Bad status", Status: "Done", Priority: "P1", AcceptanceTest: "x"},
		{ID: "BP-201", Capability: "Bad priority", Status: "Partial", Priority: "high", AcceptanceTest: "x"},
		{ID: "BP-202", Capability: "No test", Status: "Partial", Priority: "P2"},
		{ID: "BP-203", Capability: "Verified, no evidence", Status: "Verified", Priority: "P0", AcceptanceTest: "x"},
		{ID: "BP-204", Capability: "Missing evidence", Status: "Verified", Priority: "P0", AcceptanceTest: "x", Evidence: []string{"engines/absent_test.go"}},
		{ID: "BP-205", Capability: "Unknown item", Status: "Partial", Priority: "P1", AcceptanceTest: "x", ChecklistRef: []string{"99.9"}},
	}
	r.Capabilities[0].Checks = append([]CapabilityCheck{ok}, bad...)
	got := validateCapabilityChecks(root, r)
	var messages []string
	for _, f := range got {
		messages = append(messages, f.Message)
	}
	joined := strings.Join(messages, "\n")
	for _, want := range []string{"BP-126: invalid or duplicate check id", "ROW-1: invalid or duplicate check id", "invalid check status Done", "invalid check priority high", "BP-202: capability and acceptance test are required", "BP-203: a Verified row needs its acceptance evidence", "missing evidence engines/absent_test.go", "unknown checklist item 99.9"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing finding %q in:\n%s", want, joined)
		}
	}
	if len(got) != len(bad) {
		t.Errorf("%d findings for %d bad rows:\n%s", len(got), len(bad), joined)
	}

	var b strings.Builder
	r.Capabilities[0].Checks = []CapabilityCheck{ok, {ID: "BP-127", Capability: "Payment | state", Status: "Partial", Priority: "P0", AcceptanceTest: "Reconciles"}}
	writeCapabilityChecks(&b, r)
	out := b.String()
	for _, want := range []string{"## Capability checks", "2 rows. Source: the blueprint.", "### CAP-OMS — Order management (2)", "| BP-126 | Promise/ATP | Promised date | Missing / benchmark gap | P1/P2 | Promise tied to supply | pending | 73.4.2, 73.2.5, 49.5.5b.1 |", `| BP-127 | Payment \| state |`, "| pending | none |"} {
		if !strings.Contains(out, want) {
			t.Errorf("catalogue lacks %q:\n%s", want, out)
		}
	}
}
