-- Ask 170.35. Additive and idempotent, replayed into every tenant schema.
-- What a filed GSTR-1 reported, one row per document, so a later change to a
-- filed document is reported in the amendment tables (engines/gstr1_amendments.go).
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %1$I.gstr1_filed_rows (
        gstin       TEXT NOT NULL,
        doc_key     TEXT NOT NULL,
        doctype     TEXT NOT NULL,
        doc_id      TEXT NOT NULL,
        party       TEXT NOT NULL DEFAULT '',
        month_start DATE NOT NULL,
        section     TEXT NOT NULL,
        row         JSONB NOT NULL,
        filed_month DATE NOT NULL,
        filed_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (gstin, doc_key)
      )
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS gstr1_filed_rows_month_idx ON %1$I.gstr1_filed_rows (gstin, month_start)
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
