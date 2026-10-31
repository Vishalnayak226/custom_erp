package server

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 49.10.5 (rest): connection-pool exhaustion. With every pooled connection
// held - a stuck query, a runaway report, a lock queue - the server answers
// in bounded time with a clear code instead of hanging, and recovers by
// itself once the connections are free. (One tenant's flood never gets
// there: the per-tenant cap turns it away first - BLD-029's test.)
func TestStage49105PoolExhaustionAnswersAndRecovers(t *testing.T) {
	db.InitDB(testConnStr())
	t.Cleanup(func() { db.InitDB(testConnStr()) }) // the normal pool for the next test
	// A running server has read the default client's cap already (settings
	// are cached); this test starts from that steady state.
	_ = perTenantMaxConcurrentRequestsFor("default")
	const poolSize = 3
	db.DB.SetMaxOpenConns(poolSize)

	ctx := context.Background()
	var held []*sql.Conn
	for i := 0; i < poolSize; i++ {
		c, err := db.DB.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	release := func() {
		for _, c := range held {
			_ = c.Close()
		}
		held = nil
	}
	defer release()
	if s := db.DB.Stats(); s.InUse != poolSize || s.MaxOpenConnections != poolSize {
		t.Fatalf("the pool is not exhausted: %+v", s)
	}

	// Work bound to a deadline gives up at the deadline, counted as a wait.
	waitsBefore := db.DB.Stats().WaitCount
	qctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	start := time.Now()
	var one int
	err := db.DB.QueryRowContext(qctx, "SELECT 1").Scan(&one)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("a bounded query on an exhausted pool: %v after %v", err, time.Since(start))
	}
	if db.DB.Stats().WaitCount <= waitsBefore {
		t.Fatal("the wait for a connection is not counted (the health check's wait_count)")
	}

	// The health check answers 503 within its bound and says why. It used to
	// call Ping with no deadline and wait for a free connection for ever.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", apiMiddleware(handleHealth))
	health := func() (*httptest.ResponseRecorder, time.Duration, bool) {
		done := make(chan *httptest.ResponseRecorder, 1)
		start := time.Now()
		go func() {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
			done <- rec
		}()
		select {
		case rec := <-done:
			return rec, time.Since(start), true
		case <-time.After(readinessDBTimeout + 5*time.Second):
			return nil, time.Since(start), false
		}
	}
	// Admission never holds the limiter's lock across a database read. A
	// client's cap needs its schema looked up, which waits for a connection;
	// that read used to happen under the one lock every client's admission
	// and release share, so one waiting request stalled every client's
	// sign-in and health check behind it.
	limiter := &tenantConcurrencyLimiter{inFlight: map[string]int{}, heavy: map[string]int{}}
	coldDone := make(chan bool, 1)
	go func() { coldDone <- limiter.acquireClassWait(nil, "t49105-cold", false, false, 0) }()
	time.Sleep(300 * time.Millisecond)
	if len(coldDone) != 0 {
		t.Fatal("the cold client's cap read did not wait for a connection - the test is not exercising the exhausted pool")
	}
	otherAdmitted := make(chan bool, 1)
	go func() { otherAdmitted <- limiter.acquireClass("default", false, true) }()
	select {
	case ok := <-otherAdmitted:
		if !ok {
			t.Fatal("another client's admission was refused")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("every client's admission stalled behind one client's database read")
	}

	rec, took, answered := health()
	if !answered {
		t.Fatalf("the health check hung on an exhausted pool (no answer in %v)", took)
	}
	var body struct {
		Status string `json:"status"`
		Pool   struct {
			Max   int `json:"max_open_connections"`
			InUse int `json:"in_use"`
		} `json:"db_pool"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body.Status != "unavailable" || body.Pool.InUse != poolSize || body.Pool.Max != poolSize {
		t.Fatalf("health on an exhausted pool: %d %s (after %v)", rec.Code, rec.Body.String(), took)
	}
	if took > readinessDBTimeout+2*time.Second {
		t.Fatalf("health took %v, its bound is %v", took, readinessDBTimeout)
	}

	// Free the connections: the same calls work again, nothing to restart,
	// and the request that was waiting is answered.
	release()
	select {
	case <-coldDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting request was not answered once the pool was free")
	}
	rec, took, answered = health()
	if !answered || rec.Code != http.StatusOK {
		t.Fatalf("health after the pool is free: answered=%v %v", answered, rec)
	}
	if took > 2*time.Second {
		t.Fatalf("recovery took %v", took)
	}
	qctx, cancel = context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := db.DB.QueryRowContext(qctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("the pool did not recover: %v", err)
	}
}
