package engines

import (
	"custom_erp/db"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
)

// TestStage134ConcurrentClaims (49.10.2): the same webhook delivery arriving
// on 20 connections at once is processed by exactly one of them, and five
// servers racing for the scheduler lock elect exactly one leader.
func TestStage134ConcurrentClaims(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`CREATE TABLE IF NOT EXISTS ` + schema + `.inbound_webhook_receipts (source VARCHAR(120) NOT NULL, event_id VARCHAR(200) NOT NULL, received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (source, event_id))`); err != nil {
		t.Fatal(err)
	}
	var owners, errs int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dup, err := ClaimInboundWebhook(tenantID, "race:134", "evt-race", []byte("x"))
			if err != nil {
				atomic.AddInt32(&errs, 1)
			} else if !dup {
				atomic.AddInt32(&owners, 1)
			}
		}()
	}
	wg.Wait()
	if owners != 1 || errs != 0 {
		t.Fatalf("one delivery, 20 arrivals: %d processed it, %d errors", owners, errs)
	}

	var held bool
	_ = db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND objid = ($1::bigint & x'ffffffff'::bigint)::oid AND granted)`, schedulerLeaderLockKey).Scan(&held)
	if held {
		t.Skip("another process on this database holds the scheduler lock")
	}
	electors := make([]*schedulerElector, 5)
	for i := range electors {
		p, err := sql.Open("postgres", testConnStr())
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		electors[i] = &schedulerElector{pool: p}
		defer electors[i].resign()
	}
	var leaders int32
	for _, e := range electors {
		wg.Add(1)
		go func(e *schedulerElector) {
			defer wg.Done()
			if e.leader() {
				atomic.AddInt32(&leaders, 1)
			}
		}(e)
	}
	wg.Wait()
	if leaders != 1 {
		t.Fatalf("five servers racing for the lock: %d leaders", leaders)
	}
}
