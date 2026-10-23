package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"os"
	"sync"
	"time"
)

// Stage 134.7 (47.11.5): one scheduler leader per database. Every scheduled
// sweep (outbox, alerts, auto-cancel, depreciation, channel sync, ...) runs
// its tick only in the server process holding the leader lock, so two
// servers on one database never run the same sweep twice. The lock is a
// PostgreSQL session advisory lock held on one dedicated connection: when the
// leader process dies its connection closes, PostgreSQL frees the lock, and
// the next follower to tick takes over - no lease to expire, no clock.
//
// The job runner's own claim loop is not gated: it claims rows with
// FOR UPDATE SKIP LOCKED and is meant to run on every server.
//
// SCHEDULER_LEADER=off turns the election off (every process runs every
// sweep, the old behaviour) - for a single-process setup sharing its
// database with another tool that would otherwise hold the lock.

const schedulerLeaderLockKey int64 = 0x45_52_50_53_43_48_44 // "ERPSCHD"

var schedulerElection = os.Getenv("SCHEDULER_LEADER") != "off"

// schedulerElector holds (or tries for) the leader lock on one pool.
type schedulerElector struct {
	mu      sync.Mutex
	pool    *sql.DB
	conn    *sql.Conn
	checked time.Time
}

var defaultElector = &schedulerElector{}

// IsSchedulerLeader reports whether this process runs scheduled sweeps now.
func IsSchedulerLeader() bool {
	if !schedulerElection {
		return true
	}
	defaultElector.mu.Lock()
	defaultElector.pool = db.DB
	defaultElector.mu.Unlock()
	return defaultElector.leader()
}

// leader keeps the lock while its connection lives, and tries for it when
// this process does not hold it.
func (e *schedulerElector) leader() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e.conn != nil {
		if time.Since(e.checked) < 30*time.Second {
			return true
		}
		if err := e.conn.PingContext(ctx); err == nil {
			e.checked = time.Now()
			return true
		}
		_ = e.conn.Close() // the session (and with it the lock) is gone
		e.conn = nil
	}
	if e.pool == nil {
		return false
	}
	conn, err := e.pool.Conn(ctx)
	if err != nil {
		return false
	}
	var got bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, schedulerLeaderLockKey).Scan(&got); err != nil || !got {
		_ = conn.Close()
		return false
	}
	e.conn, e.checked = conn, time.Now()
	return true
}

// resign gives the lock up (tests; a graceful shutdown would too).
func (e *schedulerElector) resign() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.conn != nil {
		_, _ = e.conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, schedulerLeaderLockKey)
		_ = e.conn.Close()
		e.conn = nil
	}
}
