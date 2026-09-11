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
