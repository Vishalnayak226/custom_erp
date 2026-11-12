package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"custom_erp/db"
)

// Stage 49.13.2: when ordinary work fills a tenant's slots, sign-in, health,
// cancellation and the emergency controls still get priorityReserve more -
// and no more than that.
func TestStage49132PriorityLaneSurvivesAFullTenant(t *testing.T) {
	db.InitDB(testConnStr())
	l := &tenantConcurrencyLimiter{inFlight: make(map[string]int), heavy: make(map[string]int)}
	limit := perTenantMaxConcurrentRequestsFor("default")
	for i := 0; i < limit; i++ {
		if !l.acquire("default", false) {
			t.Fatalf("ordinary slot %d of %d refused", i+1, limit)
		}
	}
	if l.acquire("default", false) {
		t.Fatal("ordinary work beyond the cap must be refused")
	}
	for i := 0; i < priorityReserve; i++ {
		if !l.acquireClass("default", false, true) {
			t.Fatalf("priority slot %d of %d refused while ordinary work filled the cap", i+1, priorityReserve)
		}
	}
	if l.acquireClass("default", false, true) {
		t.Fatal("the priority lane is bounded too")
	}
	for i := 0; i < limit+priorityReserve; i++ {
		l.release("default", false)
	}
	if len(l.snapshot()) != 0 {
		t.Fatalf("slots still held: %v", l.snapshot())
	}

	priority := map[string]bool{
		"POST /api/v1/login":                           true,
		"GET /api/v1/health":                           true,
		"POST /api/v1/auth/mfa/verify":                 true,
		"POST /api/v1/admin/security/freeze-exports":   true,
		"POST /api/v1/reports/export/abc/cancel":       true,
		"GET /api/v1/reports/export/abc/cancel":        false,
		"POST /api/v1/checkout":                        false,
		"GET /api/v1/reports/run/stock-ledger":         false,
		"GET /api/v1/admin/security-groups-not-a-lane": false,
	}
	for route, want := range priority {
		method, path, _ := cutRoute(route)
		if got := isPriorityRequest(httptest.NewRequest(method, path, nil)); got != want {
			t.Errorf("%s priority = %v, want %v", route, got, want)
		}
	}
}

func cutRoute(route string) (method, path string, ok bool) {
	for i := 0; i < len(route); i++ {
		if route[i] == ' ' {
			return route[:i], route[i+1:], true
		}
	}
	return http.MethodGet, route, false
}
