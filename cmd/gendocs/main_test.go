package main

import (
	"os"
	"path/filepath"
	"testing"

	"custom_erp/internal/docgen"
)

func TestReferenceOutputsStayUnderExplicitRoot(t *testing.T) {
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(source, "docs", "project_ledger.md")
	if err := os.WriteFile(ledger, []byte("# Ledger\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDictionaryFixture(t, source)
	files, err := referenceFiles(source, "2026-09-06")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 10 {
		t.Fatalf("got %d outputs", len(files))
	}
	out := t.TempDir()
	if string(files["docs/api/generated/public-v1.json"]) != string(files["docs/specs/openapi_public_v1.json"]) {
		t.Fatal("OpenAPI compatibility projection diverged")
	}
	if err := docgen.Write(out, files); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docs/guides/ERROR_CODES.md", "docs/kb/reference/report-catalog.md", "docs/specs/openapi_public_v1.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(source, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatalf("source mutated: %s", name)
		}
	}
	if got := docgen.Diff(out, files); len(got) > 0 {
		t.Fatal(got)
	}
}

func TestMissingLedgerFailsBeforeAnyOutput(t *testing.T) {
	if files, err := referenceFiles(t.TempDir(), "2026-09-06"); err == nil || files != nil {
		t.Fatal("silently skipped required output")
	}
}
