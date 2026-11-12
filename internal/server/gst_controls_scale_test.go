package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"strings"
	"testing"
)

// Stage 47.16.4b (G2): the scale test, asked for on a real client through its
// route, is refused with PLATFORM-0318 before anything is seeded or posted.
func TestStage47164bScaleTestRouteRefusesARealClient(t *testing.T) {
	db.InitDB(testConnStr())
	var before, seededBefore int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.gl_postings`).Scan(&before)
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.inventory_availability WHERE sku = 'BAR-SCALE'`).Scan(&seededBefore)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/admin/scale-test", apiMiddleware(handleScaleTest))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/admin/scale-test", token,
		map[string]interface{}{"num_stores": 2, "num_workers": 1, "num_transactions": 2})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "PLATFORM-0318") {
		t.Fatalf("scale test on the default client: %d %s, want 422 PLATFORM-0318", rec.Code, rec.Body.String())
	}
	var after, seeded int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.gl_postings`).Scan(&after)
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.inventory_availability WHERE sku = 'BAR-SCALE'`).Scan(&seeded)
	if after != before || seeded != seededBefore {
		t.Fatalf("a refused scale test posted %d row(s) and changed %d stock row(s)", after-before, seeded-seededBefore)
	}
}
