package server

import "testing"

// Stage 57: CSP violation reports must not share the "default" bucket. They
// arrive unauthenticated, keyed by IP alone, at ~28 per page load; sharing
// the bucket let them exhaust it and refuse the next ordinary request.
func TestCSPReportsHaveTheirOwnRateLimitBucket(t *testing.T) {
	category, limit := rateLimitCategory("/api/v1/security/csp-report", "POST")
	if category == "default" {
		t.Fatalf("csp-report shares the default bucket; want its own")
	}
	if limit < 300 {
		t.Fatalf("csp-report limit %d is too low for ~28 reports per page load across a shop's tills", limit)
	}
	if other, _ := rateLimitCategory("/api/v1/system/environment", "GET"); other != "default" {
		t.Fatalf("an ordinary unauthenticated call moved out of the default bucket: %q", other)
	}
}
