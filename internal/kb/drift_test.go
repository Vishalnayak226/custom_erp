package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func driftFixtureSources(t *testing.T) (dir string, sources DriftSources) {
	t.Helper()
	dir = t.TempDir()

	appJS := filepath.Join(dir, "app.js")
	writeFixture(t, appJS, `
	if (view === 'pos') {
		x();
	} else if (view === 'oms') {
		y();
	} else if (view === 'reports') {
		z();
	}
	`)

	errorCatalog := filepath.Join(dir, "error_catalog_generated.go")
	writeFixture(t, errorCatalog, `
package server
var catalog = map[string]ErrorInfo{
	"POSOFF-0238": {
		Code: "POSOFF-0238",
	},
	"GLOBAL-0019": {
		Code: "GLOBAL-0019",
	},
}
`)

	routes := filepath.Join(dir, "routes.go")
	writeFixture(t, routes, `
package server
func registerRoutes() {
	http.HandleFunc("POST /api/v1/checkout", apiMiddleware(handleCheckout))
	http.HandleFunc("GET /api/v1/wms/batch/{id}/history", apiMiddleware(handleBatchHistory))
	http.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
}
`)

	sources = DriftSources{
		AppJSPath:        appJS,
		ErrorCatalogPath: errorCatalog,
		RouteFiles:       []string{routes},
	}
	return dir, sources
}

func hasWarningContaining(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestDriftGuardsStaleLastVerified(t *testing.T) {
	articlesDir, sources := driftFixtureSources(t)
	kbDir := filepath.Join(articlesDir, "kb")
	writeArticle(t, kbDir, "module-handbooks/stale.md", `---
title: Stale Article
section: Module Handbooks
order: 1
summary: An old article.
last_verified: 2020-01-01
screens: [pos]
---

# Stale Article

Body text.
`)
	writeArticle(t, kbDir, "module-handbooks/fresh.md", `---
title: Fresh Article
section: Module Handbooks
order: 2
summary: A recent article.
last_verified: 2026-08-30
screens: [oms]
---

# Fresh Article

Body text.
`)

	result, err := Build(kbDir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	warnings := DriftGuards(result.Articles, sources, now)

	if !hasWarningContaining(warnings, "module-handbooks/stale.md: last_verified 2020-01-01") {
		t.Errorf("expected a staleness warning for stale.md, got: %v", warnings)
	}
	if hasWarningContaining(warnings, "fresh.md: last_verified") {
		t.Errorf("did not expect a staleness warning for fresh.md, got: %v", warnings)
	}
}

func TestDriftGuardsDanglingScreen(t *testing.T) {
	articlesDir, sources := driftFixtureSources(t)
	kbDir := filepath.Join(articlesDir, "kb")
	writeArticle(t, kbDir, "module-handbooks/screens.md", `---
title: Screens Article
section: Module Handbooks
order: 1
summary: Uses one real and one fake screen id.
last_verified: 2026-08-30
screens: [pos, made-up-screen]
---

# Screens Article

Body text.
`)

	result, err := Build(kbDir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	warnings := DriftGuards(result.Articles, sources, time.Now())

	if !hasWarningContaining(warnings, `screens: lists "made-up-screen"`) {
		t.Errorf("expected a dangling screen warning, got: %v", warnings)
	}
	if hasWarningContaining(warnings, `screens: lists "pos"`) {
		t.Errorf("did not expect pos to be flagged, got: %v", warnings)
	}
}

func TestDriftGuardsDanglingEndpoint(t *testing.T) {
	articlesDir, sources := driftFixtureSources(t)
	kbDir := filepath.Join(articlesDir, "kb")
	writeArticle(t, kbDir, "module-handbooks/endpoints.md", `---
title: Endpoints Article
section: Module Handbooks
order: 1
summary: Cites a real and a fake endpoint.
last_verified: 2026-08-30
---

# Endpoints Article

Calls `+"`POST /api/v1/checkout`"+` for a real sale, and
`+"`GET /api/v1/wms/batch/42/history`"+` for a wildcarded one, and
`+"`POST /api/v1/totally/made-up`"+` which does not exist.
`)

	result, err := Build(kbDir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	warnings := DriftGuards(result.Articles, sources, time.Now())

	if !hasWarningContaining(warnings, `cites "POST /api/v1/totally/made-up"`) {
		t.Errorf("expected a dangling endpoint warning, got: %v", warnings)
	}
	if hasWarningContaining(warnings, `POST /api/v1/checkout", which matches`) {
		t.Errorf("did not expect the real literal endpoint to be flagged, got: %v", warnings)
	}
	if hasWarningContaining(warnings, `GET /api/v1/wms/batch/42/history`) {
		t.Errorf("did not expect the wildcard-matched endpoint to be flagged, got: %v", warnings)
	}
}

func TestDriftGuardsDanglingErrorCode(t *testing.T) {
	articlesDir, sources := driftFixtureSources(t)
	kbDir := filepath.Join(articlesDir, "kb")
	writeArticle(t, kbDir, "module-handbooks/codes.md", `---
title: Codes Article
section: Module Handbooks
order: 1
summary: Cites real and fake error codes, and an unrelated hyphenated token.
last_verified: 2026-08-30
---

# Codes Article

`+"`POSOFF-0238`"+` is real. `+"`POSOFF-9999`"+` is not, same known prefix.
Dates use ISO-8601 formatting, which is not an error code at all.
`)

	result, err := Build(kbDir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	warnings := DriftGuards(result.Articles, sources, time.Now())

	if !hasWarningContaining(warnings, `cites error code "POSOFF-9999"`) {
		t.Errorf("expected a dangling error code warning, got: %v", warnings)
	}
	if hasWarningContaining(warnings, `"POSOFF-0238"`) {
		t.Errorf("did not expect the real code to be flagged, got: %v", warnings)
	}
	if hasWarningContaining(warnings, "ISO-8601") {
		t.Errorf("did not expect an unrelated hyphenated token (unknown prefix) to be flagged, got: %v", warnings)
	}
}

func TestDriftGuardsUnmappedScreensSummary(t *testing.T) {
	articlesDir, sources := driftFixtureSources(t)
	kbDir := filepath.Join(articlesDir, "kb")
	writeArticle(t, kbDir, "module-handbooks/onescreen.md", `---
title: One Screen Article
section: Module Handbooks
order: 1
summary: Maps only pos.
last_verified: 2026-08-30
screens: [pos]
---

# One Screen Article

Body text.
`)

	result, err := Build(kbDir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	warnings := DriftGuards(result.Articles, sources, time.Now())

	// The fixture app.js has 3 real screens (pos, oms, reports); only pos is mapped.
	if !hasWarningContaining(warnings, "2 of 3 screens have no Knowledge Center article mapped") {
		t.Errorf("expected an unmapped-screens summary warning, got: %v", warnings)
	}
	if !hasWarningContaining(warnings, "oms") || !hasWarningContaining(warnings, "reports") {
		t.Errorf("expected the summary to name oms and reports, got: %v", warnings)
	}
}

func TestDriftGuardsMethodlessRouteIsAWildcard(t *testing.T) {
	articlesDir, sources := driftFixtureSources(t)
	kbDir := filepath.Join(articlesDir, "kb")
	writeArticle(t, kbDir, "module-handbooks/generic-doc.md", `---
title: Generic Doc Article
section: Module Handbooks
order: 1
summary: Cites the method-less generic-doc route with two different verbs.
last_verified: 2026-08-30
---

# Generic Doc Article

`+"`POST /api/v1/doc/GRN`"+` creates one, `+"`GET /api/v1/doc/GRN`"+` reads it back -
both against a route registered with no method prefix at all, which Go's own
mux treats as matching every method.
`)

	result, err := Build(kbDir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	warnings := DriftGuards(result.Articles, sources, time.Now())

	if hasWarningContaining(warnings, "api/v1/doc/GRN") {
		t.Errorf("a method-less registered route must match every cited HTTP method, got: %v", warnings)
	}
}
func TestDriftGuardsMissingSourcesDegradeGracefully(t *testing.T) {
	kbDir := t.TempDir()
	writeArticle(t, kbDir, "module-handbooks/only.md", `---
title: Only Article
section: Module Handbooks
order: 1
summary: The only article.
last_verified: 2026-08-30
screens: [pos]
---

# Only Article

Cites `+"`POST /api/v1/checkout`"+` and `+"`POSOFF-0238`"+`.
`)
	result, err := Build(kbDir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	missing := DriftSources{
		AppJSPath:        filepath.Join(kbDir, "does-not-exist.js"),
		ErrorCatalogPath: filepath.Join(kbDir, "does-not-exist.go"),
		RouteFiles:       []string{filepath.Join(kbDir, "does-not-exist-routes.go")},
	}
	warnings := DriftGuards(result.Articles, missing, time.Now())

	if !hasWarningContaining(warnings, "drift guard skipped (screen ids)") {
		t.Errorf("expected a skipped-screen-guard warning, got: %v", warnings)
	}
	if !hasWarningContaining(warnings, "drift guard skipped (endpoints)") {
		t.Errorf("expected a skipped-endpoint-guard warning, got: %v", warnings)
	}
	if !hasWarningContaining(warnings, "drift guard skipped (error codes)") {
		t.Errorf("expected a skipped-error-code-guard warning, got: %v", warnings)
	}
	// It must not panic or produce false claims about the article's real
	// content just because the comparison sources were unreadable.
	if hasWarningContaining(warnings, `screens: lists "pos"`) {
		t.Errorf("must not fabricate a dangling-screen finding when the guard was skipped, got: %v", warnings)
	}
}
