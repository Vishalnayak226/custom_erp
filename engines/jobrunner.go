package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

// Stage 38.6: one general async job runner with retries, DLQ and a
// visibility screen - the foundation micro_checklist.md's Stage 47.11.4
// already names as the target every bespoke ticker in this codebase
// (outbox, scheduled reports, wave auto-creation, amortization, ...) should
// migrate onto incrementally, later, each as its own deliberate change. This
// stage builds the runner itself plus its visibility screen; it does not
// touch any existing ticker.
//
// Deliberately its own table (async_jobs), not a generic doctype - the same
// choice integration_event_outbox already made: this needs
// SELECT ... FOR UPDATE SKIP LOCKED claiming and a status/lease taxonomy the
// generic document engine's maker-checker-shaped assumptions don't fit.
//
// The "lease" here is a claim deadline, not a live heartbeat renewed
// mid-handler: every handler in this codebase (and every one registered so
// far) runs synchronously to completion within one worker tick, so there is
// no genuinely long-running handler to renew a lease for yet. leaseDuration
// only has to outlive the slowest realistic handler; a worker that crashes
// mid-handler leaves its claim to expire and be reclaimed by the next tick,
// which is the actual crash-recovery guarantee this exists for. A future
// handler that is genuinely long-running (Stage 47.7.7's verifier/
// checkpoint/archive jobs) can call UpdateJobProgress periodically and a
// later change can add real heartbeat renewal then - not invented ahead of
// a real caller that needs it.

// Job is one async_jobs row, as handed to a registered handler.
type Job struct {
	ID          string
	JobType     string
	Payload     map[string]interface{}
	Attempts    int
	MaxAttempts int
}

// JobHandlerFunc executes one job for one tenant schema. Returning an error
// marks the job Failed (and retried with backoff, or DeadLettered once
// MaxAttempts is reached); returning nil marks it Succeeded with result
// stored alongside.
type JobHandlerFunc func(schema string, job Job) (result map[string]interface{}, err error)

var jobHandlers = map[string]JobHandlerFunc{}

// RegisterJobHandler adds a handler to the registry - the RegisterReport
// precedent (engines/report_registry.go). Called only from this package's
// own init() functions; a duplicate job_type is a build-time programmer
// error, so it panics rather than silently overwriting.
func RegisterJobHandler(jobType string, handler JobHandlerFunc) {
	if _, exists := jobHandlers[jobType]; exists {
		panic(fmt.Sprintf("job handler %q already registered", jobType))
	}
	jobHandlers[jobType] = handler
}

const (
	jobClaimBatchSize   = 10
	jobLeaseDuration    = 5 * time.Minute
	jobBaseBackoff      = 30 * time.Second
	jobMaxPayloadBytes  = 64 * 1024
	jobDefaultRetention = 30
)

var jobWorkerIdentity = fmt.Sprintf("pid-%d", os.Getpid())

// EnqueueJob queues a job for a tenant, returning its id. If idempotencyKey
// is non-empty and a job with the same (job_type, idempotency_key) already
// exists, that job's own id is returned instead of creating a duplicate -
// the same "the unique index is the lock" guarantee Stage 38.5's public API
// idempotency already established, applied here to job enqueueing.
func EnqueueJob(tenantID, jobType string, payload map[string]interface{}, idempotencyKey string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	return enqueueJobInSchema(schema, jobType, payload, idempotencyKey)
}

func enqueueJobInSchema(schema, jobType string, payload map[string]interface{}, idempotencyKey string) (string, error) {
	if jobType == "" {
		return "", fmt.Errorf("a job needs a job_type")
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if len(payloadBytes) > jobMaxPayloadBytes {
		return "", fmt.Errorf("job payload of %d bytes exceeds the %d byte limit", len(payloadBytes), jobMaxPayloadBytes)
	}
	jobID := NewDocID("JOB")
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.async_jobs (id, job_type, payload, idempotency_key)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (job_type, idempotency_key) WHERE idempotency_key <> '' DO NOTHING`, schema),
		jobID, jobType, payloadBytes, idempotencyKey)
	if err != nil {
		return "", err
	}
	if idempotencyKey == "" {
		return jobID, nil
	}
	// Either this INSERT won the race and jobID is correct, or an existing
	// row already held the key and ON CONFLICT DO NOTHING silently kept it -
	// re-read so the caller always gets back the job that actually owns this
	// idempotency key, not the id it merely tried to insert.
	var existingID string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT id FROM %s.async_jobs WHERE job_type = $1 AND idempotency_key = $2`, schema),
		jobType, idempotencyKey).Scan(&existingID); err != nil {
		return "", err
	}
	return existingID, nil
}

// StartJobRunnerWorker polls every tenant schema for claimable jobs -
// Pending jobs whose next_attempt_at has arrived, plus Leased jobs whose
// lease has expired (a crashed worker's claim, reclaimed by the next tick).
func StartJobRunnerWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[JOBRUNNER] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					runClaimedJobs(schema)
				}
			}
		}
	}()
}

func runClaimedJobs(schema string) {
	jobs, err := claimJobs(schema, jobClaimBatchSize)
	if err != nil {
		log.Printf("[JOBRUNNER] claim failed for %s: %v", schema, err)
		return
	}
	for _, job := range jobs {
		handler, ok := jobHandlers[job.JobType]
		if !ok {
			finishJob(schema, job, nil, fmt.Errorf("no handler registered for job_type %q", job.JobType))
			continue
		}
		result, err := handler(schema, job)
		finishJob(schema, job, result, err)
	}
}

func claimJobs(schema string, limit int) ([]Job, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.Query(fmt.Sprintf(`
		SELECT id, job_type, payload, attempts, max_attempts
		FROM %s.async_jobs
		WHERE (status = 'Pending' AND next_attempt_at <= CURRENT_TIMESTAMP)
		   OR (status = 'Leased' AND leased_until < CURRENT_TIMESTAMP)
		ORDER BY next_attempt_at
		LIMIT %d
		FOR UPDATE SKIP LOCKED`, schema, limit))
	if err != nil {
		return nil, err
	}
	var claimed []Job
	var ids []string
	for rows.Next() {
		var j Job
		var payloadStr string
		if err := rows.Scan(&j.ID, &j.JobType, &payloadStr, &j.Attempts, &j.MaxAttempts); err != nil {
			rows.Close()
			return nil, err
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
			log.Printf("[JOBRUNNER] corrupt payload for job %s: %v", j.ID, err)
			payload = map[string]interface{}{}
		}
		j.Payload = payload
		claimed = append(claimed, j)
		ids = append(ids, j.ID)
	}
	rows.Close()

	if len(ids) > 0 {
		leasedUntil := time.Now().Add(jobLeaseDuration)
		for _, id := range ids {
			if _, err := tx.Exec(fmt.Sprintf(`
				UPDATE %s.async_jobs SET status = 'Leased', leased_by = $1, leased_until = $2, updated_at = CURRENT_TIMESTAMP
				WHERE id = $3`, schema), jobWorkerIdentity, leasedUntil, id); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return claimed, nil
}

func finishJob(schema string, job Job, result map[string]interface{}, jobErr error) {
	if jobErr == nil {
		resultBytes, merr := json.Marshal(result)
		if merr != nil {
			resultBytes = []byte("{}")
		}
		if len(resultBytes) > jobMaxPayloadBytes {
			resultBytes = []byte(fmt.Sprintf(`{"truncated":true,"original_size":%d}`, len(resultBytes)))
		}
		_, err := db.DB.Exec(fmt.Sprintf(`
			UPDATE %s.async_jobs SET status = 'Succeeded', result = $1, last_error = '', updated_at = CURRENT_TIMESTAMP, completed_at = CURRENT_TIMESTAMP
			WHERE id = $2`, schema), resultBytes, job.ID)
		if err != nil {
			log.Printf("[JOBRUNNER] failed to mark job %s succeeded: %v", job.ID, err)
		}
		return
	}

	nextAttempts := job.Attempts + 1
	errMsg := jobErr.Error()
	if len(errMsg) > 2000 {
		errMsg = errMsg[:2000]
	}
	if nextAttempts >= job.MaxAttempts {
		_, err := db.DB.Exec(fmt.Sprintf(`
			UPDATE %s.async_jobs SET status = 'DeadLettered', attempts = $1, last_error = $2, updated_at = CURRENT_TIMESTAMP, completed_at = CURRENT_TIMESTAMP
			WHERE id = $3`, schema), nextAttempts, errMsg, job.ID)
		if err != nil {
			log.Printf("[JOBRUNNER] failed to dead-letter job %s: %v", job.ID, err)
		}
		log.Printf("[JOBRUNNER] job %s (%s) dead-lettered after %d attempts: %s", job.ID, job.JobType, nextAttempts, errMsg)
		return
	}

	// Exponential backoff: 30s, 60s, 120s, ... - the same "cheap, bounded,
	// no external scheduling library" shape as every other retry loop in
	// this codebase.
	backoff := jobBaseBackoff * time.Duration(1<<uint(nextAttempts-1))
	_, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.async_jobs SET status = 'Pending', attempts = $1, last_error = $2, next_attempt_at = CURRENT_TIMESTAMP + $3, updated_at = CURRENT_TIMESTAMP
		WHERE id = $4`, schema), nextAttempts, errMsg, fmt.Sprintf("%d seconds", int(backoff.Seconds())), job.ID)
	if err != nil {
		log.Printf("[JOBRUNNER] failed to reschedule job %s: %v", job.ID, err)
	}
}

// UpdateJobProgress lets a long-running handler report percent-complete
// mid-execution. Not used by either handler registered so far (both run to
// completion in one synchronous step); exposed for a future genuinely
// long-running handler (Stage 47.7.7) to call from inside its own loop.
func UpdateJobProgress(schema, jobID string, pct int) error {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	_, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET progress_pct = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema), pct, jobID)
	return err
}

// CancelJob cancels a Pending job, or a Leased one whose worker has likely
// crashed (its lease has not yet been reclaimed by the next tick) - refused
// once a job has reached any terminal state.
func CancelJob(tenantID, jobID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.async_jobs SET status = 'Cancelled', updated_at = CURRENT_TIMESTAMP, completed_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status IN ('Pending', 'Leased')`, schema), jobID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("job %s not found, or already in a terminal state", jobID)
	}
	return nil
}

// ReplayJob resets a Failed, DeadLettered or Cancelled job back to Pending
// with a clean attempt count - the RetryIntegrationEvent precedent
// (engines/outbox.go), applied to the general runner.
func ReplayJob(tenantID, jobID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.async_jobs SET status = 'Pending', attempts = 0, next_attempt_at = CURRENT_TIMESTAMP, last_error = '', updated_at = CURRENT_TIMESTAMP, completed_at = NULL
		WHERE id = $1 AND status IN ('Failed', 'DeadLettered', 'Cancelled')`, schema), jobID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("job %s not found, or not in a replayable state", jobID)
	}
	return nil
}

// ListJobs returns the most recent jobs for the visibility screen, newest
// first, optionally filtered by status and/or job_type.
func ListJobs(tenantID, statusFilter, jobTypeFilter string, limit int) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := fmt.Sprintf(`
		SELECT id, job_type, status, attempts, max_attempts, progress_pct, last_error, created_at, updated_at
		FROM %s.async_jobs WHERE ($1 = '' OR status = $1) AND ($2 = '' OR job_type = $2)
		ORDER BY created_at DESC LIMIT $3`, schema)
	rows, err := db.DB.Query(query, statusFilter, jobTypeFilter, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, jobType, status, lastError string
		var attempts, maxAttempts, progressPct int
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &jobType, &status, &attempts, &maxAttempts, &progressPct, &lastError, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"id": id, "job_type": jobType, "status": status, "attempts": attempts, "max_attempts": maxAttempts,
			"progress_pct": progressPct, "last_error": lastError, "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	return out, rows.Err()
}

// SweepJobRunnerRetention deletes terminal jobs (Succeeded, DeadLettered,
// Cancelled) older than the retention window - the SweepPublicAPIRuntime
// precedent (engines/public_api_runtime.go). async_jobs is append-heavy and
// not a system of record, so unbounded growth is the only real risk it
// carries.
func SweepJobRunnerRetention(tenantID string) (int64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	retentionDays := GetSettingInt(tenantID, "platform.job_runner_retention_days")
	if retentionDays <= 0 {
		retentionDays = jobDefaultRetention
	}
	result, err := db.DB.Exec(fmt.Sprintf(`
		DELETE FROM %s.async_jobs
		WHERE status IN ('Succeeded', 'DeadLettered', 'Cancelled')
		  AND completed_at < CURRENT_TIMESTAMP - ($1 || ' days')::interval`, schema), retentionDays)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

// StartJobRunnerRetentionSweeper keeps async_jobs bounded - hourly, the same
// cadence StartPublicAPIRuntimeSweeper uses.
func StartJobRunnerRetentionSweeper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[JOBRUNNER-SWEEP] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					tenantID, idErr := tenantIDForSchema(schema)
					if idErr != nil {
						continue
					}
					if _, err := SweepJobRunnerRetention(tenantID); err != nil {
						log.Printf("[JOBRUNNER-SWEEP] sweep failed for %s: %v", schema, err)
					}
				}
			}
		}
	}()
}
