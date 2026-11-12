-- ---------------------------------------------------------------------------
-- Stage 73.1.9 - every GL row names who posted it.
--
-- gl_postings carried no actor: a reviewer could see an account, a source
-- document and an amount, but not which user (or which job / connector)
-- produced the row. Two nullable columns, filled at the one posting choke
-- point (engines.PostDoubleEntryTx) from the request or job the posting runs
-- inside (engines/posting_actor.go):
--
--   posted_by       the signed-in username, or a service identity
--                   ("service:job", "service:api-key:<prefix>", ...)
--   source_command  the route ("POST /api/v1/...") or job ("job:<type>")
--
-- Rows written before this migration stay NULL and read as "before
-- attribution" in the GL reports - nothing is backfilled, because a guess
-- would be worse than an honest blank.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
ALTER TABLE tenant_default.gl_postings ADD COLUMN IF NOT EXISTS posted_by VARCHAR(150);
ALTER TABLE tenant_default.gl_postings ADD COLUMN IF NOT EXISTS source_command VARCHAR(200);
CREATE INDEX IF NOT EXISTS idx_gl_postings_posted_by
    ON tenant_default.gl_postings (posted_by, created_at) WHERE posted_by IS NOT NULL;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_postings') THEN
      EXECUTE format('ALTER TABLE %I.gl_postings ADD COLUMN IF NOT EXISTS posted_by VARCHAR(150)', schema_rec.schema_name);
      EXECUTE format('ALTER TABLE %I.gl_postings ADD COLUMN IF NOT EXISTS source_command VARCHAR(200)', schema_rec.schema_name);
      EXECUTE format('CREATE INDEX IF NOT EXISTS idx_gl_postings_posted_by ON %I.gl_postings (posted_by, created_at) WHERE posted_by IS NOT NULL', schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
