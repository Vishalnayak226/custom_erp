-- 49.2.6: one purpose per API credential (what the integration does), so a
-- second active credential for the same purpose can be refused when the
-- client turns that on. Additive; a blank purpose is today's behaviour.
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS purpose VARCHAR(80);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS purpose VARCHAR(80)', schema_rec.schema_name);
  END LOOP;
END $$;
