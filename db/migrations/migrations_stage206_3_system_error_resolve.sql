-- ---------------------------------------------------------------------------
-- Stage 206.3 (System Errors as a fix list): Resolve on the System Errors
-- screen closes every open occurrence of one fault. A later occurrence is a
-- new row with no resolved_at, so the fault comes back if it happens again.
--
--   system_error_logs.resolved_at  when the fault was resolved (NULL = open)
--   system_error_logs.resolved_by  who resolved it
--
-- Additive; loops every tenant schema that has the table; re-running changes
-- nothing.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                    WHERE table_schema = schema_rec.schema_name AND table_name = 'system_error_logs') THEN
      CONTINUE;
    END IF;
    EXECUTE format($sql$
      ALTER TABLE %I.system_error_logs
        ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMP,
        ADD COLUMN IF NOT EXISTS resolved_by VARCHAR(100)
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
