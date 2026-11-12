-- ---------------------------------------------------------------------------
-- Stage 86.2 (user, 2026-10-18, on the Number Series form): "Year can be
-- 26-27 or 2627 or other std possible way."
--
-- prefix_configs.year_format - how the financial year prints in a number:
--   ''          26-27   (blank = the format every series had before)
--   FY_COMPACT  2627
--   FY_LONG     2026-27
--   START_YY    26
--   START_YYYY  2026
-- The counter is still keyed by the financial year itself ("26-27"), so a
-- format changed mid-year carries the count on instead of restarting it.
--
-- Additive and idempotent; tenant_default and every tenant schema. No row is
-- changed - every existing series keeps printing exactly what it printed.
-- ---------------------------------------------------------------------------
ALTER TABLE tenant_default.prefix_configs ADD COLUMN IF NOT EXISTS year_format VARCHAR(12) NOT NULL DEFAULT '';

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
    EXECUTE format('ALTER TABLE %I.prefix_configs ADD COLUMN IF NOT EXISTS year_format VARCHAR(12) NOT NULL DEFAULT ''''', schema_rec.schema_name);
  END LOOP;
END $$;
