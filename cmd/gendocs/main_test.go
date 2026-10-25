package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"custom_erp/internal/docgen"
	"custom_erp/internal/server"
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
	writePageInventoryFixture(t, source)
	files, err := referenceFiles(source, "2026-09-06")
	if err != nil {
		t.Fatal(err)
	}
	if want := 7 + len(kbReportCatalog("2026-09-06")) + len(kbErrorCodeReference("2026-09-06")) + len(kbReleaseNotes("2026-09-06", "# Ledger\n")); len(files) != want {
		t.Fatalf("got %d outputs, want %d", len(files), want)
	}
	out := t.TempDir()
	if err := docgen.Write(out, files); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docs/guides/ERROR_CODES.md", "docs/kb/reference/report-catalog.md", "docs/api/generated/public-v1.json"} {
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

// The error-code reference spills onto further pages as the catalog grows.
// Every code must still appear exactly once, and no page may outgrow its share.
func TestErrorCodeReferenceSplitsWithoutLosingCodes(t *testing.T) {
	pages := kbErrorCodeReference("2026-09-06")
	if _, ok := pages["error-code-reference.md"]; !ok {
		t.Fatal("the first page must keep its name; every existing link points at it")
	}
	all := ""
	for name, body := range pages {
		if name != "error-code-reference.md" && len(body) > 2*kbErrorCodePartBytes {
			t.Errorf("%s is %d bytes", name, len(body))
		}
		all += body
	}
	for _, entry := range server.CatalogEntries() {
		if n := strings.Count(all, "| `"+entry.Code+"` |"); n != 1 {
			t.Errorf("%s appears %d times across the pages", entry.Code, n)
		}
	}
}

func TestMissingLedgerFailsBeforeAnyOutput(t *testing.T) {
	if files, err := referenceFiles(t.TempDir(), "2026-09-06"); err == nil || files != nil {
		t.Fatal("silently skipped required output")
	}
}
