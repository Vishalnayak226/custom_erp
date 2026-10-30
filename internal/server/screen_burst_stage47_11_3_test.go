package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
)

// screenOpenReads is what one browser fires when a signed-in user opens a
// record list: the shell's boot reads (app.js init: me, modules, permissions,
// doctypes, labels, localization, environment, setup status, field formats,
// pending approvals, stores) and the list screen's own (view-documents.js:
// doctype meta, the first page with its count, a Link field's count).
var screenOpenReads = []string{
	"/api/v1/me", "/api/v1/me/modules", "/api/v1/me/permissions", "/api/v1/meta/doctypes", "/api/v1/labels",
	"/api/v1/localization", "/api/v1/system/environment", "/api/v1/setup/status", "/api/v1/meta/field-formats",
	"/api/v1/approval/pending", "/api/v1/me/stores",
	"/api/v1/doc/Department/meta", "/api/v1/doc/Department?count=true&limit=25&offset=0", "/api/v1/doc/Department?count=true",
}

func screenMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/me", apiMiddleware(handleGetProfile))
	mux.HandleFunc("GET /api/v1/me/permissions", apiMiddleware(handleMyPermissions))
	mux.HandleFunc("GET /api/v1/me/modules", apiMiddleware(handleMyModules))
	mux.HandleFunc("GET /api/v1/localization", apiMiddleware(handleGetLocalization))
	mux.HandleFunc("GET /api/v1/me/stores", apiMiddleware(handleMyStores))
	mux.HandleFunc("GET /api/v1/approval/pending", apiMiddleware(handleListPendingApprovals))
	mux.HandleFunc("GET /api/v1/system/environment", apiMiddleware(handleGetEnvironment))
	mux.HandleFunc("GET /api/v1/setup/status", apiMiddleware(handleSetupStatus))
	mux.HandleFunc("GET /api/v1/doc/{doctype}/meta", apiMiddleware(handleGetDocTypeMeta))
	mux.HandleFunc("GET /api/v1/meta/field-formats", apiMiddleware(handleFieldFormats))
	mux.HandleFunc("GET /api/v1/meta/doctypes", apiMiddleware(handleGetDocTypes))
	mux.HandleFunc("/api/v1/labels", apiMiddleware(handleLabels))
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("GET /api/v1/reports/run/{id}", apiMiddleware(moduleGate("reports", handleRunReport)))
	return mux
}

// 47.11.3: the load test of the normal-screen burst that once failed 8 of
// 30. One signed-in user, the tenant's default concurrency cap, the reads a
// record list makes on open fired twice over at the same instant plus two
// report runs - thirty at once. No ordinary read may be turned away (429 or
// 503); a report run in the same burst is the only one that may be told to
// wait, and only with a Retry-After.
func TestStage47113NormalScreenBurstIsNotTurnedAway(t *testing.T) {
	db.InitDB(testConnStr())
	mux := screenMux()
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	var burst []string
	burst = append(burst, screenOpenReads...)
	burst = append(burst, screenOpenReads...)
	burst = append(burst, "/api/v1/reports/run/po-commitments", "/api/v1/reports/run/po-commitments")
	if len(burst) != 30 {
		t.Fatalf("the burst is %d requests, want 30", len(burst))
	}

	type outcome struct {
		path       string
		code       int
		retryAfter string
		body       string
	}
	results := make([]outcome, len(burst))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, path := range burst {
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			<-start
			mux.ServeHTTP(rec, req)
			body := rec.Body.String()
			if len(body) > 160 {
				body = body[:160]
			}
			results[i] = outcome{path: path, code: rec.Code, retryAfter: rec.Header().Get("Retry-After"), body: body}
		}(i, path)
	}
	close(start)
	wg.Wait()

	var failed []string
	heavyWaited := 0
	for _, r := range results {
		heavy := isHeavyRead(httptest.NewRequest(http.MethodGet, r.path, nil))
		switch {
		case r.code == http.StatusTooManyRequests || r.code == http.StatusServiceUnavailable:
			if heavy && r.retryAfter != "" {
				heavyWaited++
				continue
			}
			failed = append(failed, fmt.Sprintf("%d %s (Retry-After %q) %s", r.code, r.path, r.retryAfter, r.body))
		case r.code >= 500:
			failed = append(failed, fmt.Sprintf("%d %s %s", r.code, r.path, r.body))
		}
	}
	sort.Strings(failed)
	if len(failed) > 0 {
		t.Fatalf("%d of %d requests of one screen open were turned away or failed (limit %d concurrent per tenant):\n%s",
			len(failed), len(burst), perTenantMaxConcurrentRequestsFor("default"), joinLines(failed))
	}
	t.Logf("30 at once: none turned away; report runs told to wait: %d", heavyWaited)
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += "- " + l + "\n"
	}
	return out
}
