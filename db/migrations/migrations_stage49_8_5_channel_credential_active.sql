-- 49.8.5 (screen): a channel / courier credential can be switched off without
-- being deleted (the other credential tables already have `active`). A
-- switched-off credential is refused where it is read; saving new fields
-- switches it back on. Additive and safe to re-run.

ALTER TABLE tenant_default.channel_credentials
  ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT TRUE;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.channel_credentials', schema_rec.schema_name)) IS NOT NULL THEN
      EXECUTE format('ALTER TABLE %I.channel_credentials ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT TRUE', schema_rec.schema_name);
    END IF;
  END LOOP;
END $$;
