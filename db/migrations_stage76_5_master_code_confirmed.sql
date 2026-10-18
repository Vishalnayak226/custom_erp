-- ---------------------------------------------------------------------------
-- Stage 76.5 (user, 2026-10-17): "All code will be simple V00001. Ask me to
-- set up the length for the first time ... in that vendor panel itself if not
-- set up."
--
-- prefix_configs.confirmed_at records that a person (or the first numbered
-- record) settled a master's code format. Until then the master's New form
-- asks for the prefix and length; engines/master_codes.go sets it. A seeded
-- row such as Vendor/"/"/6/ANNUAL is NOT confirmed - that shape is what
-- produced Vendor/HQ/2026/000006, and it is exactly what gets replaced.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs');
    EXECUTE format('ALTER TABLE %I.prefix_configs ADD COLUMN IF NOT EXISTS confirmed_at TIMESTAMPTZ', schema_rec.schema_name);
  END LOOP;
END
$mig$;
