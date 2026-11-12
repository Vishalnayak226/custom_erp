package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// 49.5.7 (process kill): a command whose process dies at any point leaves no half state, and the
// retry completes it exactly once. The business write and the Completed record commit together,
// so an abandoned claim proves nothing was committed.

func killTestEffects(t *testing.T, schema, marker string) (n int) {
	t.Helper()
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.documents WHERE doctype = 'CourierTrackingEvent' AND data->>'marker' = $1`, marker).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStage4957ProcessKilledMidCommandRetryCompletesOnce(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	marker := NewDocID("KILL")
	key := NewDocID("kill-key")
	defer db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype = 'CourierTrackingEvent' AND data->>'marker' = $1`, marker)
	defer db.DB.Exec(`DELETE FROM `+schema+`.command_idempotency WHERE idempotency_key LIKE $1`, "%"+key)
	digest := CommandDigest(map[string]string{"marker": marker})

	// Attempt 1: the process dies after the business write, before commit.
	c1, err := ClaimCommand("default", "test.kill", key, digest, "corr-1")
	if err != nil || c1.Outcome != ClaimAcquired {
		t.Fatalf("first claim: %+v %v", c1, err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"marker": marker})
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id,doctype,data,status,created_by) VALUES ($1,'CourierTrackingEvent',$2,'Processed','system')`, schema), NewDocID("CTE"), data); err != nil {
		t.Fatal(err)
	}
	if err := CompleteCommandTx(tx, schema, c1.Key, "DOC-1", map[string]interface{}{"ok": true}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback() // the kill: nothing of the write or the Completed record survives
	if n := killTestEffects(t, schema, marker); n != 0 {
		t.Fatalf("a killed command left %d business row(s)", n)
	}

	// A retry while the dead process's lease still runs is told "in progress", not run twice.
	c2, err := ClaimCommand("default", "test.kill", key, digest, "corr-2")
	if err != nil || c2.Outcome != ClaimInProgress {
		t.Fatalf("retry inside the lease: %+v %v, want in_progress", c2, err)
	}

	// The lease lapses; the retry takes the claim over and completes it.
	if _, err := db.DB.Exec(`UPDATE `+schema+`.command_idempotency SET lease_expires_at = CURRENT_TIMESTAMP - interval '1 second' WHERE idempotency_key = $1`, c1.Key); err != nil {
		t.Fatal(err)
	}
	c3, err := ClaimCommand("default", "test.kill", key, digest, "corr-3")
	if err != nil || c3.Outcome != ClaimAcquired {
		t.Fatalf("retry after the lease: %+v %v, want acquired", c3, err)
	}
	tx, _ = db.DB.Begin()
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id,doctype,data,status,created_by) VALUES ($1,'CourierTrackingEvent',$2,'Processed','system')`, schema), NewDocID("CTE"), data); err != nil {
		t.Fatal(err)
	}
	if err := CompleteCommandTx(tx, schema, c3.Key, "DOC-1", map[string]interface{}{"ok": true}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Every later retry is a replay of the one result; the work is not repeated.
	for i := 0; i < 3; i++ {
		c, err := ClaimCommand("default", "test.kill", key, digest, "corr-r")
		if err != nil || c.Outcome != ClaimReplay || c.DocumentID != "DOC-1" {
			t.Fatalf("replay %d: %+v %v", i, c, err)
		}
	}
	if n := killTestEffects(t, schema, marker); n != 1 {
		t.Fatalf("the retried command left %d business rows, want exactly 1", n)
	}
}

// A process killed before it records anything leaves an abandoned claim; a failed command frees it at once.
func TestStage4957FailedCommandFreesItsKeyForTheRetry(t *testing.T) {
	spInitDB()
	key := NewDocID("kill-fail")
	schema, _ := db.GetTenantSchema("default")
	defer db.DB.Exec(`DELETE FROM `+schema+`.command_idempotency WHERE idempotency_key LIKE $1`, "%"+key)
	digest := CommandDigest(map[string]string{"k": key})
	c1, err := ClaimCommand("default", "test.kill", key, digest, "c1")
	if err != nil || c1.Outcome != ClaimAcquired {
		t.Fatalf("claim: %+v %v", c1, err)
	}
	FailCommand("default", c1.Key, "simulated failure")
	c2, err := ClaimCommand("default", "test.kill", key, digest, "c2")
	if err != nil || (c2.Outcome != ClaimFailedEarlier && c2.Outcome != ClaimAcquired) {
		t.Fatalf("retry after a failure: %+v %v", c2, err)
	}
}
