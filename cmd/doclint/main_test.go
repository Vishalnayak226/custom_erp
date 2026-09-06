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
