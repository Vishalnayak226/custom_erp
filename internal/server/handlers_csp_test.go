package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSecurityHeadersEnforcesStrictScriptSrc pins the policy contract after
// 47.8.2.
//
// This test previously asserted the opposite - that the enforcing policy
// still carried 'unsafe-inline' "before inline actions are migrated". They
// are now migrated: all 126 inline event-handler attributes are gone and the
// enforced policy drops the exception. The assertion is inverted rather than
// deleted, because the thing worth guarding did not go away, it moved: the
// risk now is the exception being quietly reintroduced to make some future
// inline handler work.
func TestSecurityHeadersEnforcesStrictScriptSrc(t *testing.T) {
	response := httptest.NewRecorder()
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	enforced := response.Header().Get("Content-Security-Policy")
	reportOnly := response.Header().Get("Content-Security-Policy-Report-Only")

	if !strings.Contains(enforced, "script-src 'self';") {
		t.Fatalf("the enforced policy must restrict script-src to 'self': %q", enforced)
	}
	// The three ways the exception could come back, each individually refused.
	for _, forbidden := range []string{"'unsafe-inline'", "'unsafe-eval'", "'unsafe-hashes'"} {
		scriptSrc := enforced
		if i := strings.Index(enforced, "script-src"); i >= 0 {
			scriptSrc = enforced[i:]
			if j := strings.Index(scriptSrc, ";"); j >= 0 {
				scriptSrc = scriptSrc[:j]
			}
		}
		if strings.Contains(scriptSrc, forbidden) {
			t.Fatalf("script-src must not regain %s - convert the handler with actionAttrs() instead: %q", forbidden, enforced)
		}
	}
	if !strings.Contains(enforced, "object-src 'none'") || !strings.Contains(enforced, "base-uri 'self'") {
		t.Errorf("object-src/base-uri lockdown must survive the script-src change: %q", enforced)
	}

	// The report-only header carries the NEXT candidate (strict style-src), so
	// the same two-phase mechanism is still in place for the next step.
	if !strings.Contains(reportOnly, "style-src 'self';") {
		t.Errorf("report-only candidate should now be tightening style-src: %q", reportOnly)
	}
	if !strings.Contains(reportOnly, "report-uri /api/v1/security/csp-report") {
		t.Fatalf("report-only candidate has no reporting endpoint: %q", reportOnly)
	}
}

func TestCSPReportHandlerBoundsAndRedactsBrowserReport(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	body := `{"csp-report":{"document-uri":"https://erp.invalid/doc/SECRET-RECORD?token=SECRET-TOKEN","violated-directive":"script-src-attr","blocked-uri":"inline","line-number":27}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/security/csp-report", strings.NewReader(body))
	response := httptest.NewRecorder()
	handleCSPReport(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("report status=%d body=%q", response.Code, response.Body.String())
	}
	logged := logs.String()
	if !strings.Contains(logged, "directive=script-src-attr") || !strings.Contains(logged, "blocked=inline") || !strings.Contains(logged, "line=27") {
		t.Fatalf("CSP violation summary missing: %q", logged)
	}
	for _, secret := range []string{"SECRET-RECORD", "SECRET-TOKEN", "erp.invalid"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("CSP report leaked URL data %q: %q", secret, logged)
		}
	}

	logs.Reset()
	oversized := httptest.NewRequest(http.MethodPost, "/api/v1/security/csp-report", strings.NewReader(strings.Repeat("x", maxCSPReportBytes+1)))
	oversizedResponse := httptest.NewRecorder()
	handleCSPReport(oversizedResponse, oversized)
	if oversizedResponse.Code != http.StatusNoContent || logs.Len() != 0 {
		t.Fatalf("oversized report must be silently dropped: status=%d log=%q", oversizedResponse.Code, logs.String())
	}
}
