package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stage 49.4.7: docs/security/idempotency-audit.md is a pure function of
// routes.go and the handler sources. This regenerates it into a temp dir and
// fails when the committed file differs, so a new state-changing route is
// audited the day it is registered. Set ERP_WRITE_IDEMPOTENCY_AUDIT=1 to
// write the committed file instead of comparing.
func TestStage4947IdempotencyAuditIsCurrent(t *testing.T) {
	generated, err := IdempotencyAuditMarkdown("routes.go", ".")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generated, "`POST /api/v1/checkout`") || !strings.Contains(generated, "yes (command key)") {
		t.Fatalf("the audit does not read the checkout's command key - the handler scan has broken:\n%s", generated[:2000])
	}
	scratch := filepath.Join(t.TempDir(), "idempotency-audit.md")
	if err := os.WriteFile(scratch, []byte(generated), 0o644); err != nil {
		t.Fatal(err)
	}
	committed := filepath.Join(repoRoot(t), "docs", "security", "idempotency-audit.md")
	if os.Getenv("ERP_WRITE_IDEMPOTENCY_AUDIT") == "1" {
		if err := os.WriteFile(committed, []byte(generated), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", committed)
		return
	}
	body, err := os.ReadFile(committed)
	if err != nil {
		t.Fatalf("%v - regenerate with ERP_WRITE_IDEMPOTENCY_AUDIT=1 go test ./internal/server -run TestStage4947IdempotencyAuditIsCurrent", err)
	}
	if strings.ReplaceAll(string(body), "\r\n", "\n") != generated {
		t.Fatalf("docs/security/idempotency-audit.md is stale (a route or handler changed). The current version is at %s; regenerate with ERP_WRITE_IDEMPOTENCY_AUDIT=1 go test ./internal/server -run TestStage4947IdempotencyAuditIsCurrent", scratch)
	}
}

// The classification reads real handlers: the keyed commands are keyed, a
// search is not a command, and a plain record save is unkeyed.
func TestStage4947IdempotencyAuditReadsHandlers(t *testing.T) {
	rows, err := IdempotencyAuditRows("routes.go", ".")
	if err != nil {
		t.Fatal(err)
	}
	byRoute := map[string]IdempotencyAuditRow{}
	for _, row := range rows {
		byRoute[row.Route] = row
	}
	for route, wantKey := range map[string]string{
		"POST /api/v1/checkout":          "yes (command key)",
		"POST /api/v1/returns":           "yes (command key)",
		"POST /api/v1/reports/export":    "n.a. (a question, not a command)",
		"ANY /api/v1/doc/{doctype}":      "no",
		"ANY /api/v1/doc/{doctype}/{id}": "no",
	} {
		row, ok := byRoute[route]
		if !ok {
			t.Errorf("%s is not in the audit", route)
			continue
		}
		if row.Key != wantKey {
			t.Errorf("%s: key %q, want %q", route, row.Key, wantKey)
		}
	}
	if len(rows) < 300 {
		t.Fatalf("only %d state-changing routes audited", len(rows))
	}
}
