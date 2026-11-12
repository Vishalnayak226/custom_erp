package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Stage 49.4.5: an API answer is not cached unless its handler opts in, and
// every response carries the clickjacking, MIME-sniffing, referrer and HSTS
// headers.
func TestStage49APIAnswersAreNotCachedAndCarrySecurityHeaders(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))

	rec := doRequestFromIP(t, mux.ServeHTTP, "GET", "/api/v1/doc/Brand?limit=1", token, nil, mfaTestIP())
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a record list: %d, Cache-Control %q; want no-store", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := doRequestFromIP(t, mux.ServeHTTP, "GET", "/api/v1/doc/Brand", "", nil, mfaTestIP()); rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a refused request must not be cached either: %q", rec.Header().Get("Cache-Control"))
	}

	// The security-header wrapper the server puts around everything.
	sec := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(sec, httptest.NewRequest("GET", "/", nil))
	for header, want := range map[string]string{"X-Frame-Options": "DENY", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer"} {
		if got := sec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if sec.Header().Get("Strict-Transport-Security") == "" || sec.Header().Get("Permissions-Policy") == "" {
		t.Error("HSTS and Permissions-Policy must be set")
	}
}
