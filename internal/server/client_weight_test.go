package server

// Stage 49.18.4 - no client bloat. The browser app loads nothing from another
// origin: no remote script, stylesheet or font, no analytics SDK, no CAPTCHA
// bundle. Stage 40.4 removed the last third-party resource (Google Fonts) and
// the CSP is 'self' for scripts, styles and fonts; this keeps both true, so a
// convenience include cannot quietly add a third party to every page load.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	remoteTag   = regexp.MustCompile(`(?i)<(script|link|iframe|img)\b[^>]*\b(src|href)\s*=\s*["']?(https?:)?//`)
	cssComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssRemote   = regexp.MustCompile(`(?i)(@import\b|url\(\s*["']?(https?:)?//)`)
	jsRemoteURL = regexp.MustCompile(`(?i)(importScripts|import|fetch|new\s+Worker|\.src\s*=)\s*\(?\s*["'](https?:)?//`)
	trackerHost = regexp.MustCompile(`(?i)["'][^"']*(googletagmanager|google-analytics|doubleclick|segment\.(io|com)|mixpanel|hotjar|amplitude\.com|fullstory|clarity\.ms|recaptcha|hcaptcha|challenges\.cloudflare|fonts\.googleapis|fonts\.gstatic|cdn\.jsdelivr|unpkg\.com|cdnjs)[^"']*["']`)
)

func TestStage49184BrowserAppLoadsNothingFromAnotherOrigin(t *testing.T) {
	root := filepath.Join("..", "..", "public")
	checked := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".html" && ext != ".js" && ext != ".css" && ext != ".mjs" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		if ext == ".css" {
			src = cssComment.ReplaceAllString(src, "")
		}
		rel, _ := filepath.Rel(root, path)
		checked++
		switch ext {
		case ".html":
			if m := remoteTag.FindString(src); m != "" {
				t.Errorf("%s loads a resource from another origin: %s", rel, m)
			}
		case ".css":
			if m := cssRemote.FindString(src); m != "" {
				t.Errorf("%s: %q - stylesheets may not import or reference another origin (or chain an @import onto the critical path)", rel, m)
			}
		default:
			if m := jsRemoteURL.FindString(src); m != "" {
				t.Errorf("%s fetches code or data from another origin: %s", rel, m)
			}
		}
		if m := trackerHost.FindString(src); m != "" {
			t.Errorf("%s references a third-party SDK, font or CDN: %s - a tracker, CAPTCHA or remote include needs a measured threat benefit and a privacy review first", rel, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 20 {
		t.Fatalf("checked only %d files under public/", checked)
	}
}

func TestStage49184CSPKeepsScriptsStylesAndFontsSameOrigin(t *testing.T) {
	response := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := response.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"default-src", "script-src", "font-src"} {
		value := ""
		for _, part := range strings.Split(csp, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, directive+" ") {
				value = strings.TrimSpace(strings.TrimPrefix(part, directive))
			}
		}
		if value != "'self'" {
			t.Errorf("%s is %q, want only 'self' (CSP: %s)", directive, value, csp)
		}
	}
	if style := regexp.MustCompile(`style-src ([^;]*)`).FindStringSubmatch(csp); style == nil || strings.Contains(style[1], "http") {
		t.Errorf("style-src must not allow another origin: %v", style)
	}
}
