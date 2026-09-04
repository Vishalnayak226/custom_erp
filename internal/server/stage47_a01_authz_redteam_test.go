//go:build stage47redteam

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"custom_erp/db"
)

// Stage 47.0.1 / audit finding A-01 ("Cashier can read confidential HR,
// system, and finance data" - docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md
// lines 65-75). The audit's live evidence: a signed Cashier request returned
// HTTP 200 from the trial balance, audit log, system log, sales register,
// vendor ledger, asset register, employee loan, expense claim, allocation
// strategy and gate-pass endpoints. This test covers the three the audit
// quotes first (trial balance, audit log, system log) as the reproducible
// red case; the rest are the same shape and belong to 47.1.8's exhaustive
// generated contract tests, not repeated by hand here.
//
// Required closure (47.1): deny-by-default route capabilities, so an
// authenticated-but-unauthorized role gets 403, not a 200 full of data it
// has no business reading. See this file's build-tag header comment
// (stage47_redteam_helpers_test.go) for how to run this and what a pass
// means.
func TestA01CashierCannotReadFinanceAuditSystemLogs(t *testing.T) {
	db.InitDB(testConnStr())

	userID, cleanup := seedStage47User(t, "Cashier", "HO")
	defer cleanup()
	token := stage47Token(userID, "Cashier", "HO")

	cases := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"trial balance", http.MethodGet, "/api/v1/finance/trial-balance?as_of=2026-09-03", handleTrialBalance},
		{"audit log", http.MethodGet, "/api/v1/logs/audit", handleAuditLogs},
		{"system log", http.MethodGet, "/api/v1/logs/system", handleSystemLogs},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			apiMiddleware(c.handler)(rec, req)

			if rec.Code == http.StatusOK {
				t.Fatalf("A-01: Cashier got HTTP 200 from %s (%s) - this must be 403 once 47.1's deny-by-default route capabilities land; body=%s", c.name, c.path, rec.Body.String())
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("A-01: expected 403 (or today's actual 200, asserted above) from %s, got %d: %s", c.name, rec.Code, rec.Body.String())
			}
		})
	}
}
