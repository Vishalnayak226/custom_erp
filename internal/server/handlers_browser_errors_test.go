package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"
)

// 158.3: a browser error report lands in the caller's own System Log as one
// BROWSER row, bounded and without the page URL's query.
func TestBrowserErrorIsWrittenToTheSystemLog(t *testing.T) {
	db.InitDB(testConnStr())
	correlationID := newTestCorrelationID(t)
	defer db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE correlation_id = $1::uuid`, correlationID)

	// The stack is longer than the stored bound, the body still within its own.
	body := `{"message":"TypeError: x is null\n  (second line)","where":"/view-pos.js:12:5","screen":"#view/pos?token=SECRET-QUERY","stack":"TypeError: x is null\n    at render (/view-pos.js:12:5)` + strings.Repeat("a", 6000) + `"}`
	rec := postBrowserError(body, correlationID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("report status=%d body=%s", rec.Code, rec.Body.String())
	}

	var severity, source, message, stack string
	err := db.DB.QueryRow(`SELECT severity, module_source, error_message, stack_trace FROM tenant_default.system_error_logs WHERE correlation_id = $1::uuid`, correlationID).
		Scan(&severity, &source, &message, &stack)
	if err != nil {
		t.Fatalf("no System Log row for the report: %v", err)
	}
	if severity != "BROWSER" || source != "browser #view/pos" {
		t.Fatalf("row is %q from %q, want BROWSER from \"browser #view/pos\"", severity, source)
	}
	if message != "TypeError: x is null (second line) at /view-pos.js:12:5" {
		t.Fatalf("message not folded onto one line with its place: %q", message)
	}
	if len(stack) > maxBrowserErrorStack || !strings.HasPrefix(stack, "TypeError: x is null\n    at render") {
		t.Fatalf("stack not kept and bounded (%d bytes): %.80q", len(stack), stack)
	}
	for _, field := range []string{source, message, stack} {
		if strings.Contains(field, "SECRET-QUERY") {
			t.Fatalf("the page URL's query reached the log: %q", field)
		}
	}
}

func TestBrowserErrorRefusesBadReports(t *testing.T) {
	db.InitDB(testConnStr())
	correlationID := newTestCorrelationID(t)
	defer db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE correlation_id = $1::uuid`, correlationID)

	cases := []struct {
		name, body, purpose string
		want                int
	}{
		{"no message", `{"message":"   ","where":"/app.js:1:1"}`, "", http.StatusBadRequest},
		{"not JSON", `TypeError`, "", http.StatusBadRequest},
		{"too large", `{"message":"` + strings.Repeat("x", maxBrowserErrorBody) + `"}`, "", http.StatusRequestEntityTooLarge},
		{"extension token", `{"message":"TypeError"}`, "extension", http.StatusForbidden},
	}
	for _, c := range cases {
		if rec := postBrowserError(c.body, correlationID, c.purpose); rec.Code != c.want {
			t.Errorf("%s: status=%d, want %d", c.name, rec.Code, c.want)
		}
	}
	for _, m := range loggedMessages(t, correlationID) {
		if !strings.HasPrefix(m, "[") {
			t.Errorf("a refused report was stored as a browser error: %q", m)
		}
	}
}

func TestBrowserErrorsHaveTheirOwnRateLimitBucket(t *testing.T) {
	category, limit := rateLimitCategory("/api/v1/logs/browser-error", http.MethodPost)
	if category == "default" || limit < 5 {
		t.Fatalf("browser errors must have their own bucket with room for one page's five reports, got %q/%d", category, limit)
	}
}

func TestBoundBrowserFieldCutsOnACharacterBoundary(t *testing.T) {
	got := boundBrowserField("ab₹", 4, true) // ₹ is three bytes
	if got != "ab" {
		t.Fatalf("got %q, want \"ab\"", got)
	}
}

func postBrowserError(body, correlationID, purpose string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/logs/browser-error", strings.NewReader(body))
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("Resolved-Correlation-ID", correlationID)
	if purpose != "" {
		req.Header.Set("Resolved-Purpose", purpose)
	}
	rec := httptest.NewRecorder()
	handleBrowserError(rec, req)
	return rec
}
