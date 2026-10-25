package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"custom_erp/internal/server"
)

// writePageInventoryFixture gives a temp source root the three inputs the
// page inventory reads besides public/*.js: routes.go, the view registry and
// the plan's owner table.
func writePageInventoryFixture(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"internal/server/routes.go": "\thttp.HandleFunc(\"POST /api/v1/returns/{id}/qc\", apiMiddleware(moduleGate(\"oms\", handleApplyReturnQC)))\n",
		"public/app.js":             "const LAZY_VIEW_MODULES = {\n  returns: ['view-returns.js', 'renderReturnsView', 'Returns'],\n};\n",
		"public/view-returns.js":    "apiFetch(`/api/v1/returns/${encodeURIComponent(id)}/qc`, { method: 'POST' });\n",
		ownerPlanPath:               "### Page ownership inventory at review time\n\n| UI | Owners |\n|---|---|\n| `view-returns.js` | M06 |\n\n### M01\n",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPageInventoryNamesScreenOwnerRouteAndRoles(t *testing.T) {
	root := t.TempDir()
	writeDictionaryFixture(t, root)
	writePageInventoryFixture(t, root)
	files, err := pageInventoryFiles(root, "2026-10-25")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(files[pageInventoryPath])
	for _, want := range []string{
		"### `public/view-returns.js`",
		"Returns (`#/view/returns`; no help article)",
		"| Returns | `returns` | `public/view-returns.js` | yes | missing |",
		"Owners: M06. Live inspection: not recorded.",
		"| POST | `/api/v1/returns/{id}/qc` | `handleApplyReturnQC` | oms | signed-in: oms.default | every signed-in role |",
		"| Routes no front-end file calls | 0 |",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("inventory lacks %q", want)
		}
	}
}

func TestPageInventoryCallScanAndMatching(t *testing.T) {
	src := "apiFetch(`/api/v1/doc/${encodeURIComponent(dt)}/meta?x=1`); fetch('/api/v1/doc/' + dt); const base = '/api/v1/procurement/bulk-upload';"
	if got, want := apiCalls(src), []string{"/api/v1/doc/{}/meta", "/api/v1/doc/{}", "/api/v1/procurement/bulk-upload"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("apiCalls = %q, want %q", got, want)
	}
	routes := []server.RouteAccess{
		{Pattern: "GET /api/v1/doc/{doctype}/meta", Path: "/api/v1/doc/{doctype}/meta"},
		{Pattern: "/api/v1/doc/{doctype}/{id}", Path: "/api/v1/doc/{doctype}/{id}"},
		{Pattern: "POST /api/v1/procurement/bulk-upload/preview", Path: "/api/v1/procurement/bulk-upload/preview"},
	}
	if got := matchRoutes("/api/v1/doc/{}/meta", routes); len(got) != 1 || got[0].Path != "/api/v1/doc/{doctype}/meta" {
		t.Fatalf("the literal segment must win over {id}: %+v", got)
	}
	if got := matchRoutes("/api/v1/procurement/bulk-upload", routes); len(got) != 0 {
		t.Fatalf("a base matches no route exactly: %+v", got)
	}
	if got := prefixRoutes("/api/v1/procurement/bulk-upload", routes); len(got) != 1 {
		t.Fatalf("a base matches the routes under it: %+v", got)
	}
	if got := prefixRoutes("/api/{}", routes); got != nil {
		t.Fatalf("\"/api/\" + anything names no base: %+v", got)
	}
}
