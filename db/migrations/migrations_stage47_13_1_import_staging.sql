-- ---------------------------------------------------------------------------
-- Stage 47.13.1: a staged import - upload, parse, normalize, validate, match,
-- preview, approve, commit, reconcile, expire - inside PostgreSQL, with no
-- separate ETL service (engines/import_staging.go).
--
-- import_staging_rows: one row per data row of an uploaded file, keyed by its
-- batch and its row number in the file. It keeps the cells as uploaded (raw),
-- the row after the versioned transform (normalized), its status, its errors
-- and how it matched the records already there. A row is marked Committed in
-- the same transaction that writes its record, which is what makes a commit
-- safe to repeat or resume.
--
-- ImportBatch: the batch itself is a record in documents (the existing
-- maker-checker approval engine works on documents), so the file's name,
-- size, SHA-256, the rules version, the counts, the preview and the
-- reconciliation live on that record. This adds the record type; no approval
-- rule is added (a client with none approves a batch directly, as it does a
-- customer merge). Staged rows expire through platform.import_file_retention_days
-- (default 0 = keep).
--
-- Additive and idempotent. The new table goes into tenant_default
-- (SyncAllTenantSchemas copies it to every client after migrate); the record
-- type and its read permission go into every schema. No existing data changes.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant_default.import_staging_rows (
  batch_id          VARCHAR(100) NOT NULL,
  row_no            INT          NOT NULL,
  raw               JSONB        NOT NULL DEFAULT '{}'::jsonb,
  normalized        JSONB        NOT NULL DEFAULT '{}'::jsonb,
  transform_version VARCHAR(30)  NOT NULL DEFAULT '',
  status            VARCHAR(20)  NOT NULL DEFAULT 'Staged',
  errors            JSONB,
  match_kind        VARCHAR(20),
  match_id          VARCHAR(200),
  natural_key       VARCHAR(300),
  committed_at      TIMESTAMPTZ,
  PRIMARY KEY (batch_id, row_no)
);
CREATE INDEX IF NOT EXISTS import_staging_rows_status_idx ON tenant_default.import_staging_rows (batch_id, status);

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_meta (name, module, document_type, module_key) VALUES
        ('ImportBatch', 'Core', 'Transaction', 'core')
      ON CONFLICT (name) DO NOTHING$sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('ImportBatch', 'code',         'Code',     'Data',   FALSE, NULL, 1),
        ('ImportBatch', 'doctype_name', 'Imports',  'Data',   FALSE, NULL, 2),
        ('ImportBatch', 'file_name',    'File',     'Data',   FALSE, NULL, 3),
        ('ImportBatch', 'status',       'Status',   'Select', TRUE,  'Draft,Pending Approval,Approved,Rejected,Committed,Reconciled,Expired', 4)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$sql$, schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('HR/Admin', 'ImportBatch', TRUE, FALSE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING$sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
