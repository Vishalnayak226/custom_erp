-- Stage 17.1: preserve deleted documents for audit/recovery.
ALTER TABLE tenant_default.documents ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP NULL;
CREATE INDEX IF NOT EXISTS idx_documents_active_doctype ON tenant_default.documents (doctype, deleted_at) WHERE deleted_at IS NULL;
