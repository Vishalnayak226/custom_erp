-- Stage 47.13.5 - merging a duplicate supplier or item, and undoing it.
--
-- master_merges is one row per merge (and per unmerge: status moves to
-- 'Unmerged', the row stays). master_merge_rows is one row per reference the
-- merge moved: which record, which Link field, what it held before. Unmerge
-- reads these rows and puts exactly those values back - and leaves a record
-- alone if it was changed again since. Nothing is deleted: the duplicate keeps
-- its row, marked Merged (merged_into names the survivor).
--
-- Additive: two new tables. Same DO-block catch-up shape as Stage 49.3.6 for
-- schemas provisioned earlier.
CREATE TABLE IF NOT EXISTS tenant_default.master_merges (
    id VARCHAR(100) PRIMARY KEY,
    master_doctype VARCHAR(100) NOT NULL,
    keep_id VARCHAR(100) NOT NULL,
    merge_id VARCHAR(100) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'Merged',
    prev_status VARCHAR(50) NOT NULL DEFAULT '',
    moved_count INTEGER NOT NULL DEFAULT 0,
    merged_by VARCHAR(100) NOT NULL,
    merged_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    unmerged_by VARCHAR(100),
    unmerged_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_master_merges_pair
  ON tenant_default.master_merges (master_doctype, merge_id, status);

CREATE TABLE IF NOT EXISTS tenant_default.master_merge_rows (
    id BIGSERIAL PRIMARY KEY,
    merge_id VARCHAR(100) NOT NULL,
    doc_id VARCHAR(100) NOT NULL,
    doctype VARCHAR(100) NOT NULL,
    fieldname VARCHAR(100) NOT NULL,
    old_value TEXT NOT NULL,
    new_value TEXT NOT NULL,
    restored_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_master_merge_rows_merge
  ON tenant_default.master_merge_rows (merge_id);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.master_merges (LIKE tenant_default.master_merges INCLUDING ALL)', schema_rec.schema_name);
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.master_merge_rows (LIKE tenant_default.master_merge_rows INCLUDING ALL)', schema_rec.schema_name);
  END LOOP;
END $$;
