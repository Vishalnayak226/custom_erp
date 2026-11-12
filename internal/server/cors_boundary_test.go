package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 49.4.2: CSRF / cross-origin boundary.

func TestNormalizeCORSOrigin(t *testing.T) {
	good := map[string]string{
		"https://app.example.com":      "https://app.example.com",
		"HTTPS://App.Example.com/":     "https://app.example.com",
		"https://app.example.com:443":  "https://app.example.com",
		"http://localhost:8080":        "http://localhost:8080",
		"https://app.example.com:8443": "https://app.example.com:8443",
	}
	for in, want := range good {
		if got, ok := normalizeCORSOrigin(in); !ok || got != want {
			t.Errorf("%q -> %q,%v want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "null", "NULL", "*", "https://*.example.com", "https://a.example/path",
		"https://a.example?x=1", "https://user@a.example", "ftp://a.example", "a.example", "https://", "file://"} {
		if got, ok := normalizeCORSOrigin(bad); ok {
			t.Errorf("%q must be refused, got %q", bad, got)
		}
	}
}

func TestCORSAllowlistDropsNullWildcardAndPaths(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "null, *, https://a.example/path, https://ok.example:8443, https://*.bad.example")
	got := loadCORSAllowlist()
	for _, bad := range []string{"null", "*", "https://a.example/path", "https://a.example", "https://*.bad.example"} {
		if got[bad] {
			t.Errorf("allowlist accepted %q", bad)
		}
	}
	if !got["https://ok.example:8443"] || !got["http://localhost:8080"] {
		t.Errorf("allowlist lost a valid entry: %v", got)
	}
}

func TestOriginAllowedRefusesNullAndWildcard(t *testing.T) {
	for _, o := range []string{"null", "*", "https://*.example.com", ""} {
		if originAllowed(o) {
			t.Errorf("originAllowed(%q) = true", o)
		}
	}
	if !originAllowed("http://localhost:8080") {
		t.Error("the built-in local origin must stay allowed")
	}
	// Scheme and port are part of the match.
	for _, o := range []string{"https://localhost:8080", "http://localhost:8081", "http://localhost"} {
		if originAllowed(o) {
			t.Errorf("originAllowed(%q) = true; only http://localhost:8080 is listed", o)
		}
	}
}

func corsProbe(method, origin string, hdr map[string]string) *httptest.ResponseRecorder {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest(method, "http://erp.test/api/v1/ping", strings.NewReader(""))
	req.RemoteAddr = mfaTestIP() + ":40000"
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	apiMiddleware(ok)(rec, req)
	return rec
}

func TestCORSPreflightAndVary(t *testing.T) {
	db.InitDB(testConnStr())
	// Allowed origin: headers, Vary, cacheable preflight.
	rec := corsProbe("OPTIONS", "http://localhost:8080", map[string]string{"Access-Control-Request-Method": "POST"})
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:8080" ||
		rec.Header().Get("Access-Control-Allow-Credentials") != "true" ||
		rec.Header().Get("Access-Control-Max-Age") != "600" {
		t.Errorf("allowed preflight headers wrong: %v", rec.Header())
	}
	if !strings.Contains(strings.Join(rec.Header().Values("Vary"), ","), "Origin") {
		t.Errorf("Vary: Origin missing: %v", rec.Header().Values("Vary"))
	}
	// Refused origins and null: no CORS headers, still Vary: Origin.
	for _, o := range []string{"https://evil.example", "null", "http://localhost:8081"} {
		rec := corsProbe("OPTIONS", o, map[string]string{"Access-Control-Request-Method": "POST"})
		for _, h := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Allow-Methods", "Access-Control-Max-Age"} {
			if rec.Header().Get(h) != "" {
				t.Errorf("origin %q got %s=%q", o, h, rec.Header().Get(h))
			}
		}
		if !strings.Contains(strings.Join(rec.Header().Values("Vary"), ","), "Origin") {
			t.Errorf("origin %q: Vary: Origin missing", o)
		}
	}
	// A request with no Origin header also varies, so a shared cache never
	// hands a CORS-less copy to a cross-origin caller.
	if rec := corsProbe("GET", "", nil); !strings.Contains(strings.Join(rec.Header().Values("Vary"), ","), "Origin") {
		t.Error("Vary: Origin missing on a request without Origin")
	}
}

func TestCrossOriginWriteCheck(t *testing.T) {
	db.InitDB(testConnStr())
	cases := []struct {
		name, method, origin, host string
		mode                       string
		want                       bool
	}{
		{"report mode never refuses", "POST", "https://evil.example", "erp.test", "report", false},
		{"default is report", "POST", "https://evil.example", "erp.test", "", false},
		{"enforce refuses a stranger", "POST", "https://evil.example", "erp.test", "enforce", true},
		{"enforce refuses null", "POST", "null", "erp.test", "enforce", true},
		{"enforce refuses a stranger delete", "DELETE", "https://evil.example", "erp.test", "enforce", true},
		{"same host allowed", "POST", "http://erp.test", "erp.test", "enforce", false},
		{"same host other port refused", "POST", "http://erp.test:9999", "erp.test", "enforce", true},
		{"allowlisted allowed", "POST", "http://localhost:8080", "erp.test", "enforce", false},
		{"reads are not state changes", "GET", "https://evil.example", "erp.test", "enforce", false},
		{"no origin (server to server) allowed", "POST", "", "erp.test", "enforce", false},
		{"off", "POST", "https://evil.example", "erp.test", "off", false},
	}
	for _, c := range cases {
		t.Setenv("CSRF_ORIGIN_CHECK", c.mode)
		req := httptest.NewRequest(c.method, "http://"+c.host+"/api/v1/x", strings.NewReader(""))
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		if got := crossOriginWriteRefused(req); got != c.want {
			t.Errorf("%s: refused=%v want %v", c.name, got, c.want)
		}
	}
	// Through the real middleware.
	t.Setenv("CSRF_ORIGIN_CHECK", "enforce")
	if rec := corsProbe("POST", "https://evil.example", nil); rec.Code != http.StatusForbidden {
		t.Errorf("enforce through apiMiddleware: %d, want 403", rec.Code)
	}
	if rec := corsProbe("POST", "http://localhost:8080", nil); rec.Code == http.StatusForbidden {
		t.Errorf("allowlisted origin refused: %s", rec.Body.String())
	}
}

// Simple-content-type bypass: a cross-site form or no-cors fetch can send
// text/plain or urlencoded without a preflight; the request envelope refuses
// both on every JSON route (49.4.4), checked here for the sign-in route.
func TestSimpleContentTypesRefusedOnJSONRoutes(t *testing.T) {
	db.InitDB(testConnStr())
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	for _, ct := range []string{"text/plain", "text/plain;charset=UTF-8", "application/x-www-form-urlencoded"} {
		req := httptest.NewRequest("POST", "/api/v1/login", strings.NewReader(`{"username":"a","password":"b"}`))
		req.RemoteAddr = mfaTestIP() + ":40000"
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		apiMiddleware(ok)(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("%s accepted on a JSON route", ct)
		}
	}
}

// Bearer tokens authorise; a cookie never does. If a handler ever starts to
// set or read a cookie this test fails and the CSRF design must be redone
// (token plus origin check) before it can pass again.
func TestNoRouteAuthorizesFromCookie(t *testing.T) {
	db.InitDB(testConnStr())
	token := engines.SignToken("admin", "admin", "Admin", "default", "HO", 1)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	req := httptest.NewRequest("GET", "/api/v1/profile", nil)
	req.RemoteAddr = mfaTestIP() + ":40000"
	for _, n := range []string{"erp_token", "token", "session", "Authorization", "access_token"} {
		req.AddCookie(&http.Cookie{Name: n, Value: token})
	}
	rec := httptest.NewRecorder()
	apiMiddleware(ok)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a request authorised only by a cookie got %d", rec.Code)
	}

	// Source check: no server or engine code sets or reads a cookie.
	for _, dir := range []string{".", "../../engines"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			s := string(b)
			for _, needle := range []string{"http.SetCookie(", ".Cookie(", ".Cookies()", `Header.Get("Cookie")`, `Set("Set-Cookie"`} {
				if strings.Contains(s, needle) {
					t.Errorf("%s uses %s: cookie authorization needs the CSRF design (49.4.2) first", f, needle)
				}
			}
		}
	}
}
