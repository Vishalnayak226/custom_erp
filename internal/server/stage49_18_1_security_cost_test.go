package server

// Stage 49.18.1 - what the security controls cost, measured. The release
// budget (cmd/releasebudget) already holds binary, static, KB, database and
// archive bytes; this measures the per-request side: what the authenticated
// middleware chain adds to a request (p50/p95/p99) and how many database
// statements it issues. Budgets are loose on purpose - they catch an uncached
// lookup or an extra query per request, not machine noise - and the measured
// numbers are logged for the ledger.
//
// Statements are counted by wrapping the real driver and charging only the
// ones issued from the request's own goroutine: a database-wide counter also
// counted whatever background goroutines earlier tests in this package left
// running (0.49 per request alone, 5.79 inside the full suite).

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * p)
	return sorted[i]
}

func goroutineID() uint64 {
	var buf [64]byte
	b := bytes.TrimPrefix(buf[:runtime.Stack(buf[:], false)], []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i > 0 {
		id, _ := strconv.ParseUint(string(b[:i]), 10, 64)
		return id
	}
	return 0
}

// statementCounter is a driver.Connector over the real driver that counts the
// statements one goroutine sends.
type statementCounter struct {
	inner driver.Driver
	dsn   string
	gid   uint64
	stmts int64
	mu    sync.Mutex
	seen  map[string]int // statement text (first line, trimmed) -> times
}

func (c *statementCounter) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.inner.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: conn, c: c}, nil
}

func (c *statementCounter) Driver() driver.Driver { return c.inner }

// countingConn exposes only Prepare/Begin/Close, so database/sql sends every
// query through Prepare - one count per statement.
type countingConn struct {
	driver.Conn
	c *statementCounter
}

func (cc *countingConn) Prepare(query string) (driver.Stmt, error) {
	if goroutineID() == cc.c.gid {
		atomic.AddInt64(&cc.c.stmts, 1)
		text := strings.Join(strings.Fields(query), " ")
		if len(text) > 90 {
			text = text[:90]
		}
		cc.c.mu.Lock()
		cc.c.seen[text]++
		cc.c.mu.Unlock()
	}
	return cc.Conn.Prepare(query)
}

func TestStage49181AuthenticatedRequestCost(t *testing.T) {
	db.InitDB(testConnStr())
	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	chain := securityHeaders(apiMiddleware(ok))
	bare := http.HandlerFunc(ok)

	run := func(h http.Handler, n int) []time.Duration {
		out := make([]time.Duration, 0, n)
		for i := 0; i < n; i++ {
			if i%40 == 0 {
				globalLimiter = NewRateLimiter() // the limiter is measured, not tripped
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.SetPathValue("doctype", "Item")
			rec := httptest.NewRecorder()
			began := time.Now()
			h.ServeHTTP(rec, req)
			out = append(out, time.Since(began))
			if rec.Code != http.StatusOK {
				t.Fatalf("request %d answered %d: %s", i, rec.Code, rec.Body.String())
			}
		}
		sort.Slice(out, func(a, b int) bool { return out[a] < out[b] })
		return out
	}
	run(chain, 30) // warm the live-state cache and the pool

	const n = 400
	secured := run(chain, n)
	plain := run(bare, n)

	// Statements per request, charged to this goroutine only.
	counter := &statementCounter{inner: db.DB.Driver(), dsn: testConnStr(), gid: goroutineID(), seen: map[string]int{}}
	counting := sql.OpenDB(counter)
	real := db.DB
	db.DB = counting
	const counted = 200
	run(chain, counted)
	db.DB = real
	counting.Close()
	globalLimiter = NewRateLimiter()
	perRequest := float64(atomic.LoadInt64(&counter.stmts)) / counted

	added := percentile(secured, 0.95) - percentile(plain, 0.95)
	t.Logf("49.18.1 authenticated GET through securityHeaders+apiMiddleware: p50 %v p95 %v p99 %v (bare handler p95 %v); %.2f database statements per request",
		percentile(secured, 0.50), percentile(secured, 0.95), percentile(secured, 0.99), percentile(plain, 0.95), perRequest)
	if added > 25*time.Millisecond {
		t.Errorf("the security chain adds %v at p95 - over the 25 ms budget; look for an uncached lookup per request", added)
	}
	for text, times := range counter.seen {
		t.Logf("  %.2f per request: %s", float64(times)/counted, text)
	}
	// 49.18.8: the sandbox row and the doctype's module are cached; the
	// module entitlement is read fresh on purpose, so one statement remains.
	// The half-statement headroom absorbs a cache refresh during a slow run
	// (the release runs five test jobs side by side - an exact budget failed
	// once there); one new query per request still fails it.
	if perRequest > 1.5 {
		t.Errorf("%.2f database statements per authenticated read - over the budget of 1 (the fresh entitlement read); something stopped being cached", perRequest)
	}
}
