-- 49.6.8: a correction request carries the corrected values, applied to the
-- subject's record when the request is approved and executed. Additive; no rows
-- are written.
ALTER TABLE tenant_default.data_subject_requests ADD COLUMN IF NOT EXISTS corrections JSONB;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE %I.data_subject_requests ADD COLUMN IF NOT EXISTS corrections JSONB', schema_rec.schema_name);
  END LOOP;
END $$;
