-- ---------------------------------------------------------------------------
-- Stage 55.15b - document attachments (first used for expense receipts).
--
-- One small store for files that belong to a document: the bytes live under
-- the server's media store (engines/pim_media.go mediaStoreDir, in an
-- attachments/<schema>/ folder, under a random name), and this table records
-- what each file is and which document it belongs to. A file is readable only
-- by someone who can read its document (engines/attachments.go,
-- internal/server/handlers_attachments.go). Removal is a soft delete, so the
-- row and the file stay for audit.
--
-- The partial unique index refuses the same bytes twice on one document while
-- leaving a removed copy free to be added again.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant_default.document_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    doctype VARCHAR(100) NOT NULL,
    document_id VARCHAR(255) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256 CHAR(64) NOT NULL,
    storage_key VARCHAR(300) NOT NULL,
    uploaded_by VARCHAR(150) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    deleted_by VARCHAR(150)
);
CREATE INDEX IF NOT EXISTS idx_document_attachments_doc ON tenant_default.document_attachments (doctype, document_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_document_attachments_content ON tenant_default.document_attachments (doctype, document_id, sha256) WHERE deleted_at IS NULL;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'documents') THEN
      EXECUTE format($sql$
        CREATE TABLE IF NOT EXISTS %1$I.document_attachments (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            doctype VARCHAR(100) NOT NULL,
            document_id VARCHAR(255) NOT NULL,
            file_name VARCHAR(255) NOT NULL,
            content_type VARCHAR(100) NOT NULL,
            size_bytes BIGINT NOT NULL,
            sha256 CHAR(64) NOT NULL,
            storage_key VARCHAR(300) NOT NULL,
            uploaded_by VARCHAR(150) NOT NULL,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            deleted_at TIMESTAMP,
            deleted_by VARCHAR(150))
      $sql$, schema_rec.schema_name);
      EXECUTE format('CREATE INDEX IF NOT EXISTS idx_document_attachments_doc ON %I.document_attachments (doctype, document_id) WHERE deleted_at IS NULL', schema_rec.schema_name);
      EXECUTE format('CREATE UNIQUE INDEX IF NOT EXISTS uq_document_attachments_content ON %I.document_attachments (doctype, document_id, sha256) WHERE deleted_at IS NULL', schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
