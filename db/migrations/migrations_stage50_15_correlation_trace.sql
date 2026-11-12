-- Stage 50.15 / BLD-052: correlation trace across request -> transaction ->
-- outbox/job.
--
-- Every HTTP request already gets a correlation id (middleware.go sets
-- Resolved-Correlation-ID, and writeAPIError returns it to the user so a
-- support report carries it). system_error_logs already stores it. What was
-- missing is the hop from a request to the durable async work it queued:
-- async_jobs and integration_event_outbox rows carried no correlation id at
-- all, so a job that failed at 03:00 could not be traced back to the request
-- that created it, and a user quoting their correlation id could not be
-- connected to the outbox event their action produced.
--
-- Additive and backward-compatible: a nullable-by-default TEXT column with an
-- empty-string default, so every existing row and every writer that does not
-- supply one keeps working unchanged. Indexed because the lookup this exists
-- to serve is "show me everything for correlation id X", and an unindexed
-- scan of a large async_jobs/outbox table is exactly the cost BLD-046 spent
-- its time removing elsewhere. Partial index: the overwhelming majority of
-- historical rows have no correlation id, and indexing those empty strings
-- would be dead weight.
--
-- Same DO-block catch-up shape as Stage 31.1/32.5/47.1/50.14 for schemas
-- provisioned before this migration existed.

ALTER TABLE tenant_default.async_jobs
  ADD COLUMN IF NOT EXISTS correlation_id TEXT NOT NULL DEFAULT '';

ALTER TABLE tenant_default.integration_event_outbox
  ADD COLUMN IF NOT EXISTS correlation_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_async_jobs_correlation
  ON tenant_default.async_jobs (correlation_id)
  WHERE correlation_id <> '';

CREATE INDEX IF NOT EXISTS idx_outbox_correlation
  ON tenant_default.integration_event_outbox (correlation_id)
  WHERE correlation_id <> '';

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.async_jobs ADD COLUMN IF NOT EXISTS correlation_id TEXT NOT NULL DEFAULT ''''', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.integration_event_outbox ADD COLUMN IF NOT EXISTS correlation_id TEXT NOT NULL DEFAULT ''''', schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_async_jobs_correlation ON %I.async_jobs (correlation_id) WHERE correlation_id <> ''''', schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_outbox_correlation ON %I.integration_event_outbox (correlation_id) WHERE correlation_id <> ''''', schema_rec.schema_name);
  END LOOP;
END $$;
