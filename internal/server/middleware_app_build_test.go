package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// 297.1: the API names the build of the files on disk, and picks up a new one
// without a restart.
func TestAppBuildHeaderFollowsTheFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app.js")
	write := func(v string, mod time.Time) {
		t.Helper()
		if err := os.WriteFile(app, []byte("x;\nconst VIEW_MODULE_VERSION = '"+v+"';\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(app, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now().Add(-time.Hour)
	write("41", start)
	src := &appBuildSource{path: app}
	h := appBuildHeaderFrom(src, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	get := func(path string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Header().Get(appBuildHeaderName)
	}
	if got := get("/api/v1/me"); got != "41" {
		t.Fatalf("API reply build = %q, want 41", got)
	}
	if got := get("/app.js"); got != "" {
		t.Fatalf("a static file carries the build header (%q); only API replies should", got)
	}

	// A deploy swaps the file. Inside the recheck window the old number may
	// stand; after it, the new one must be sent.
	write("42", start.Add(time.Minute))
	if got := src.get(time.Now().Add(appBuildRecheck + time.Second)); got != "42" {
		t.Fatalf("after a deploy the build = %q, want 42", got)
	}

	// The file going missing mid-deploy keeps the last good number.
	if err := os.Remove(app); err != nil {
		t.Fatal(err)
	}
	if got := src.get(time.Now().Add(3 * appBuildRecheck)); got != "42" {
		t.Fatalf("with app.js missing the build = %q, want the last good 42", got)
	}
}

// 297.1: an ordinary refresh after a deploy gets every new file only if every
// file the app loads carries the release version. A static import, or an
// import() of a fixed path, carries none: the browser keeps the old file, and a
// second copy of a module that is also loaded with ?v= runs beside it.
func TestEveryAppScriptCarriesTheReleaseVersion(t *testing.T) {
	app := readPublicAsset(t, "app.js")
	html := readPublicAsset(t, "index.html")
	ver := regexp.MustCompile(`const VIEW_MODULE_VERSION = '(\d+)'`).FindStringSubmatch(app)
	if ver == nil {
		t.Fatal("VIEW_MODULE_VERSION not found in app.js")
	}

	// The shell: every local script and stylesheet index.html names is stamped,
	// and db.js moves with app.js (G4 bumps both).
	tags := regexp.MustCompile(`<(?:script[^>]*\ssrc|link[^>]*\shref)="(/[^"]+\.(?:js|css)[^"]*)"`).FindAllStringSubmatch(html, -1)
	if len(tags) < 4 {
		t.Fatalf("found only %d script/stylesheet tags in index.html", len(tags))
	}
	for _, m := range tags {
		if !strings.Contains(m[1], "?v=") {
			t.Errorf("index.html loads %s with no ?v= - a deploy would not reach it", m[1])
		}
	}
	if db := regexp.MustCompile(`/db\.js\?v=(\d+)`).FindStringSubmatch(html); db == nil || db[1] != ver[1] {
		t.Errorf("db.js must carry the view-module version %s (got %v)", ver[1], db)
	}

	// Modules: no static import of a local file, no import() of a fixed path
	// without its version. Node's own tests (*.test.mjs) do not run in the browser.
	static := regexp.MustCompile(`(?m)^[ \t]*(?:import|export)\b[^;'"]*?\bfrom\s*['"]([^'"]+)['"]|^[ \t]*import\s*['"]([^'"]+)['"]`)
	fixed := regexp.MustCompile(`\bimport\(\s*['"]([^'"]+)['"]\s*\)`)
	root := filepath.Join("..", "..", "public")
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if strings.HasSuffix(name, ".test.mjs") || !(strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".mjs")) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		rel, _ := filepath.Rel(root, path)
		for _, m := range static.FindAllStringSubmatch(string(body), -1) {
			spec := m[1] + m[2]
			if !strings.Contains(spec, "?v=") {
				t.Errorf("%s: static import of %q - load it with import('%s' + new URL(import.meta.url).search) so it carries this module's ?v=", filepath.ToSlash(rel), spec, spec)
			}
		}
		for _, m := range fixed.FindAllStringSubmatch(string(body), -1) {
			if !strings.Contains(m[1], "?v=") {
				t.Errorf("%s: import(%q) carries no ?v= - add new URL(import.meta.url).search", filepath.ToSlash(rel), m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 100 {
		t.Fatalf("checked only %d scripts under public/", checked)
	}
}

// 297.1: the store app (public/store/) has one stamp of its own - every ?v= in
// its page and scripts, and its service worker's cache name. G4 bumps them all
// together; until 297.1 they stayed at 2 from 0.12.0 on, so phones kept that
// store.js through every later release.
func TestStoreAppStampsAgree(t *testing.T) {
	dir := filepath.Join("..", "..", "public", "store")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	stamp := regexp.MustCompile(`\?v=(\d+)`)
	seen := map[string][]string{}
	for _, e := range entries {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".js") || strings.HasSuffix(e.Name(), ".html")) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range stamp.FindAllStringSubmatch(string(body), -1) {
			seen[m[1]] = append(seen[m[1]], e.Name())
		}
		if e.Name() == "sw.js" {
			c := regexp.MustCompile(`const CACHE = 'store-app-v(\d+)'`).FindStringSubmatch(string(body))
			if c == nil {
				t.Fatal("sw.js: const CACHE = 'store-app-vN' not found")
			}
			seen[c[1]] = append(seen[c[1]], "sw.js CACHE")
		}
	}
	if len(seen) != 1 {
		t.Fatalf("the store app's stamps disagree: %v", seen)
	}
}
