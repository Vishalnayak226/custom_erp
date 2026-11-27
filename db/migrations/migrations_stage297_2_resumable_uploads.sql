-- 297.2: a bulk upload that never fails. A large file goes up in pieces that
-- survive a dropped line, a closed tab or a server restart, and its import
-- steps run in the background in slices that carry on from where they were.
-- Additive and idempotent; no existing row changes.
--
-- resumable_uploads: one row per file being sent. The bytes go to a file on
-- the server's disk; this row is how far it has got and the running SHA-256
-- (hash_state), so a piece sent twice is never written twice and the whole
-- file's hash is known the moment the last byte lands.
--   status: Receiving | Complete | Used | Expired
-- The staged-rows index serves the duplicate check: "has an earlier row of
-- this file the same key?" for one chunk of keys at a time.
CREATE TABLE IF NOT EXISTS tenant_default.resumable_uploads (
  id              VARCHAR(64)  PRIMARY KEY,
  owner           VARCHAR(100) NOT NULL,
  purpose         VARCHAR(40)  NOT NULL DEFAULT 'import',
  file_name       VARCHAR(255) NOT NULL DEFAULT '',
  size_bytes      BIGINT       NOT NULL,
  received_bytes  BIGINT       NOT NULL DEFAULT 0,
  status          VARCHAR(20)  NOT NULL DEFAULT 'Receiving',
  sha256          VARCHAR(64)  NOT NULL DEFAULT '',
  hash_state      BYTEA,
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_resumable_uploads_owner ON tenant_default.resumable_uploads (owner, created_at DESC);
CREATE INDEX IF NOT EXISTS import_staging_rows_key_idx ON tenant_default.import_staging_rows (batch_id, lower(natural_key), row_no);
-- Serves the commit's walk over valid rows in file order and the per-status
-- samples and lists, without reading the rest of a large batch.
CREATE INDEX IF NOT EXISTS import_staging_rows_status_row_idx ON tenant_default.import_staging_rows (batch_id, status, row_no);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.resumable_uploads (LIKE tenant_default.resumable_uploads INCLUDING ALL)', schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = schema_rec.schema_name AND table_name = 'import_staging_rows') THEN
      EXECUTE format('CREATE INDEX IF NOT EXISTS import_staging_rows_key_idx ON %I.import_staging_rows (batch_id, lower(natural_key), row_no)', schema_rec.schema_name);
      EXECUTE format('CREATE INDEX IF NOT EXISTS import_staging_rows_status_row_idx ON %I.import_staging_rows (batch_id, status, row_no)', schema_rec.schema_name);
    END IF;
  END LOOP;
END $$;
