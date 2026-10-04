package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLinksResolveRepositoryAndKBAnchors(t *testing.T) {
	slugs := map[string]string{"checkout": "docs/kb/tasks/checkout.md"}
	path, anchor, external := resolveLink("docs/kb/start/intro.md", "checkout.md#refund", slugs)
	if path != slugs["checkout"] || anchor != "refund" || external {
		t.Fatal(path, anchor, external)
	}
	path, anchor, external = resolveLink("docs/README.md", "../README.md#start", slugs)
	if path != "README.md" || anchor != "start" || external {
		t.Fatal(path, anchor, external)
	}
	_, _, external = resolveLink("docs/README.md", "https://example.invalid/offline", slugs)
	if !external {
		t.Fatal("external link must not trigger network access")
	}
	ids := headingIDs("# Start\n## Refund\n## Refund\n", false)
	if !ids["refund"] || !ids["refund-1"] {
		t.Fatal(ids)
	}
	if strings.Contains(stripCode("```md\n[example](missing.md)\n```\n[real](present.md)"), "missing") {
		t.Fatal("linted illustrative fenced code")
	}
	if got := stripInlineCode("`[example](missing.md)` and ``[example](missing.md)`` [real](present.md)"); strings.Contains(got, "missing") || !strings.Contains(got, "present") {
		t.Fatal(got)
	}
}

func TestLintFindsBrokenLinksExpiredReviewAndDuplicateIDsWithoutWriting(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	header := "---\ndoc_id: DOC-001\nreview_by: 2020-01-01\n---\n"
	for name, body := range map[string]string{"a.md": header + "# Alpha\n[broken](missing.md)\n[anchor](b.md#absent)\n", "b.md": header + "# Beta\n"} {
		if err := os.WriteFile(filepath.Join(root, "docs", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rules := []Rule{{Type: "reference", Status: "active", Owner: "docs", Authority: "canonical", Disposition: "rebuild"}}
	before, _ := os.Stat(filepath.Join(root, "docs/a.md"))
	reg, report, err := inspect(root, rules, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"broken-link", "broken-anchor", "review", "duplicate-id", "metadata", "unregistered"} {
		if report.Counts[code] == 0 {
			t.Error("missing finding", code)
		}
	}
	if len(reg.Documents) != 2 {
		t.Fatal(reg.Documents)
	}
	after, _ := os.Stat(filepath.Join(root, "docs/a.md"))
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("lint touched source")
	}
	if _, err := os.Stat(filepath.Join(root, "docs/governance")); !os.IsNotExist(err) {
		t.Fatal("lint created outputs")
	}
}

func TestSecretAndScopeRules(t *testing.T) {
	if !secret.MatchString("postgres://user:password@host/db") || !nonportable.MatchString("file:///workstation/doc.md") {
		t.Fatal("missed unsafe documentation")
	}
	rules := []Rule{{Prefix: "docs/archive/", Type: "record", Authority: "historical"}, {Prefix: "docs/", Type: "reference"}}
	if got := classify("docs/archive/a.md", rules); got.Authority != "historical" {
		t.Fatal(got)
	}
}

func TestOnlyDeclaredProjectWorkRegistersExemptArticleByteBudget(t *testing.T) {
	metadata := map[string]string{"format": "work-register"}
	for _, path := range []string{"docs/micro_checklist.md", "docs/product/erp-build-checklist.md"} {
		if !isWorkRegister(path, metadata) {
			t.Errorf("declared work register not recognized: %s", path)
		}
	}
	for _, path := range []string{"docs/ai_handover.md", "docs/product/other.md"} {
		if isWorkRegister(path, metadata) {
			t.Errorf("unapproved path exempted from article byte budget: %s", path)
		}
	}
}

// --- BLD-055: recoverable-failure safety ------------------------------------
//
// The safety rounds this item asks for have two halves: the checks are
// read-only (covered by
// TestLintFindsBrokenLinksExpiredReviewAndDuplicateIDsWithoutWriting above,
// and re-proved against the real tree by hashing docs/ before and after three
// strict runs), and a failure is recoverable - it reports the problem and
// leaves the tree exactly as it found it, rather than half-writing an
// inventory. The tests below cover the second half.

func TestCorruptRegisterIsReportedWithoutDamagingTheTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs/governance"), 0o755); err != nil {
		t.Fatal(err)
	}
	page := "---\ndoc_id: DOC-OK\nowner: docs\n---\n# Fine\n"
	pagePath := filepath.Join(root, "docs/a.md")
	if err := os.WriteFile(pagePath, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	registerPath := filepath.Join(root, "docs/governance/document-register.json")
	corrupt := []byte("{ not json at all")
	if err := os.WriteFile(registerPath, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	pageBefore, _ := os.Stat(pagePath)

	rules := []Rule{{Type: "reference", Status: "active", Owner: "docs", Authority: "canonical", Disposition: "rebuild"}}
	_, _, err := inspect(root, rules, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("a corrupt register must be reported, not silently treated as an empty one - that would mark every document unregistered and invite a destructive 'refresh'")
	}
	if !strings.Contains(err.Error(), "register") {
		t.Errorf("the error must name the register as the problem, got %v", err)
	}

	// Recoverable: the corrupt file is left exactly as-is for an operator to
	// fix or restore, and no source page was touched.
	after, readErr := os.ReadFile(registerPath)
	if readErr != nil {
		t.Fatalf("the register must be left in place for recovery: %v", readErr)
	}
	if string(after) != string(corrupt) {
		t.Errorf("the corrupt register was rewritten; recovery must be the operator's choice, got %q", string(after))
	}
	pageAfter, _ := os.Stat(pagePath)
	if !pageBefore.ModTime().Equal(pageAfter.ModTime()) {
		t.Error("a failed run touched a source page")
	}
}

func TestMissingRegisterIsNotAFailure(t *testing.T) {
	// A tree with no register yet must still lint - that is the bootstrap
	// case, and it is distinct from a register that exists but is unreadable.
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/a.md"), []byte("---\ndoc_id: DOC-OK\nowner: docs\n---\n# Fine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rules := []Rule{{Type: "reference", Status: "active", Owner: "docs", Authority: "canonical", Disposition: "rebuild"}}
	_, report, err := inspect(root, rules, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("a missing register must lint cleanly as the bootstrap case: %v", err)
	}
	if report.Counts["unregistered"] == 0 {
		t.Error("with no register, every document should be reported unregistered")
	}
}
