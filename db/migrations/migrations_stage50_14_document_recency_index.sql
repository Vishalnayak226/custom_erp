-- Stage 50.14 / BLD-043: the generic doc-list endpoint's opt-in `sort=recent`
-- (added by Stage 50.13/BLD-033 for Home's "Recent" panel, and available to
-- every doctype through GET /api/v1/doc/{doctype}?sort=recent) does
-- `ORDER BY updated_at DESC` with no supporting index. Measured against a
-- seeded 100,000-row SalesOrder table this cost ~57ms (spilling to a
-- temp-file sort under concurrent/parallel load) versus <1ms for the
-- existing `ORDER BY id` default, which the pre-existing
-- idx_documents_active_doctype (doctype, deleted_at) index already serves
-- via a plain index scan. This mirrors that same index, adding updated_at
-- as a second key so a recency-ordered read of any doctype's active rows
-- can be satisfied by an index scan instead of an in-memory/disk sort.
-- Same DO-block catch-up shape as Stage 31.1/32.5/47.1 for schemas that
-- were provisioned before this migration existed.
CREATE INDEX IF NOT EXISTS idx_documents_active_doctype_updated
  ON tenant_default.documents (doctype, updated_at DESC)
  WHERE deleted_at IS NULL;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_documents_active_doctype_updated ON %I.documents (doctype, updated_at DESC) WHERE deleted_at IS NULL', schema_rec.schema_name);
  END LOOP;
END $$;
