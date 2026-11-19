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

	// 49.4.3 (ask 191): style-src is enforced strict too - run-time styles go
	// through the CSSOM. 'unsafe-inline' anywhere in the enforced policy would
	// bring markup styles (and the attack surface they carry) back.
	if !strings.Contains(enforced, "style-src 'self';") || strings.Contains(enforced, "'unsafe-inline'") {
		t.Errorf("the enforced policy must restrict style-src to 'self', without 'unsafe-inline' - set a run-time style through data-css or adoptCss instead: %q", enforced)
	}
	// The report-only header keeps the same strictness and carries the NEXT
	// candidate (Trusted Types), so the two-phase mechanism is still in place.
	if !strings.Contains(reportOnly, "style-src 'self';") {
		t.Errorf("report-only candidate must keep the strict style-src: %q", reportOnly)
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

// 49.4.3: the report-only candidate runs the Trusted Types trial, which the enforced policy does not
// carry yet (it changes only once a browser sweep shows zero violations). Both carry the strict style-src;
// the enforced one since ask 191's zero-violation crawl. 238.2: the trial is sent only when
// CSP_TRUSTED_TYPES_TRIAL is on (a CSP sweep crawl); by default the report-only policy still reports
// what the enforced one refuses, without a report per innerHTML write.
func TestSecurityHeadersTrustedTypesTrialIsReportOnly(t *testing.T) {
	headers := func() (enforced, reportOnly string) {
		response := httptest.NewRecorder()
		securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		return response.Header().Get("Content-Security-Policy"), response.Header().Get("Content-Security-Policy-Report-Only")
	}
	for _, c := range []struct {
		value string
		trial bool
	}{{"", false}, {"off", false}, {"yes please", false}, {"on", true}, {" ON ", true}, {"1", true}, {"true", true}} {
		t.Setenv("CSP_TRUSTED_TYPES_TRIAL", c.value)
		enforced, reportOnly := headers()
		if got := strings.Contains(reportOnly, "require-trusted-types-for 'script'"); got != c.trial {
			t.Errorf("CSP_TRUSTED_TYPES_TRIAL=%q: report-only carries the Trusted Types trial = %v, want %v: %q", c.value, got, c.trial, reportOnly)
		}
		if !strings.HasSuffix(reportOnly, "report-uri /api/v1/security/csp-report") {
			t.Errorf("CSP_TRUSTED_TYPES_TRIAL=%q: the report-only policy must still report: %q", c.value, reportOnly)
		}
		if strings.Contains(reportOnly, "unsafe-inline") || !strings.Contains(reportOnly, "style-src 'self';") {
			t.Errorf("the report-only policy must carry the strict style-src: %q", reportOnly)
		}
		if strings.Contains(enforced, "require-trusted-types-for") {
			t.Errorf("Trusted Types must not be enforced until a sweep shows zero violations: %q", enforced)
		}
	}
}
