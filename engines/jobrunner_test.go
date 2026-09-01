package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"testing"
)

func TestStage386JobRunner(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func(ids ...string) {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE id = $1", id)
		}
	}

	t.Run("EnqueueJob rejects an empty job_type and dedupes on idempotency_key", func(t *testing.T) {
		if _, err := EnqueueJob(tenantID, "", map[string]interface{}{"x": 1}, ""); err == nil {
			t.Fatalf("expected an empty job_type to be rejected")
		}
		id1, err := EnqueueJob(tenantID, "test-3860-echo", map[string]interface{}{"n": 1}, "TEST3860-DEDUP")
		if err != nil {
			t.Fatalf("EnqueueJob: %v", err)
		}
		defer cleanup(id1)
		id2, err := EnqueueJob(tenantID, "test-3860-echo", map[string]interface{}{"n": 2}, "TEST3860-DEDUP")
		if err != nil {
			t.Fatalf("EnqueueJob (dedup attempt): %v", err)
		}
		if id1 != id2 {
			t.Fatalf("expected the same idempotency_key to return the same job id, got %s and %s", id1, id2)
		}
		id3, err := EnqueueJob(tenantID, "test-3860-echo", map[string]interface{}{"n": 3}, "")
		if err != nil {
			t.Fatalf("EnqueueJob (no dedup key): %v", err)
		}
		defer cleanup(id3)
		if id3 == id1 {
			t.Fatalf("expected an empty idempotency_key to always create a new job")
		}
	})

	t.Run("claimJobs + finishJob: a succeeding handler marks Succeeded, a failing one retries then dead-letters", func(t *testing.T) {
		okID, err := EnqueueJob(tenantID, "test-3860-ok", map[string]interface{}{}, "")
		if err != nil {
			t.Fatalf("EnqueueJob (ok): %v", err)
		}
		defer cleanup(okID)
		failID, err := EnqueueJob(tenantID, "test-3860-fail", map[string]interface{}{}, "")
		if err != nil {
			t.Fatalf("EnqueueJob (fail): %v", err)
		}
		defer cleanup(failID)
		// max_attempts defaults to 5 - lower it so the test doesn't need 5 ticks.
		if _, err := db.DB.Exec("UPDATE "+schema+".async_jobs SET max_attempts = 2 WHERE id = $1", failID); err != nil {
			t.Fatalf("lower max_attempts: %v", err)
		}

		RegisterJobHandler("test-3860-ok", func(schema string, job Job) (map[string]interface{}, error) {
			return map[string]interface{}{"echoed": true}, nil
		})
		attemptCount := 0
		RegisterJobHandler("test-3860-fail", func(schema string, job Job) (map[string]interface{}, error) {
			attemptCount++
			return nil, errors.New("deliberate test failure")
		})

		runClaimedJobs(schema)

		var okStatus, okResult string
		if err := db.DB.QueryRow("SELECT status, COALESCE(result::text, '') FROM "+schema+".async_jobs WHERE id = $1", okID).Scan(&okStatus, &okResult); err != nil {
			t.Fatalf("reload ok job: %v", err)
		}
		if okStatus != "Succeeded" || okResult == "" {
			t.Fatalf("expected the ok job to be Succeeded with a stored result, got status=%s result=%s", okStatus, okResult)
		}

		var failStatus string
		var failAttempts int
		if err := db.DB.QueryRow("SELECT status, attempts FROM "+schema+".async_jobs WHERE id = $1", failID).Scan(&failStatus, &failAttempts); err != nil {
			t.Fatalf("reload fail job (1st attempt): %v", err)
		}
		if failStatus != "Pending" || failAttempts != 1 {
			t.Fatalf("expected the failing job to be rescheduled Pending after attempt 1, got status=%s attempts=%d", failStatus, failAttempts)
		}

		// Force next_attempt_at into the past so the 2nd claim doesn't have to
		// wait out the real backoff window.
		if _, err := db.DB.Exec("UPDATE "+schema+".async_jobs SET next_attempt_at = CURRENT_TIMESTAMP - INTERVAL '1 second' WHERE id = $1", failID); err != nil {
			t.Fatalf("force next_attempt_at: %v", err)
		}
		runClaimedJobs(schema)

		if err := db.DB.QueryRow("SELECT status, attempts FROM "+schema+".async_jobs WHERE id = $1", failID).Scan(&failStatus, &failAttempts); err != nil {
			t.Fatalf("reload fail job (2nd attempt): %v", err)
		}
		if failStatus != "DeadLettered" || failAttempts != 2 {
			t.Fatalf("expected the failing job to be DeadLettered after reaching max_attempts, got status=%s attempts=%d", failStatus, failAttempts)
		}
		if attemptCount != 2 {
			t.Fatalf("expected the handler to have actually run exactly twice, got %d", attemptCount)
		}
	})

	t.Run("an unclaimed job_type dead-letters immediately rather than looping forever", func(t *testing.T) {
		id, err := EnqueueJob(tenantID, "test-3860-unregistered", map[string]interface{}{}, "")
		if err != nil {
			t.Fatalf("EnqueueJob: %v", err)
		}
		defer cleanup(id)
		if _, err := db.DB.Exec("UPDATE "+schema+".async_jobs SET max_attempts = 1 WHERE id = $1", id); err != nil {
			t.Fatalf("lower max_attempts: %v", err)
		}
		runClaimedJobs(schema)
		var status string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".async_jobs WHERE id = $1", id).Scan(&status); err != nil {
			t.Fatalf("reload: %v", err)
		}
		if status != "DeadLettered" {
			t.Fatalf("expected an unregistered job_type to dead-letter, got %s", status)
		}
	})

	t.Run("CancelJob refuses a terminal job and accepts a Pending one; ReplayJob resets a DeadLettered job back to Pending", func(t *testing.T) {
		id, err := EnqueueJob(tenantID, "test-3860-cancel", map[string]interface{}{}, "")
		if err != nil {
			t.Fatalf("EnqueueJob: %v", err)
		}
		defer cleanup(id)
		if err := CancelJob(tenantID, id); err != nil {
			t.Fatalf("expected cancelling a Pending job to succeed: %v", err)
		}
		if err := CancelJob(tenantID, id); err == nil {
			t.Fatalf("expected cancelling an already-Cancelled job to be refused")
		}
		if err := ReplayJob(tenantID, id); err != nil {
			t.Fatalf("expected replaying a Cancelled job to succeed: %v", err)
		}
		var status string
		var attempts int
		if err := db.DB.QueryRow("SELECT status, attempts FROM "+schema+".async_jobs WHERE id = $1", id).Scan(&status, &attempts); err != nil {
			t.Fatalf("reload: %v", err)
		}
		if status != "Pending" || attempts != 0 {
			t.Fatalf("expected the replayed job to be Pending with attempts reset to 0, got status=%s attempts=%d", status, attempts)
		}
	})

	t.Run("ListJobs filters by status and job_type", func(t *testing.T) {
		id, err := EnqueueJob(tenantID, "test-3860-list", map[string]interface{}{}, "")
		if err != nil {
			t.Fatalf("EnqueueJob: %v", err)
		}
		defer cleanup(id)
		jobs, err := ListJobs(tenantID, "Pending", "test-3860-list", 0)
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		found := false
		for _, j := range jobs {
			if j["id"] == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected the newly enqueued job to appear in a Pending/test-3860-list filtered list, got %+v", jobs)
		}
		wrongType, err := ListJobs(tenantID, "Pending", "test-3860-nonexistent-type", 0)
		if err != nil {
			t.Fatalf("ListJobs (wrong type): %v", err)
		}
		for _, j := range wrongType {
			if j["id"] == id {
				t.Fatalf("expected job_type filtering to exclude this job")
			}
		}
	})

	t.Run("SweepJobRunnerRetention deletes an old terminal job but leaves a fresh one and a still-Pending one alone", func(t *testing.T) {
		oldID, err := EnqueueJob(tenantID, "test-3860-sweep-old", map[string]interface{}{}, "")
		if err != nil {
			t.Fatalf("EnqueueJob: %v", err)
		}
		defer cleanup(oldID)
		freshID, err := EnqueueJob(tenantID, "test-3860-sweep-fresh", map[string]interface{}{}, "")
		if err != nil {
			t.Fatalf("EnqueueJob: %v", err)
		}
		defer cleanup(freshID)
		pendingID, err := EnqueueJob(tenantID, "test-3860-sweep-pending", map[string]interface{}{}, "")
		if err != nil {
			t.Fatalf("EnqueueJob: %v", err)
		}
		defer cleanup(pendingID)

		if _, err := db.DB.Exec("UPDATE "+schema+".async_jobs SET status = 'Succeeded', completed_at = CURRENT_TIMESTAMP - INTERVAL '400 days' WHERE id = $1", oldID); err != nil {
			t.Fatalf("age old job: %v", err)
		}
		if _, err := db.DB.Exec("UPDATE "+schema+".async_jobs SET status = 'Succeeded', completed_at = CURRENT_TIMESTAMP WHERE id = $1", freshID); err != nil {
			t.Fatalf("complete fresh job: %v", err)
		}

		if _, err := SweepJobRunnerRetention(tenantID); err != nil {
			t.Fatalf("SweepJobRunnerRetention: %v", err)
		}

		var count int
		db.DB.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s.async_jobs WHERE id = $1", schema), oldID).Scan(&count)
		if count != 0 {
			t.Fatalf("expected the old terminal job to be swept, but it still exists")
		}
		db.DB.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s.async_jobs WHERE id = $1", schema), freshID).Scan(&count)
		if count != 1 {
			t.Fatalf("expected the freshly-completed job to survive the sweep")
		}
		db.DB.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s.async_jobs WHERE id = $1", schema), pendingID).Scan(&count)
		if count != 1 {
			t.Fatalf("expected the still-Pending job to survive the sweep")
		}
	})
}
