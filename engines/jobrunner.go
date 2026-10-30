package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"sync"
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
	// Actor (Stage 49.8.6) is who queued the job: the signed-in user's id,
	// or system:<worker> for a background enqueue. Never empty.
	Actor string
}

// JobHandlerFunc executes one job for one tenant schema. Returning an error
// marks the job Failed (and retried with backoff, or DeadLettered once
// MaxAttempts is reached); returning nil marks it Succeeded with result
// stored alongside.
type JobHandlerFunc func(ctx context.Context, schema string, job Job) (result map[string]interface{}, err error)

var jobHandlers = map[string]JobHandlerFunc{}

type runningJobKey struct{ schema, id string }

type runningJob struct {
	cancel context.CancelFunc
}

var runningJobs = struct {
	sync.Mutex
	byKey map[runningJobKey]runningJob
}{byKey: make(map[runningJobKey]runningJob)}

const jobCancellationPollInterval = time.Second

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
	// Stage 49.8.6: result and runtime bounds. A result over jobMaxResultBytes
	// is stored as a marker (truncated, original size, the first bytes), so
	// one chatty handler cannot grow async_jobs without limit. A job's
	// context is cancelled at its type's maximum runtime (the default below
	// unless SetJobMaxRuntime names another) and the attempt is marked
	// failed as "timed out", retried like any other failure. A handler that
	// ignores its context keeps running past the deadline - the attempt is
	// still recorded as timed out when it returns, and its lease (claimed
	// for the runtime plus a grace) is what stops a second worker taking the
	// job meanwhile.
	jobMaxResultBytes     = 64 * 1024
	jobResultHeadBytes    = 512
	jobDefaultMaxRuntime  = jobLeaseDuration
	jobRuntimeLeaseGrace  = time.Minute
	jobActorSystemDefault = "system:background"
)

// jobMaxRuntimes holds the per-type overrides of jobDefaultMaxRuntime,
// registered beside the handler (SetJobMaxRuntime) from init().
var jobMaxRuntimes = map[string]time.Duration{}

// SetJobMaxRuntime bounds how long one attempt of jobType may run. Called
// from init() beside RegisterJobHandler; a non-positive duration is ignored.
func SetJobMaxRuntime(jobType string, d time.Duration) {
	if d > 0 {
		jobMaxRuntimes[jobType] = d
	}
}

// JobMaxRuntime is the runtime bound one attempt of jobType gets.
func JobMaxRuntime(jobType string) time.Duration {
	if d, ok := jobMaxRuntimes[jobType]; ok {
		return d
	}
	return jobDefaultMaxRuntime
}

// jobActorKey carries the signed-in user through a request's context so the
// durable work it queues is stamped with who asked for it - the same shape
// as WithCorrelationID (BLD-052).
type jobActorKey struct{}

// WithJobActor returns ctx carrying actor as the one every job queued under
// it is stamped with: a user id from apiMiddleware, or system:<worker> from
// a background worker that queues on its own schedule.
func WithJobActor(ctx context.Context, actor string) context.Context {
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, jobActorKey{}, actor)
}

// JobActorFromContext is the actor WithJobActor stored, or "".
func JobActorFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	actor, _ := ctx.Value(jobActorKey{}).(string)
	return actor
}

// jobActorFor resolves the actor a job is stamped with: the context's actor
// (a user, or a named worker); else, inside a running job handler, that job
// (system:job:<type>, from the posting actor the worker binds); else the
// generic background name. Never empty, so every job row says who queued it.
func jobActorFor(ctx context.Context) string {
	if actor := JobActorFromContext(ctx); actor != "" {
		return truncateAttribution(actor, 150)
	}
	if pa := currentPostingActor(); pa.PostedBy == "service:job" && pa.SourceCommand != "" {
		return "system:" + pa.SourceCommand
	}
	return jobActorSystemDefault
}

// SystemJobContext names the background worker queuing a job, for
// EnqueueJobContext: the job is stamped system:<worker>.
func SystemJobContext(worker string) context.Context {
	return WithJobActor(context.Background(), "system:"+strings.ToLower(strings.TrimSpace(worker)))
}

var jobWorkerIdentity = fmt.Sprintf("pid-%d", os.Getpid())

// EnqueueJob queues a job for a tenant, returning its id. If idempotencyKey
// is non-empty and a job with the same (job_type, idempotency_key) already
// exists, that job's own id is returned instead of creating a duplicate -
// the same "the unique index is the lock" guarantee Stage 38.5's public API
// idempotency already established, applied here to job enqueueing.
func EnqueueJob(tenantID, jobType string, payload map[string]interface{}, idempotencyKey string) (string, error) {
	return EnqueueJobContext(context.Background(), tenantID, jobType, payload, idempotencyKey)
}

// EnqueueJobContext is the BLD-052 correlation-aware form: the job row is
// stamped with the correlation id carried by ctx, so an async failure can be
// traced back to the request that queued it. A ctx with no correlation (a
// background sweep, a test) stores an empty string, which is the column's
// own default.
func EnqueueJobContext(ctx context.Context, tenantID, jobType string, payload map[string]interface{}, idempotencyKey string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	return enqueueJobInSchema(ctx, schema, jobType, payload, idempotencyKey, SafeCorrelationID(CorrelationIDFromContext(ctx)))
}

func enqueueJobInSchema(ctx context.Context, schema, jobType string, payload map[string]interface{}, idempotencyKey, correlationID string) (string, error) {
	return enqueueJobWith(db.DB, schema, jobType, payload, idempotencyKey, correlationID, jobActorFor(ctx))
}

// jobSecretKeys are payload keys a job never stores (Stage 49.8.6): a job row
// is listed, retried and dead-lettered, so a secret in it outlives its use. A
// handler that needs one reads it when it runs, as webhook delivery does
// (134.2). Exact names, not fragments - a document's session_id or a token
// count inside an event payload must pass through untouched.
var jobSecretKeys = map[string]bool{
	"password": true, "password_hash": true, "passcode": true, "secret": true,
	"client_secret": true, "api_secret": true, "webhook_secret": true, "signing_secret": true,
	"api_key": true, "apikey": true, "private_key": true, "access_token": true,
	"refresh_token": true, "auth_token": true, "bearer_token": true, "mfa_secret": true, "totp_secret": true,
}

// redactJobSecrets replaces the value of every jobSecretKeys key, at any depth.
func redactJobSecrets(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, item := range val {
			if jobSecretKeys[strings.ToLower(k)] {
				out[k] = redactedMarker
			} else {
				out[k] = redactJobSecrets(item)
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = redactJobSecrets(item)
		}
		return out
	}
	return v
}

// jobEnqueuer is satisfied by both *sql.DB and *sql.Tx, so a caller that
// must commit a job together with its own row (Stage 73.2.13's notification
// log) enqueues inside its transaction - the outbox guarantee - rather than
// after it.
type jobEnqueuer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

func enqueueJobWith(q jobEnqueuer, schema, jobType string, payload map[string]interface{}, idempotencyKey, correlationID, actor string) (string, error) {
	if jobType == "" {
		return "", fmt.Errorf("a job needs a job_type")
	}
	// Stage 49.8.6: the handler registry is the allowlist, checked here at
	// enqueue and not only by the worker - a type nothing can run is refused
	// to its caller instead of sitting in the queue until it dead-letters.
	if _, ok := jobHandlers[jobType]; !ok {
		return "", fmt.Errorf("job type %q has no registered handler", jobType)
	}
	if actor == "" {
		actor = jobActorSystemDefault
	}
	if payload != nil {
		payload, _ = redactJobSecrets(payload).(map[string]interface{})
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if len(payloadBytes) > jobMaxPayloadBytes {
		return "", fmt.Errorf("job payload of %d bytes exceeds the %d byte limit", len(payloadBytes), jobMaxPayloadBytes)
	}
	jobID := NewDocID("JOB")
	_, err = q.Exec(fmt.Sprintf(`
		INSERT INTO %s.async_jobs (id, job_type, payload, idempotency_key, correlation_id, actor)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (job_type, idempotency_key) WHERE idempotency_key <> '' DO NOTHING`, schema),
		jobID, jobType, payloadBytes, idempotencyKey, correlationID, actor)
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
	if err := q.QueryRow(fmt.Sprintf(
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
					if ctx.Err() != nil {
						return
					}
					runClaimedJobsContext(ctx, schema)
				}
			}
		}
	}()
}

func runClaimedJobs(schema string) {
	runClaimedJobsContext(context.Background(), schema)
}

func runClaimedJobsContext(ctx context.Context, schema string) {
	if ctx.Err() != nil {
		return
	}
	// Stage 49.3.2: the client a job acts for is the one whose schema holds
	// it. Resolved before anything is claimed, so a schema that names no
	// client leaves its jobs queued instead of running them for nobody.
	tenantID, err := tenantIDForSchema(schema)
	if err != nil {
		LogBackgroundJobError("JOBRUNNER", "tenant", schema, err)
		return
	}
	jobs, err := claimJobs(schema, jobClaimBatchSize)
	if err != nil {
		LogBackgroundJobError("JOBRUNNER", "claim", schema, err)
		return
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			// Leave unstarted leases untouched. They are reclaimed after the
			// normal lease timeout, so graceful shutdown does not lose accepted work.
			return
		}
		handler, ok := jobHandlers[job.JobType]
		if !ok {
			finishJob(schema, job, nil, fmt.Errorf("no handler registered for job_type %q", job.JobType))
			continue
		}
		if err := bindJobTenant(schema, tenantID, &job); err != nil {
			finishJob(schema, job, nil, err)
			continue
		}
		// 49.8.7a: the job's client; 49.8.6: bounded to its type's runtime.
		jobCtx, cancel := context.WithTimeout(withOutsideCallsSchema(ctx, schema), JobMaxRuntime(job.JobType))
		key := runningJobKey{schema: schema, id: job.ID}
		runningJobs.Lock()
		runningJobs.byKey[key] = runningJob{cancel: cancel}
		runningJobs.Unlock()
		watchDone := make(chan struct{})
		go watchJobCancellation(jobCtx, schema, job.ID, cancel, watchDone)
		// Stage 73.1.9: GL rows a job posts name the job, not a user.
		var result map[string]interface{}
		func() {
			defer BindPostingActor(PostingActor{PostedBy: "service:job", SourceCommand: "job:" + job.JobType})()
			// Stage 49.8.8: a poison job - one whose payload makes its handler
			// panic - is a failed attempt like any other: retried with backoff,
			// then dead-lettered. Unrecovered, the panic ended the worker
			// goroutine and with it the whole server process, and the job,
			// still Leased, was claimed again after the restart.
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[JOBRUNNER] job %s (%s) panicked: %v\n%s", job.ID, job.JobType, r, debug.Stack())
					result, err = nil, fmt.Errorf("job handler panicked: %v", r)
				}
			}()
			result, err = handler(jobCtx, schema, job)
			if jobCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
				// Stage 49.8.6: over its runtime bound is a failed attempt
				// whatever the handler returned.
				result, err = nil, fmt.Errorf("job timed out after its %s runtime bound", JobMaxRuntime(job.JobType))
			}
		}()
		cancel()
		<-watchDone
		runningJobs.Lock()
		delete(runningJobs.byKey, key)
		runningJobs.Unlock()
		finishJob(schema, job, result, err)
	}
}

// bindJobTenant (Stage 49.3.2) makes the payload's tenant_id - which several
// handlers act on - the client the job is queued for. A payload naming
// another client is refused, never run there. A payload naming none gets this
// one: before, its handler read "" and db.GetTenantSchema sent it to the
// default client.
func bindJobTenant(schema, tenantID string, job *Job) error {
	named, _ := job.Payload["tenant_id"].(string)
	if named == "" {
		if job.Payload == nil {
			job.Payload = map[string]interface{}{}
		}
		job.Payload["tenant_id"] = tenantID
		return nil
	}
	resolved, err := db.GetTenantSchema(named)
	if err != nil {
		return err
	}
	if resolved != schema {
		return fmt.Errorf("job names client %q but is queued for %s - not run", named, schema)
	}
	return nil
}

// watchJobCancellation observes the durable Cancelled state so an operator
// request routed to a different app process still interrupts the lease owner.
// The local runningJobs map makes same-process cancellation immediate; this
// bounded one-second poll is the cross-process fallback without a broker.
func watchJobCancellation(ctx context.Context, schema, jobID string, cancel context.CancelFunc, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(jobCancellationPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var status string
			if err := db.DB.QueryRowContext(ctx, fmt.Sprintf(`SELECT status FROM %s.async_jobs WHERE id = $1`, schema), jobID).Scan(&status); err == nil && status == "Cancelled" {
				cancel()
				return
			}
		}
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
		SELECT id, job_type, payload, attempts, max_attempts, actor
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
		if err := rows.Scan(&j.ID, &j.JobType, &payloadStr, &j.Attempts, &j.MaxAttempts, &j.Actor); err != nil {
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
		for _, j := range claimed {
			// Stage 49.8.6: the lease outlives the type's runtime bound, so a
			// job still inside its bound is never claimed twice.
			lease := jobLeaseDuration
			if bound := JobMaxRuntime(j.JobType) + jobRuntimeLeaseGrace; bound > lease {
				lease = bound
			}
			if _, err := tx.Exec(fmt.Sprintf(`
				UPDATE %s.async_jobs SET status = 'Leased', leased_by = $1, leased_until = $2, updated_at = CURRENT_TIMESTAMP
				WHERE id = $3`, schema), jobWorkerIdentity, time.Now().Add(lease), j.ID); err != nil {
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
		if len(resultBytes) > jobMaxResultBytes {
			// Stage 49.8.6: a marker with the first bytes, never the whole.
			head, _ := json.Marshal(string(resultBytes[:jobResultHeadBytes]))
			resultBytes = []byte(fmt.Sprintf(`{"truncated":true,"original_size":%d,"limit":%d,"head":%s}`, len(resultBytes), jobMaxResultBytes, head))
		}
		_, err := db.DB.Exec(fmt.Sprintf(`
			UPDATE %s.async_jobs SET status = 'Succeeded', result = $1, last_error = '', updated_at = CURRENT_TIMESTAMP, completed_at = CURRENT_TIMESTAMP
			WHERE id = $2 AND status = 'Leased'`, schema), resultBytes, job.ID)
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
			WHERE id = $3 AND status = 'Leased'`, schema), nextAttempts, errMsg, job.ID)
		if err != nil {
			log.Printf("[JOBRUNNER] failed to dead-letter job %s: %v", job.ID, err)
		}
		log.Printf("[JOBRUNNER] job %s (%s) dead-lettered after %d attempts: %s", job.ID, job.JobType, nextAttempts, errMsg)
		// Stage 49.11.1: a job that gave up is a security / operations event,
		// not only a log line - a failed delivery or sync is someone's to act on.
		if tenantID, idErr := tenantIDForSchema(schema); idErr == nil {
			LogAuditEvent(tenantID, "system", "JOB", "DEAD_LETTERED", fmt.Sprintf("Job %s (%s) gave up after %d attempts: %.300s", job.ID, job.JobType, nextAttempts, errMsg))
		}
		return
	}

	// Exponential backoff: 30s, 60s, 120s, ... - the same "cheap, bounded,
	// no external scheduling library" shape as every other retry loop in
	// this codebase.
	backoff := jobBaseBackoff * time.Duration(1<<uint(nextAttempts-1))
	_, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.async_jobs SET status = 'Pending', attempts = $1, last_error = $2, next_attempt_at = CURRENT_TIMESTAMP + $3, updated_at = CURRENT_TIMESTAMP
		WHERE id = $4 AND status = 'Leased'`, schema), nextAttempts, errMsg, fmt.Sprintf("%d seconds", int(backoff.Seconds())), job.ID)
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
	key := runningJobKey{schema: schema, id: jobID}
	runningJobs.Lock()
	running := runningJobs.byKey[key]
	runningJobs.Unlock()
	if running.cancel != nil {
		running.cancel()
	}
	return nil
}

// ReplayJob resets a Failed, DeadLettered, Cancelled or Quarantined job back
// to Pending with a clean attempt count - the RetryIntegrationEvent
// precedent (engines/outbox.go), applied to the general runner. A
// Quarantined job (Stage 47.0.5/47.11.6 Gate 0 - see
// quarantineStaleExternalDispatchJobsInSchema below) reaching here means an
// administrator deliberately chose to resume it, exactly the "deliberate
// replay" the gate requires.
func ReplayJob(tenantID, jobID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.async_jobs SET status = 'Pending', attempts = 0, next_attempt_at = CURRENT_TIMESTAMP, last_error = '', updated_at = CURRENT_TIMESTAMP, completed_at = NULL
		WHERE id = $1 AND status IN ('Failed', 'DeadLettered', 'Cancelled', 'Quarantined')`, schema), jobID)
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
		SELECT id, job_type, status, attempts, max_attempts, progress_pct, last_error, created_at, updated_at, actor
		FROM %s.async_jobs WHERE ($1 = '' OR status = $1) AND ($2 = '' OR job_type = $2)
		ORDER BY created_at DESC LIMIT $3`, schema)
	rows, err := db.DB.Query(query, statusFilter, jobTypeFilter, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, jobType, status, lastError, actor string
		var attempts, maxAttempts, progressPct int
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &jobType, &status, &attempts, &maxAttempts, &progressPct, &lastError, &createdAt, &updatedAt, &actor); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"id": id, "job_type": jobType, "status": status, "attempts": attempts, "max_attempts": maxAttempts,
			"progress_pct": progressPct, "last_error": lastError, "created_at": createdAt, "updated_at": updatedAt,
			"actor": actor,
		})
	}
	return out, rows.Err()
}

// quarantineStaleExternalDispatchJobsInSchema moves any Pending
// webhook_delivery job in schema to 'Quarantined' - called once at startup
// by QuarantineStaleExternalDispatchJobs (engines/environment.go) when
// external side effects are off, so a leftover queued delivery from before
// this boot cannot be picked up by StartJobRunnerWorker on this run without
// an operator deliberately choosing to resume it via ReplayJob (the same
// Retry action the Async Jobs tab already exposes). 'Quarantined' is outside
// the runner's claim query (status = 'Pending'/'Leased', see
// StartJobRunnerWorker) so a quarantined row is inert until replayed.
func quarantineStaleExternalDispatchJobsInSchema(schema string) (int64, error) {
	result, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.async_jobs SET status = 'Quarantined', updated_at = CURRENT_TIMESTAMP
		WHERE job_type IN ($1, $2) AND status = 'Pending'`, schema), webhookDeliveryJobType, notificationDeliveryJobType)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
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
	// 49.18.2: batched, and nothing while the tenant is under legal hold.
	return retentionDelete(tenantID, schema, "async_jobs",
		`status IN ('Succeeded', 'DeadLettered', 'Cancelled')
		  AND completed_at < CURRENT_TIMESTAMP - ($1 || ' days')::interval`, retentionDays)
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
				if !IsSchedulerLeader() { // 134.7: one server runs each sweep
					continue
				}
				if db.DB == nil {
					continue
				}
				schemas, err := listRegisteredTenantSchemas()
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
						LogBackgroundJobError("JOBRUNNER-SWEEP", "sweep", schema, err)
					}
				}
			}
		}
	}()
}
