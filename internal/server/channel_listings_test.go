package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 146.4.5: the listing routes are classified, the commands are for an
// administrator only, and an untouched channel lists nothing.
func TestStage14645ListingRoutes(t *testing.T) {
	db.InitDB(testConnStr())
	for _, k := range []string{"GET /api/v1/marketplace/channels/{channel}/listings", "POST /api/v1/marketplace/channels/{channel}/listings/pull",
		"GET /api/v1/marketplace/channels/{channel}/listings/link-preview", "POST /api/v1/marketplace/channels/{channel}/listings/link",
		"POST /api/v1/marketplace/channels/{channel}/listings/stock-sync"} {
		if _, ok := routeCapabilities[k]; !ok {
			t.Fatalf("%s has no route_capabilities entry", k)
		}
	}
	call := func(h http.HandlerFunc, method, body, role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/x", strings.NewReader(body))
		req.SetPathValue("channel", "T14645-NONE")
		req.Header.Set("Resolved-Tenant-ID", "default")
		req.Header.Set("Resolved-Role", role)
		req.Header.Set("Resolved-Username", "t14645")
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	for name, h := range map[string]http.HandlerFunc{"pull": handleChannelListingsPull, "link": handleChannelListingsLink, "stock-sync": handleChannelListingsStockSync} {
		if rec := call(h, http.MethodPost, `{"ref":"x","on":false}`, engines.RoleCashier); rec.Code != http.StatusForbidden {
			t.Fatalf("a cashier reached %s: %d", name, rec.Code)
		}
	}
	rec := call(handleChannelListings, http.MethodGet, "", engines.RoleCashier)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"can_pull":false`) {
		t.Fatalf("list of an unknown channel: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(handleChannelListingsPull, http.MethodPost, "", engines.RoleSuperAdmin); rec.Code == http.StatusOK {
		t.Fatalf("pulling an unknown channel succeeded: %s", rec.Body.String())
	}
}
