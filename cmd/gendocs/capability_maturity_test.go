package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stage 47.17.5: the committed capability-maturity page is the registry's
// current projection. Regenerate with `pwsh docs/update-docs.ps1 -Group
// Content` (or `go run ./cmd/gendocs -out <staging>`) after changing
// engines/capability_maturity.go.
func TestStage47175CapabilityMaturityDocIsCurrent(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "docs", "governance", "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		VerifiedOn string `json:"verified_on"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	want := capabilityMaturityDoc(config.VerifiedOn)
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(capabilityMaturityPath)))
	if err != nil {
		t.Fatalf("%s is missing; generate it: %v", capabilityMaturityPath, err)
	}
	if strings.ReplaceAll(string(got), "\r\n", "\n") != want {
		t.Fatalf("%s is stale against engines.CapabilityMaturityRegistry(); regenerate it", capabilityMaturityPath)
	}
	for _, must := range []string{"## Production", "## Preview", "## Experimental", "| Certified | 0 |"} {
		if !strings.Contains(want, must) {
			t.Errorf("generated page misses %q", must)
		}
	}
}
