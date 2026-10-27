package server

// BLD-029 - noisy-neighbour containment, measured on the real admission path.
// Two clients go through apiMiddleware: tenant A floods requests that each
// hold a pooled database connection, tenant B keeps working. Three campaigns:
// contention (A's long reports), a spike (A's ordinary requests), recovery
// (A served again once its burst ends). The per-tenant cap (24.30, 15 by
// default; heavy reads at most half - 47.11.3) must turn A's excess away fast
// with Retry-After, and B must neither fail nor queue behind A.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

func TestBLD029NoisyNeighbourIsContained(t *testing.T) {
	db.InitDB(testConnStr())
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	tenantB, schemaB := "bld029b"+suffix, "tenant_bld029b"+suffix
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + schemaB + " CASCADE")
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantB)
		db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantB)
		engines.InvalidateTenantLifecycleCache(tenantB)
	}()
	if _, err := engines.ProvisionTenantSchema(tenantB, schemaB, "0.1.0-test"); err != nil {
		t.Fatalf("provision tenant B: %v", err)
	}
	var adminB string
	var versionB int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(credential_version, 1) FROM %s.users WHERE username = 'admin'`, schemaB)).Scan(&adminB, &versionB); err != nil {
		t.Fatalf("tenant B admin: %v", err)
	}
	tokenA := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	tokenB := engines.SignToken(adminB, "admin", "HR/Admin", tenantB, "HO", versionB)
	engines.ResetLiveUserStateCache()

	hold := func(w http.ResponseWriter, r *http.Request) {
		// Holds one pooled connection, like a long report or a slow save.
		if _, err := db.DB.ExecContext(r.Context(), `SELECT pg_sleep(0.3)`); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	quick := func(w http.ResponseWriter, r *http.Request) {
		var one int
		if err := db.DB.QueryRowContext(r.Context(), `SELECT 1`).Scan(&one); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	send := func(h http.HandlerFunc, token, path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.RemoteAddr = fmt.Sprintf("10.29.%d.%d:1234", time.Now().UnixNano()%250, time.Now().UnixNano()%200)
		rec := httptest.NewRecorder()
		apiMiddleware(h)(rec, req)
		if rec.Code == http.StatusServiceUnavailable && rec.Header().Get("Retry-After") == "" {
			t.Errorf("a refused request must say when to retry")
		}
		return rec.Code
	}

	campaign := func(name, pathA string, floodA, capA int) {
		globalLimiter = NewRateLimiter()
		var admitted, refused, peakInUse int64
		var wg sync.WaitGroup
		stop := make(chan struct{})
		go func() { // sample the shared pool while A floods
			for {
				select {
				case <-stop:
					return
				default:
					if n := int64(db.DB.Stats().InUse); n > atomic.LoadInt64(&peakInUse) {
						atomic.StoreInt64(&peakInUse, n)
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
		}()
		for i := 0; i < floodA; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				switch send(hold, tokenA, pathA) {
				case http.StatusOK:
					atomic.AddInt64(&admitted, 1)
				case http.StatusServiceUnavailable:
					atomic.AddInt64(&refused, 1)
				}
			}()
		}
		time.Sleep(50 * time.Millisecond) // A's burst is in flight
		var latB []time.Duration
		failedB := 0
		for i := 0; i < 20; i++ {
			began := time.Now()
			if code := send(quick, tokenB, "/api/v1/doc/Item"); code != http.StatusOK {
				failedB++
			}
			latB = append(latB, time.Since(began))
		}
		wg.Wait()
		close(stop)
		sort.Slice(latB, func(a, b int) bool { return latB[a] < latB[b] })
		p95 := latB[len(latB)*95/100]
		t.Logf("BLD-029 %s: A %d sent, %d admitted, %d refused fast; pool peak %d in use of %d; B 20 requests, %d failed, p50 %v p95 %v",
			name, floodA, admitted, refused, peakInUse, db.DB.Stats().MaxOpenConnections, failedB, latB[len(latB)/2], p95)
		if failedB > 0 {
			t.Errorf("%s: tenant B lost %d of 20 requests to tenant A's load", name, failedB)
		}
		// The containment itself: A holds at most its cap of the shared pool,
		// so B finds connections free. Before the slot moved ahead of the
		// lookups and refusals stopped writing a row each, this peaked at 50.
		if peakInUse > int64(capA+5) {
			t.Errorf("%s: the shared pool peaked at %d connections in use - tenant A's cap is %d, so its refused requests are still spending connections", name, peakInUse, capA)
		}
		// B's median is untouched; its first request can meet the burst's
		// CPU on this machine (the per-session rate limit throttles such a
		// burst in production), so only a gross stall fails.
		if med := latB[len(latB)/2]; med > 50*time.Millisecond || latB[len(latB)-1] > time.Second {
			t.Errorf("%s: tenant B median %v, worst %v - it is waiting on tenant A", name, med, latB[len(latB)-1])
		}
		if refused == 0 {
			t.Errorf("%s: tenant A sent %d at once and none was turned away - the per-tenant cap did not engage", name, floodA)
		}
	}

	capA := engines.GetSettingInt("default", "platform.per_tenant_max_concurrent_requests")
	campaign("contention (A's long reports)", "/api/v1/reports/current-stock", 40, max(1, capA/2))
	campaign("spike (A's ordinary requests)", "/api/v1/doc/Item", 100, capA)

	// Recovery: A's slots are all returned and A is served again at once.
	tenantConcurrency.mu.Lock()
	leftA, leftHeavy := tenantConcurrency.inFlight["default"], tenantConcurrency.heavy["default"]
	tenantConcurrency.mu.Unlock()
	if leftA != 0 || leftHeavy != 0 {
		t.Errorf("after the burst tenant A still holds %d slots (%d heavy)", leftA, leftHeavy)
	}
	globalLimiter = NewRateLimiter()
	if code := send(quick, tokenA, "/api/v1/doc/Item"); code != http.StatusOK {
		t.Errorf("recovery: tenant A answered %d after its burst ended", code)
	}
	globalLimiter = NewRateLimiter()
}
