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

// Stage 49.1.7 regression: outside-in verification against production found
// that http.FileServer serves its content for ANY HTTP method - TRACE, PUT
// and DELETE against a real deployed asset all returned 200, because
// FileServer only special-cases HEAD and otherwise never looks at r.Method.
// onlyReadMethods is what routes.go now wraps the file server in.
func TestOnlyReadMethodsRejectsEverythingButGetAndHead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("console.log(1)"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(onlyReadMethods(http.FileServer(http.Dir(root))))
	defer srv.Close()

	do := func(method, path string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}

	for _, method := range []string{http.MethodTrace, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodPost} {
		t.Run(method+" is refused", func(t *testing.T) {
			resp := do(method, "/app.js")
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s /app.js = %d, want 405 - a static file server must not treat every method as a read", method, resp.StatusCode)
			}
			if got := resp.Header.Get("Allow"); got != "GET, HEAD" {
				t.Errorf("Allow header = %q, want %q", got, "GET, HEAD")
			}
		})
	}

	t.Run("GET still serves the file", func(t *testing.T) {
		resp := do(http.MethodGet, "/app.js")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /app.js = %d, want 200 - the method restriction must not break normal asset loading", resp.StatusCode)
		}
	})

	t.Run("HEAD still succeeds", func(t *testing.T) {
		resp := do(http.MethodHead, "/app.js")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("HEAD /app.js = %d, want 200", resp.StatusCode)
		}
	})
}
