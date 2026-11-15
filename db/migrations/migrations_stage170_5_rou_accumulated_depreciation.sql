-- ---------------------------------------------------------------------------
-- Stage 170.5 (ask 176): right-of-use depreciation builds up in its own
-- account. Adds 1421 Accumulated Depreciation - Right-of-Use (Asset, contra),
-- ON CONFLICT DO NOTHING so a client's own row of the same code is kept. A
-- lease's monthly depreciation credits 1421; when the last period posts, 1421
-- is cleared against 1420. Additive and safe to run twice. The release counts
-- the new gl_accounts row as a data change.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
          ('1421', 'Accumulated Depreciation - Right-of-Use', 'Asset')
        ON CONFLICT DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END $mig$;
