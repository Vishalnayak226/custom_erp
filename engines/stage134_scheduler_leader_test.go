package engines

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestStage134SchedulerLeader (47.11.5): two servers on one database (two
// connection pools here) - only one leads, so a sweep runs once; when the
// leader's session ends (a killed process), the other takes over on its next
// tick.
func TestStage134SchedulerLeader(t *testing.T) {
	open := func() *sql.DB {
		p, err := sql.Open("postgres", testConnStr())
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	poolA, poolB := open(), open()
	defer poolA.Close()
	defer poolB.Close()
	// Another process (a dev server) may lead this database already.
	var held bool
	if err := poolA.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND objid = ($1::bigint & x'ffffffff'::bigint)::oid AND granted)`, schedulerLeaderLockKey).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if held {
		t.Skip("another process on this database holds the scheduler lock")
	}
	a, b := &schedulerElector{pool: poolA}, &schedulerElector{pool: poolB}
	defer a.resign()
	defer b.resign()
	if !a.leader() {
		t.Fatal("the first server leads")
	}
	if b.leader() {
		t.Fatal("a second server on the same database does not lead while the first does")
	}
	if !a.leader() {
		t.Fatal("the leader keeps leading")
	}

	// Kill: end A's session without unlocking, as a crashed process would.
	a.mu.Lock()
	var pid int
	_ = a.conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid)
	a.mu.Unlock()
	if _, err := poolB.Exec(`SELECT pg_terminate_backend($1)`, pid); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !b.leader() {
		if time.Now().After(deadline) {
			t.Fatal("the second server takes over once the leader's session is gone")
		}
		time.Sleep(100 * time.Millisecond)
	}
	a.mu.Lock()
	a.checked = time.Time{} // A notices on its next check
	a.mu.Unlock()
	if a.leader() {
		t.Fatal("the old leader does not lead again while the new one holds the lock")
	}
}
