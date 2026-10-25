package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

// 47.11.1: the limiter's and the tenant-concurrency map's state stays
// bounded under key churn, and the real budgets still hold. Measured on the
// old code: 200,000 requests with rotating IPs, or rotating junk bearer
// tokens from one IP, left 200,000 keys (~22-24 MB) that were never freed -
// and the junk-token run was never refused once.

func heapNow() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestRateLimiterStaysBoundedUnderKeyChurn(t *testing.T) {
	rl := NewRateLimiter()
	const requests = 3 * rateLimitMaxKeys
	before := heapNow()
	for i := 0; i < requests; i++ {
		rl.Allow(fmt.Sprintf("2001:db8::%x:%x:default", i>>16, i&0xffff), 60, time.Minute)
	}
	after := heapNow()
	keys := len(rl.requests)
	runtime.KeepAlive(rl)
	// The table is full; at most one shared overflow key sits on top.
	if keys > rateLimitMaxKeys+1 {
		t.Fatalf("%d rotating keys left %d limiter keys; the table must stop at %d", requests, keys, rateLimitMaxKeys)
	}
	if grown := int64(after) - int64(before); grown > 40<<20 {
		t.Fatalf("%d rotating keys grew the heap by %d MB; it must stay bounded by the key cap", requests, grown>>20)
	}
	t.Logf("%d rotating keys -> %d keys, heap +%.1f MB", requests, keys, float64(int64(after)-int64(before))/1e6)
}

func TestRateLimiterSweepsExpiredKeys(t *testing.T) {
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	rl := NewRateLimiter()
	rl.clock = func() time.Time { return now }
	rl.lastSweep = now
	for i := 0; i < 1000; i++ {
		rl.Allow(fmt.Sprintf("198.51.100.%d:default", i), 60, time.Minute)
	}
	// Past the window and the sweep interval: one new request triggers the
	// sweep, and only that caller's key is left.
	now = now.Add(time.Minute + rateLimitSweepEvery)
	rl.Allow("198.51.100.250:search", 100, time.Minute)
	if len(rl.requests) != 1 {
		t.Fatalf("after the window passed, %d keys remain; want only the new caller's", len(rl.requests))
	}
}

func TestRateLimiterFullTableKeepsExistingBudgets(t *testing.T) {
	rl := NewRateLimiter()
	rl.maxKeys = 50
	if !rl.Allow("198.51.100.1:login", 5, time.Minute) {
		t.Fatal("a known caller's first request must pass")
	}
	for i := 0; i < 500; i++ { // fill the table and spill into overflow
		rl.Allow(fmt.Sprintf("198.51.101.%d:login", i), 5, time.Minute)
	}
	if len(rl.requests) > rl.maxKeys+1 {
		t.Fatalf("table grew to %d keys past its cap of %d", len(rl.requests), rl.maxKeys)
	}
	// The caller already in the table still has its own budget: 4 more pass,
	// the 6th is refused - not reset, not merged with the flood.
	for i := 2; i <= 5; i++ {
		if !rl.Allow("198.51.100.1:login", 5, time.Minute) {
			t.Fatalf("request %d of a 5/min budget was refused while the table was full", i)
		}
	}
	if rl.Allow("198.51.100.1:login", 5, time.Minute) {
		t.Fatal("the 6th request against a 5/min budget must be refused")
	}
	// Newcomers share one overflow budget once the table is full.
	if rl.Allow("198.51.102.9:login", 5, time.Minute) {
		t.Fatal("a newcomer to a full table must draw on the shared overflow budget, which the flood already spent")
	}
}

func TestTenantConcurrencyForgetsIdleTenants(t *testing.T) {
	db.InitDB(testConnStr())
	l := &tenantConcurrencyLimiter{inFlight: make(map[string]int), heavy: make(map[string]int)}
	for i := 0; i < 5000; i++ {
		tenant := fmt.Sprintf("junk-tenant-%d", i)
		if l.acquire(tenant, false) {
			l.release(tenant, false)
		}
	}
	if n := len(l.snapshot()); n != 0 {
		t.Fatalf("%d idle tenants are still held after every request ended", n)
	}
	// The cap itself still holds while requests are in flight.
	limit := perTenantMaxConcurrentRequestsFor("default")
	for i := 0; i < limit; i++ {
		if !l.acquire("default", false) {
			t.Fatalf("slot %d of %d refused", i+1, limit)
		}
	}
	if l.acquire("default", false) {
		t.Fatalf("slot %d must be refused at a cap of %d", limit+1, limit)
	}
	for i := 0; i < limit; i++ {
		l.release("default", false)
	}
	if n := len(l.snapshot()); n != 0 {
		t.Fatalf("the tenant must leave the map when its last request ends, %d entries left", n)
	}
}

// TestJunkTokensShareTheAddressBudget drives apiMiddleware itself: rotating
// junk bearer tokens from one address now draw on that address's budget, and
// two genuinely signed sessions behind one address (an office NAT) still get
// a budget each (Stage 30.5.9).
func TestJunkTokensShareTheAddressBudget(t *testing.T) {
	db.InitDB(testConnStr())
	do := func(ip, auth string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item", nil)
		req.RemoteAddr = ip + ":40000"
		req.Header.Set("Authorization", auth)
		rec := httptest.NewRecorder()
		apiMiddleware(func(w http.ResponseWriter, r *http.Request) {})(rec, req)
		return rec.Code
	}
	_, limit := rateLimitCategory("/api/v1/doc/Item", http.MethodGet)

	junkIP := fmt.Sprintf("198.51.100.%d", 100+time.Now().Nanosecond()%100)
	for i := 0; i < limit; i++ {
		if code := do(junkIP, fmt.Sprintf("Bearer junk%d.sig", i)); code == http.StatusTooManyRequests {
			t.Fatalf("junk request %d was refused before the address's budget of %d was spent", i+1, limit)
		}
	}
	if code := do(junkIP, "Bearer junk-final.sig"); code != http.StatusTooManyRequests {
		t.Fatalf("request %d with a fresh junk token must hit the address's %d/min budget, got %d", limit+1, limit, code)
	}

	officeIP := fmt.Sprintf("198.51.100.%d", 200+time.Now().Nanosecond()%50)
	alice := "Bearer " + engines.SignToken("rl-alice", "rl-alice", "Store Manager", "default", "HO", 0)
	bob := "Bearer " + engines.SignToken("rl-bob", "rl-bob", "Store Manager", "default", "HO", 0)
	for i := 0; i < limit; i++ {
		if code := do(officeIP, alice); code == http.StatusTooManyRequests {
			t.Fatalf("alice's request %d was refused inside her own budget", i+1)
		}
	}
	if code := do(officeIP, alice); code != http.StatusTooManyRequests {
		t.Fatalf("alice's request %d must be refused, got %d", limit+1, code)
	}
	if code := do(officeIP, bob); code == http.StatusTooManyRequests {
		t.Fatal("bob, a different signed session behind the same address, must keep his own budget")
	}
}

// 47.11.2: a refused request learns when one more fits - the oldest hit's exit
// from the window - so the client can wait that long instead of guessing.
func TestRateLimiterRetryAfterIsTheOldestHitsExit(t *testing.T) {
	rl := NewRateLimiter()
	start := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	now := start
	rl.clock = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if !rl.Allow("k", 3, time.Minute) {
			t.Fatalf("hit %d refused under the limit", i+1)
		}
		now = now.Add(10 * time.Second)
	}
	if rl.Allow("k", 3, time.Minute) {
		t.Fatal("the fourth hit in the window must be refused")
	}
	if got, want := rl.RetryAfter("k", time.Minute), 30*time.Second; got != want {
		t.Fatalf("RetryAfter = %v, want %v (the first hit at +0s leaves the window at +60s; now is +30s)", got, want)
	}
	if got := rl.RetryAfter("never-seen", time.Minute); got != 0 {
		t.Fatalf("an unknown key has no wait, got %v", got)
	}
}

// 47.11.3: report runs and exports may hold at most half of a tenant's slots;
// the other half stays free for sales, receipts and sign-ins.
func TestTenantConcurrencyKeepsHalfForWorkThatIsNotAReport(t *testing.T) {
	db.InitDB(testConnStr())
	l := &tenantConcurrencyLimiter{inFlight: make(map[string]int), heavy: make(map[string]int)}
	limit := perTenantMaxConcurrentRequestsFor("default")
	heavyCap := max(1, limit/2)
	for i := 0; i < heavyCap; i++ {
		if !l.acquire("default", true) {
			t.Fatalf("report slot %d of %d refused", i+1, heavyCap)
		}
	}
	if l.acquire("default", true) {
		t.Fatalf("report %d must wait: reports may hold %d of %d slots", heavyCap+1, heavyCap, limit)
	}
	for i := heavyCap; i < limit; i++ {
		if !l.acquire("default", false) {
			t.Fatalf("a sale was refused at slot %d of %d while reports held %d", i+1, limit, heavyCap)
		}
	}
	for i := 0; i < heavyCap; i++ {
		l.release("default", true)
	}
	for i := heavyCap; i < limit; i++ {
		l.release("default", false)
	}
	if n := len(l.snapshot()); n != 0 || len(l.heavy) != 0 {
		t.Fatalf("slots still held after every request ended: %v / %v", l.snapshot(), l.heavy)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/reports/run/stock-ledger", nil)
	if !isHeavyRead(r) || isHeavyRead(httptest.NewRequest(http.MethodPost, "/api/v1/checkout", nil)) {
		t.Fatal("a report run is heavy; a checkout is not")
	}
}
