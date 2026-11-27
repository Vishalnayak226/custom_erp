package webasset

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func get(t *testing.T, h http.Handler, path string, header map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func body(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFileServerSendsScriptsWithoutComments(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string, mod time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	write("app.js", "// note\nrun();\n", t0)
	write("site.css", "/* x */\na{}\n", t0)
	write("bad.js", "// note\nf(\n", t0)
	write("page.html", "<!-- keep -->\n// keep\n", t0)
	h := http.FileServer(FileSystem(http.Dir(dir)))

	r := get(t, h, "/app.js", nil)
	if got := body(t, r); got != "\nrun();\n" {
		t.Fatalf("app.js: got %q", got)
	}
	if cl := r.Header.Get("Content-Length"); cl != strconv.Itoa(len("\nrun();\n")) {
		t.Errorf("Content-Length %s does not match the stripped body", cl)
	}
	if lm := r.Header.Get("Last-Modified"); lm != t0.Format(http.TimeFormat) {
		t.Errorf("Last-Modified %q, want the file's own time", lm)
	}
	if got := body(t, get(t, h, "/site.css", nil)); got != "\na{}\n" {
		t.Errorf("site.css: got %q", got)
	}
	if got := body(t, get(t, h, "/bad.js", nil)); got != "// note\nf(\n" {
		t.Errorf("a script the scanner refuses goes out as it is; got %q", got)
	}
	if got := body(t, get(t, h, "/page.html", nil)); got != "<!-- keep -->\n// keep\n" {
		t.Errorf("html is never changed; got %q", got)
	}
	if r := get(t, h, "/app.js", map[string]string{"Range": "bytes=1-4"}); r.StatusCode != http.StatusPartialContent || body(t, r) != "run(" {
		t.Errorf("Range: status %d", r.StatusCode)
	}
	if r := get(t, h, "/app.js", map[string]string{"If-Modified-Since": t0.Format(http.TimeFormat)}); r.StatusCode != http.StatusNotModified {
		t.Errorf("If-Modified-Since: status %d, want 304", r.StatusCode)
	}

	// An edited file is read again, not served from the cache.
	write("app.js", "// other\nrun2();\n", t0.Add(time.Minute))
	if got := body(t, get(t, h, "/app.js", nil)); got != "\nrun2();\n" {
		t.Errorf("after an edit: got %q", got)
	}
	if r := get(t, h, "/missing.js", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("missing file: status %d", r.StatusCode)
	}
}
