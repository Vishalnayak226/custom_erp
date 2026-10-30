package supplychain

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStage47184VendoredAssetsAreInTheLedger: every third-party file under
// public/ has a vendored_asset entry with its licence, size and the checksum
// of the bytes on disk, and no entry names a file that is gone.
func TestStage47184VendoredAssetsAreInTheLedger(t *testing.T) {
	ledger := loadRepoLedger(t)
	assets, err := ParseVendoredAssets(repoRoot)
	if err != nil {
		t.Fatalf("ParseVendoredAssets: %v", err)
	}
	found := map[string]bool{}
	for _, a := range assets {
		found[a.Path] = true
	}
	// The scan must still see what is known to be there, or it is broken.
	if !found["public/qrcode.min.js"] {
		t.Fatalf("ParseVendoredAssets did not find public/qrcode.min.js - the scan is broken (found %v)", assets)
	}
	if problems := ValidateVendoredCoverage(ledger, assets); len(problems) > 0 {
		t.Fatalf("vendored assets are out of step with %s:\n- %s", ledgerPath, joinProblems(problems))
	}
	for _, e := range ledger.EntriesByType(ComponentVendoredAsset) {
		if e.Version == "" || e.License == "" || e.Purpose == "" || e.Bytes <= 0 || !strings.HasPrefix(e.Checksum, "sha256:") {
			t.Errorf("%s: a vendored_asset entry needs version, license, purpose, bytes and a sha256: checksum: %+v", e.Name, e)
		}
	}
}

// TestStage47184VendoredScanRefusesMissingAndGoneEntries: a vendored file
// with no entry, an entry for a file that is gone, a changed file, a wrong
// size and a plain project file are each told apart.
func TestStage47184VendoredScanRefusesMissingAndGoneEntries(t *testing.T) {
	root := t.TempDir()
	pub := filepath.Join(root, "public")
	if err := os.MkdirAll(filepath.Join(pub, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(pub, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("app.js", "const own = 1; // this project's own code\n")
	write("lib.min.js", "!function(){}();")
	write("vendor/grid.css", ".g{display:grid}")
	write("banner.js", "/*! widget v2 | Copyright (c) 2020 Someone | MIT */\nvar w=1;")

	assets, err := ParseVendoredAssets(root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, a := range assets {
		paths = append(paths, a.Path)
	}
	if strings.Join(paths, ",") != "public/banner.js,public/lib.min.js,public/vendor/grid.css" {
		t.Fatalf("scan found %v; want the minified file, the vendor/ file and the banner file, not app.js", paths)
	}

	sum := func(body string) string {
		h := sha256.Sum256([]byte(body))
		return "sha256:" + hex.EncodeToString(h[:])
	}
	entry := func(name, body string) LedgerEntry {
		return LedgerEntry{Name: name, Type: ComponentVendoredAsset, Version: "1", Checksum: sum(body), Bytes: int64(len(body)),
			Owner: "frontend", Purpose: "test", License: "MIT", Provenance: "test"}
	}
	complete := &Ledger{SchemaVersion: LedgerSchemaVersion, Entries: []LedgerEntry{
		entry("public/lib.min.js", "!function(){}();"), entry("public/vendor/grid.css", ".g{display:grid}"),
		entry("public/banner.js", "/*! widget v2 | Copyright (c) 2020 Someone | MIT */\nvar w=1;"),
	}}
	if problems := ValidateVendoredCoverage(complete, assets); len(problems) > 0 {
		t.Fatalf("a complete ledger must pass: %v", problems)
	}

	expect := func(name string, l *Ledger, want string) {
		t.Helper()
		problems := ValidateVendoredCoverage(l, assets)
		if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), want) {
			t.Errorf("%s: want a problem containing %q, got %v", name, want, problems)
		}
	}
	missing := &Ledger{Entries: complete.Entries[1:]}
	expect("no entry", missing, "public/lib.min.js has no ledger entry")
	gone := &Ledger{Entries: append(append([]LedgerEntry{}, complete.Entries...), entry("public/old.min.js", "x"))}
	expect("gone file", gone, "public/old.min.js: ledger names a vendored file that is not under public/ any more")
	changed := &Ledger{Entries: append([]LedgerEntry{}, complete.Entries...)}
	changed.Entries[0].Checksum = sum("something else")
	expect("changed file", changed, "public/lib.min.js: ledger checksum")
	wrongSize := &Ledger{Entries: append([]LedgerEntry{}, complete.Entries...)}
	wrongSize.Entries[1].Bytes = 1
	expect("wrong size", wrongSize, "public/vendor/grid.css: ledger says 1 bytes")
	wrongType := &Ledger{Entries: append([]LedgerEntry{}, complete.Entries...)}
	wrongType.Entries[2].Type = ComponentProcess
	expect("wrong type", wrongType, `public/banner.js: ledger type "process"`)
	noLicense := &Ledger{Entries: append([]LedgerEntry{}, complete.Entries...)}
	noLicense.Entries[0].License = ""
	expect("no license", noLicense, "public/lib.min.js: empty license")
}
