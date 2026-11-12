package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"testing"
	"time"
)

// Stage 49.3.3: a token whose client is not registered (purged) or not
// serving is refused - for an extension token too, which carries no user and
// so skips the per-user live-state check - instead of falling through to the
// default client's schema.
func TestStage49TokenForAnUnregisteredClientIsRefused(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	ip := mfaTestIP()

	gone := "purged-" + time.Now().Format("150405.000000")
	if rec := doRequestFromIP(t, mux.ServeHTTP, "GET", "/api/v1/doc/Item?limit=1", engines.SignExtensionToken(gone, "Item", time.Hour), nil, ip); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an extension token of an unregistered client: %d %s, want 401", rec.Code, rec.Body.String())
	}
	if rec := doRequestFromIP(t, mux.ServeHTTP, "GET", "/api/v1/doc/Item?limit=1", engines.SignToken("admin", "admin", engines.RoleSuperAdmin, gone, "HO", 1), nil, ip); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a session token of an unregistered client: %d %s, want 401", rec.Code, rec.Body.String())
	}
	// The default client's own extension token still works.
	if rec := doRequestFromIP(t, mux.ServeHTTP, "GET", "/api/v1/doc/Item?limit=1", engines.SignExtensionToken("default", "Item", time.Hour), nil, ip); rec.Code != http.StatusOK {
		t.Fatalf("the default client's extension token: %d %s, want 200", rec.Code, rec.Body.String())
	}
}
