-- ---------------------------------------------------------------------------
-- Stage 170.1: a GL line names the document line it came from.
-- gl_postings gets a nullable source_line_id. Nothing writes it unless the
-- setting "finance.gl_source_line" is on (default off), so no existing row or
-- posting changes. The row guard makes it as fixed as every other column.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_postings');
    EXECUTE format('ALTER TABLE %I.gl_postings ADD COLUMN IF NOT EXISTS source_line_id VARCHAR(100)', schema_rec.schema_name);
  END LOOP;
END
$mig$;
