-- ---------------------------------------------------------------------------
-- Stage 115.1 (user, 2026-10-19, on the Number Series form): "year in the end".
--
-- prefix_configs.year_position - where the year prints in a number:
--   ''   before the running number   SM/HO/26-27/00001  (blank = every
--        series as it printed before)
--   END  at the end                  SM/HO/00001/26-27
-- Only the layout changes; the counter is still keyed by the year itself, so
-- moving the year mid-year carries the count on.
--
-- Additive and idempotent; tenant_default and every tenant schema. No row is
-- changed - every existing series keeps printing exactly what it printed.
-- ---------------------------------------------------------------------------
ALTER TABLE tenant_default.prefix_configs ADD COLUMN IF NOT EXISTS year_position VARCHAR(8) NOT NULL DEFAULT '';

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.prefix_configs', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format('ALTER TABLE %I.prefix_configs ADD COLUMN IF NOT EXISTS year_position VARCHAR(8) NOT NULL DEFAULT ''''', schema_rec.schema_name);
  END LOOP;
END $$;
