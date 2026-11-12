package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 47.12 / 49.17.3: the go-live routes are admin only, and the sign-off
// door refuses with its catalog code while a blocking check is open.
func TestGoLiveRoutesAreAdminOnly(t *testing.T) {
	for _, pattern := range []string{
		"GET /api/v1/golive/scope", "POST /api/v1/golive/scope", "GET /api/v1/golive/graph", "POST /api/v1/golive/graph/scope",
		"GET /api/v1/golive/packs/{pack}", "POST /api/v1/golive/packs/{pack}/evidence", "POST /api/v1/golive/packs/{pack}/waive",
		"POST /api/v1/golive/packs/{pack}/waive/end", "POST /api/v1/golive/packs/{pack}/signoff",
	} {
		if ok, _, found := checkRouteCapability(pattern, engines.RoleStoreManager); !found || ok {
			t.Errorf("%s: found=%v allowed=%v for a Store Manager, want classified and refused", pattern, found, ok)
		}
		if ok, _, found := checkRouteCapability(pattern, engines.RoleSuperAdmin); !found || !ok {
			t.Errorf("%s: found=%v allowed=%v for the admin, want allowed", pattern, found, ok)
		}
	}
	for _, code := range []string{"GLOBAL-9110", "GLOBAL-9111", "GLOBAL-9112", "GLOBAL-9113", "GLOBAL-9114", "GLOBAL-9115", "GLOBAL-9116", "GLOBAL-9117"} {
		if _, ok := errorCatalog[code]; !ok {
			t.Errorf("%s is not in the error catalog", code)
		}
	}
}

// An extension token skips the route-capability check, so the gate must refuse
// it, and every go-live route must go through the gate.
func TestGoLiveGateRefusesExtensionTokens(t *testing.T) {
	db.InitDB(testConnStr())
	reached := false
	h := goLiveGate(func(http.ResponseWriter, *http.Request) { reached = true })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
	req.Header.Set("Resolved-Purpose", "extension")
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusForbidden || reached {
		t.Fatalf("extension token: status %d reached=%v, want 403 and not reached", w.Code, reached)
	}
	req.Header.Del("Resolved-Purpose")
	w = httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK || !reached {
		t.Fatalf("admin session: status %d reached=%v, want through", w.Code, reached)
	}
	src, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	all := regexp.MustCompile(`"[A-Z]+ /api/v1/golive/[^"]*", apiMiddleware\(([a-zA-Z]+)`).FindAllSubmatch(src, -1)
	if len(all) != 9 {
		t.Fatalf("found %d go-live routes, want 9", len(all))
	}
	for _, m := range all {
		if string(m[1]) != "goLiveGate" {
			t.Errorf("a go-live route opens %s without goLiveGate", m[1])
		}
	}
}

func TestGoLiveSignOffDoorRefusesWhileOpen(t *testing.T) {
	db.InitDB(testConnStr())
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte(`{"note":"go"}`)))
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
	req.Header.Set("Resolved-Username", "asha")
	req.SetPathValue("pack", "golive")
	rec := httptest.NewRecorder()
	handleGoLiveSignOff(rec, req)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "GLOBAL-9114") {
		t.Fatalf("sign-off door: %d %s, want 422 GLOBAL-9114", rec.Code, rec.Body.String())
	}
}
