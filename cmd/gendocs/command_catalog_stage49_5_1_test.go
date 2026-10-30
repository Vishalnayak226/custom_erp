package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stage 49.5.1: every registered command of a family has a catalogue note,
// and no note names a route that is gone.
func TestStage4951EveryFamilyRouteHasAnEntry(t *testing.T) {
	if problems := commandCatalogProblems(); len(problems) > 0 {
		t.Fatalf("command catalogue:\n  %s", strings.Join(problems, "\n  "))
	}
	for _, f := range commandFamilies {
		if len(commandCatalogRoutes()[f.Name]) == 0 {
			t.Errorf("family %q has no registered command", f.Name)
		}
	}
}

// The facts the page shows are read from the code, not typed in.
func TestStage4951CodeFactsAreReadFromTheHandlers(t *testing.T) {
	facts := readCommandFacts(filepath.Join("..", ".."))
	if !facts.available {
		t.Fatal("routes.go not found from the test's working directory")
	}
	checkout := facts.routes["POST /api/v1/checkout"]
	if checkout.Handler != "handleCheckout" {
		t.Errorf("checkout handler = %q", checkout.Handler)
	}
	if !contains(checkout.Idempotency, "ClaimCommand(pos.checkout)") || !contains(checkout.Idempotency, "client idempotency_key") {
		t.Errorf("checkout idempotency = %v", checkout.Idempotency)
	}
	returns := facts.routes["POST /api/v1/returns"]
	if !contains(returns.Idempotency, "ClaimCommand(returns.create)") {
		t.Errorf("returns idempotency = %v (handler %s)", returns.Idempotency, returns.Handler)
	}
	adjust := facts.routes["POST /api/v1/inventory/adjustments/{id}/post"]
	if !contains(adjust.Idempotency, "RunEconomicCommand(inventory.adjustment.post)") {
		t.Errorf("adjustment post idempotency = %v (handler %s)", adjust.Idempotency, adjust.Handler)
	}
	settings := facts.routes["PUT /api/v1/admin/settings"]
	if !contains(settings.AuditRows, "SETTINGS / UPDATED") {
		t.Errorf("settings audit rows = %v", settings.AuditRows)
	}
	if !contains(facts.routes["POST /api/v1/pos/session/open"].AuditRows, "POS_SESSION / OPENED") {
		t.Errorf("session open audit rows = %v", facts.routes["POST /api/v1/pos/session/open"].AuditRows)
	}
	if facts.routes["POST /api/v1/wms/pick-scan"].Module != "oms,wms" {
		t.Errorf("pick-scan module gate = %q", facts.routes["POST /api/v1/wms/pick-scan"].Module)
	}
}

// The committed page is the current projection.
func TestStage4951CommandCatalogIsCurrent(t *testing.T) {
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
	want := string(commandCatalogFiles(root, config.VerifiedOn)[commandCatalogPath])
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(commandCatalogPath)))
	if err != nil {
		t.Fatalf("%s is missing; generate it: %v", commandCatalogPath, err)
	}
	if strings.ReplaceAll(string(got), "\r\n", "\n") != want {
		t.Fatalf("%s is stale against the routes, handlers or notes; regenerate it", commandCatalogPath)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
