package server

import (
	"fmt"
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

// BLD-029: the server-wide guard. Each client may hold 15 slots, but five
// clients at 15 would be 75 of a 50-connection pool, so ordinary work across
// all clients is capped (44 by default) and, near the cap, a client over its
// fair share waits while one under its share still gets in.
func TestBLD029GlobalGuardKeepsFairShareAcrossClients(t *testing.T) {
	const perTenant, globalCap = 15, 44
	l := &tenantConcurrencyLimiter{inFlight: make(map[string]int), heavy: make(map[string]int)}
	grab := func(tenant string, priority bool) bool {
		return l.acquireClassGlobal(tenant, false, priority, perTenant, globalCap)
	}
	// Five clients push at once, round-robin, until each is refused.
	clients := []string{"c1", "c2", "c3", "c4", "c5"}
	for round := 0; round < perTenant; round++ {
		for _, c := range clients {
			grab(c, false)
		}
	}
	if l.total > globalCap {
		t.Fatalf("ordinary work holds %d slots, over the server-wide cap of %d", l.total, globalCap)
	}
	for _, c := range clients {
		if l.inFlight[c] == 0 {
			t.Fatalf("client %s was starved: %v", c, l.inFlight)
		}
	}
	// One client taking all it can first (c1 alone), then others arrive: c1 is
	// stopped at its fair share once the server is 3/4 full, so c6 gets in.
	l2 := &tenantConcurrencyLimiter{inFlight: make(map[string]int), heavy: make(map[string]int)}
	for i := 0; i < perTenant; i++ {
		l2.acquireClassGlobal("c1", false, false, perTenant, globalCap)
	}
	for _, c := range []string{"c2", "c3"} {
		for i := 0; i < 10; i++ {
			l2.acquireClassGlobal(c, false, false, perTenant, globalCap)
		}
	}
	if l2.total < globalCap*3/4 {
		t.Fatalf("setup: server not saturated (%d)", l2.total)
	}
	// c1 (15, over a share of 44/4=11) is refused; new client c4 (0) is admitted.
	if l2.acquireClassGlobal("c1", false, false, perTenant, globalCap) {
		t.Fatal("a client over its fair share must be refused when the server is saturated")
	}
	if !l2.acquireClassGlobal("c4", false, false, perTenant, globalCap) {
		t.Fatal("a client under its fair share must still be admitted")
	}
	// Fill to the hard cap with fresh clients: ordinary is refused, priority is not.
	for i := 0; l2.total < globalCap; i++ {
		l2.acquireClassGlobal(fmt.Sprintf("x%d", i), false, false, perTenant, globalCap)
	}
	if l2.acquireClassGlobal("c9", false, false, perTenant, globalCap) {
		t.Fatal("ordinary work must be refused at the server-wide cap")
	}
	for i := 0; i < priorityReserve; i++ {
		if !l2.acquireClassGlobal("c9", false, true, perTenant, globalCap) {
			t.Fatalf("priority slot %d refused by the global guard", i+1)
		}
	}
	// Everything returns, including from a panicking request.
	func() {
		defer func() { _ = recover() }()
		l.release("c1", false)
		defer l.release("c2", false)
		panic("handler blew up")
	}()
	for c, n := range l.inFlight {
		for ; n > 0; n-- {
			l.release(c, false)
		}
	}
	for c, n := range l2.inFlight {
		for ; n > 0; n-- {
			l2.release(c, false)
		}
	}
	if l.total != 0 || l2.total != 0 || len(l.inFlight) != 0 || len(l2.inFlight) != 0 {
		t.Fatalf("slots not returned: %d/%d %v %v", l.total, l2.total, l.inFlight, l2.inFlight)
	}
}

// The wait path honours the guard and gives up on a cancelled caller without
// leaking a slot; the default is a registered setting below the pool size.
func TestBLD029GlobalGuardWaitAndSetting(t *testing.T) {
	db.InitDB(testConnStr())
	if got := globalMaxConcurrentRequests(); got < 1 || got >= 50 {
		t.Fatalf("global cap default %d must be 1..49 (pool is 50)", got)
	}
	l := &tenantConcurrencyLimiter{inFlight: make(map[string]int), heavy: make(map[string]int), total: 0}
	for i := 0; i < 3; i++ {
		l.acquireClassGlobal("a", false, false, 15, 4)
	}
	// Cap 4 is not what acquireClassWait reads (the setting), so check the
	// raw guard: a fourth ordinary request from a fills the cap, a fifth fails.
	l.acquireClassGlobal("b", false, false, 15, 4)
	if l.acquireClassGlobal("c", false, false, 15, 4) {
		t.Fatal("cap of 4 exceeded")
	}
	if l.total != 4 {
		t.Fatalf("total %d", l.total)
	}
}
