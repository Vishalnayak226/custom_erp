package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stage 49.1.2 regression: the file server must never generate a directory
// index. Written against a temporary tree with the same shape public/ has -
// an index.html at the root, and a subdirectory without one - so it keeps
// asserting the behaviour even if public/ gains or loses files.
func TestStaticFileServerNeverListsADirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html>shell"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "components"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "components", "secret_module.js"), []byte("export const x = 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docsite"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docsite", "index.html"), []byte("<!doctype html>docs"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.FileServer(noDirectoryListing(http.Dir(root))))
	defer srv.Close()

	get := func(path string) (int, string) {
		t.Helper()
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 4096)
		n, _ := resp.Body.Read(buf)
		return resp.StatusCode, string(buf[:n])
	}

	t.Run("a directory with no index.html is not enumerable", func(t *testing.T) {
		status, body := get("/components/")
		if status != http.StatusNotFound {
			t.Errorf("GET /components/ = %d, want 404 - a directory listing is an unauthenticated map of the application", status)
		}
		if strings.Contains(body, "secret_module.js") {
			t.Errorf("GET /components/ leaked a filename from the directory: %s", body)
		}
	})

	t.Run("its files are still served by exact path", func(t *testing.T) {
		if status, _ := get("/components/secret_module.js"); status != http.StatusOK {
			t.Errorf("GET /components/secret_module.js = %d, want 200 - suppressing the listing must not break asset loading", status)
		}
	})

	t.Run("a directory with an index.html still serves it", func(t *testing.T) {
		status, body := get("/docsite/")
		if status != http.StatusOK || !strings.Contains(body, "docs") {
			t.Errorf("GET /docsite/ = %d %q, want 200 serving index.html", status, body)
		}
	})

	t.Run("the root still serves the SPA shell", func(t *testing.T) {
		status, body := get("/")
		if status != http.StatusOK || !strings.Contains(body, "shell") {
			t.Errorf("GET / = %d %q, want 200 serving index.html", status, body)
		}
	})

	t.Run("an unknown directory and an unenumerable one are indistinguishable", func(t *testing.T) {
		listable, _ := get("/components/")
		absent, _ := get("/no-such-directory/")
		if listable != absent {
			t.Errorf("GET /components/ = %d but GET /no-such-directory/ = %d - a different status confirms which directories exist", listable, absent)
		}
	})
}
