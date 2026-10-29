package engines

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Stage 47.18.2: docs/operations/storage-forecast.md is held to the code - the
// retention windows it quotes are the defaults the settings ship with, and
// every transaction kind the generator measures has its row.
func TestStorageForecastDocMatchesTheCode(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "operations", "storage-forecast.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	defs := map[string]SettingDefinition{}
	for _, d := range ListSettingDefinitions() {
		defs[d.Key] = d
	}
	rows := regexp.MustCompile("\\| `([a-z_]+\\.[a-z_]+)` \\| ([0-9]+) (days|hours) \\|").FindAllStringSubmatch(doc, -1)
	if len(rows) < 6 {
		t.Fatalf("expected the retention table, found %d setting rows", len(rows))
	}
	for _, m := range rows {
		d, ok := defs[m[1]]
		if !ok {
			t.Errorf("the forecast quotes %s, which is not a setting", m[1])
			continue
		}
		if d.Default != m[2] || d.Unit != m[3] {
			t.Errorf("the forecast says %s is %s %s; the code ships %s %s", m[1], m[2], m[3], d.Default, d.Unit)
		}
	}
	for _, kind := range []string{"sale", "return", "receipt", "putaway", "audit", "month_end_posting", "master:Item", "master:Customer"} {
		if !strings.Contains(doc, "| "+kind+" |") {
			t.Errorf("no measured row for %s", kind)
		}
	}
	for _, name := range SynthShapeNames() {
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("shape %s is not described", name)
		}
	}
}
