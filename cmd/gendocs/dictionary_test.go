package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDictionaryFixture(t *testing.T, root string) {
	t.Helper()
	s := RegistrySnapshot{SchemaVersion: 1, CapturedOn: "2026-09-09", Environment: "development", TenantSchema: "tenant_test", Doctypes: []RegistryDoctype{{Name: "Item", Module: "Inventory"}}, Fields: []RegistryField{{Doctype: "Item", Name: "sku", Type: "Data", Required: true}}, Columns: []RegistryColumn{{Table: "documents", Name: "id", Type: "uuid"}}}
	d := DictionaryDefinitions{Status: "draft", Owner: "data-owner", ReviewBy: "2026-10-09", DefaultDefinition: "Review configured field meanings", Retention: "Qualified review before deletion"}
	if err := os.MkdirAll(filepath.Join(root, "docs/data"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"registry-snapshot.json": s, "business-definitions.json": d} {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, "docs/data", name), body, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDictionaryReproducesFactsAndRejectsOrphanMetadata(t *testing.T) {
	root := t.TempDir()
	writeDictionaryFixture(t, root)
	files, err := dictionaryFiles(root, "2026-09-09")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Registry       RegistrySnapshot `json:"registry"`
		SnapshotSHA256 string           `json:"snapshot_sha256"`
		Reports        []any            `json:"reports"`
	}
	if err = json.Unmarshal(files["docs/data/generated/dictionary.json"], &result); err != nil {
		t.Fatal(err)
	}
	if len(result.SnapshotSHA256) != 64 || len(result.Reports) == 0 || !result.Registry.Fields[0].Required || result.Registry.Fields[0].Name != "sku" {
		t.Fatal("dictionary lost source facts or provenance")
	}
	result.Registry.Fields[0].Doctype = "Missing"
	if err = validateRegistry(result.Registry); err == nil {
		t.Fatal("orphan field accepted")
	}
	result.Registry.Fields[0].Doctype = "Item"
	result.Registry.Fields = append(result.Registry.Fields, result.Registry.Fields[0])
	if err = validateRegistry(result.Registry); err == nil {
		t.Fatal("duplicate field accepted")
	}
	if !strings.Contains(string(files["docs/data/generated/dictionary.md"]), "not automatically public") {
		t.Fatal("classification limit absent")
	}
}

func TestDictionaryMissingSourceFailsClosed(t *testing.T) {
	if files, err := dictionaryFiles(t.TempDir(), "2026-09-09"); err == nil || files != nil {
		t.Fatal("missing source silently skipped")
	}
	if _, err := captureRegistry("not a database", "tenant_default;DROP SCHEMA public", "development", "2026-09-09"); err == nil {
		t.Fatal("unsafe schema accepted")
	}
}
