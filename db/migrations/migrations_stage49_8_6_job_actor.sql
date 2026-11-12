-- Stage 49.8.6: every async job names who queued it - the signed-in user's
-- id, or system:<worker> for a background enqueue (engines/jobrunner.go,
-- enqueueJobWith). Additive: an existing row reads '' until something
-- stamps it, and the job runner treats '' as "before actors were stamped".
ALTER TABLE tenant_default.async_jobs ADD COLUMN IF NOT EXISTS actor TEXT NOT NULL DEFAULT '';

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.async_jobs ADD COLUMN IF NOT EXISTS actor TEXT NOT NULL DEFAULT ''''', schema_rec.schema_name);
  END LOOP;
END $$;
