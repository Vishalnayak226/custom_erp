-- Stage 38.6: one general async job runner with retries, DLQ and a
-- visibility screen.
--
-- A dedicated table, not a generic doctype - same choice
-- integration_event_outbox already made, for the same reason: this needs
-- SELECT ... FOR UPDATE SKIP LOCKED claiming and a status/lease taxonomy the
-- generic document engine's maker-checker-shaped assumptions don't fit.
-- Deliberately a NEW, separate mechanism rather than rewiring the existing
-- outbox/scheduled-report/wave/etc. tickers onto it - per the plan already
-- recorded at micro_checklist.md's Stage 47.11.4, those migrate onto this
-- runner incrementally, later, each as its own deliberate change.
CREATE TABLE IF NOT EXISTS tenant_default.async_jobs (
    id TEXT PRIMARY KEY,
    job_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    idempotency_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'Pending', -- Pending, Leased, Succeeded, Failed, DeadLettered, Cancelled
    attempts INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    leased_by TEXT NOT NULL DEFAULT '',
    leased_until TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    progress_pct SMALLINT NOT NULL DEFAULT 0,
    result JSONB,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ
);

-- The claim query filters on (status, next_attempt_at); this is the one
-- index that query needs.
CREATE INDEX IF NOT EXISTS idx_async_jobs_claim ON tenant_default.async_jobs (status, next_attempt_at);

-- Idempotent enqueue: the same (job_type, idempotency_key) returns the
-- existing job instead of creating a duplicate. A blank idempotency_key
-- means "no dedup, always create new" - the partial index excludes those
-- rows so any number of undeduplicated jobs can share job_type='' key.
CREATE UNIQUE INDEX IF NOT EXISTS idx_async_jobs_idempotency ON tenant_default.async_jobs (job_type, idempotency_key) WHERE idempotency_key <> '';

DO $$
DECLARE
    schema_rec RECORD;
BEGIN
    FOR schema_rec IN
        SELECT schema_name FROM information_schema.schemata
        WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
    LOOP
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I.async_jobs (LIKE tenant_default.async_jobs INCLUDING ALL)', schema_rec.schema_name);
    END LOOP;
END $$;
