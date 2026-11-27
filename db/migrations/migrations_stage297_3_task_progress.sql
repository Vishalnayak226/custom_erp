-- 297.3: a progress bar for every long task. An async job a user started
-- shows in the top bar's tasks list with its rows done, rows in all and the
-- time left, so the runner records counts and when work began, not only a
-- percentage. Additive: existing rows read 0 / NULL / '' and show as before.
--   progress_done, progress_total - rows (or items) done and in all
--   started_at - when a worker first picked the job up (time left = rate so far)
--   label - what the user sees ("Import · Item · items.csv"); '' = the type's name
-- The index serves "my tasks": the caller's own jobs, newest first.
ALTER TABLE tenant_default.async_jobs ADD COLUMN IF NOT EXISTS progress_done BIGINT NOT NULL DEFAULT 0;
ALTER TABLE tenant_default.async_jobs ADD COLUMN IF NOT EXISTS progress_total BIGINT NOT NULL DEFAULT 0;
ALTER TABLE tenant_default.async_jobs ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE tenant_default.async_jobs ADD COLUMN IF NOT EXISTS label TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_async_jobs_actor_created ON tenant_default.async_jobs (actor, created_at DESC);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.async_jobs ADD COLUMN IF NOT EXISTS progress_done BIGINT NOT NULL DEFAULT 0', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.async_jobs ADD COLUMN IF NOT EXISTS progress_total BIGINT NOT NULL DEFAULT 0', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.async_jobs ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.async_jobs ADD COLUMN IF NOT EXISTS label TEXT NOT NULL DEFAULT ''''', schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = schema_rec.schema_name AND table_name = 'async_jobs') THEN
      EXECUTE format('CREATE INDEX IF NOT EXISTS idx_async_jobs_actor_created ON %I.async_jobs (actor, created_at DESC)', schema_rec.schema_name);
    END IF;
  END LOOP;
END $$;
