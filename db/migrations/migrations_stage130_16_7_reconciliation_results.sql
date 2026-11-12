-- Stage 130.16 (74.5.2, BRD §11.1): the daily reconciliation run keeps each
-- check's result - tied or not, the difference and what was found - so a
-- mismatch stays visible as an exception until a later run ties. Additive:
-- a new table.
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
      CREATE TABLE IF NOT EXISTS %I.reconciliation_results (
        id SERIAL PRIMARY KEY,
        check_id VARCHAR(100) NOT NULL,
        label VARCHAR(200),
        tied BOOLEAN NOT NULL,
        difference NUMERIC(18,2) NOT NULL DEFAULT 0,
        detail TEXT,
        ran_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_reconciliation_results_check ON %I.reconciliation_results (check_id, ran_at DESC)', schema_rec.schema_name);
  END LOOP;
END
$mig$;
