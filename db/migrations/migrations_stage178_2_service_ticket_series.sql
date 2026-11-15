-- ---------------------------------------------------------------------------
-- 178.2 (57.26 b): Service Ticket numbers come from their own series instead
-- of being typed on the form:
--
--   TKT  Service ticket                TKT/<store>/2627/00001
--
-- A series the client already has (same doc_type, or one already using the
-- prefix) is never touched. Additive and idempotent, replayed into every
-- tenant schema; the 76.12 pattern.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs');

    EXECUTE format('ALTER TABLE %I.prefix_configs ADD COLUMN IF NOT EXISTS year_format VARCHAR(12) NOT NULL DEFAULT ''''', schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %1$I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store, year_format)
      SELECT 'TKT', 'TKT', '/', 5, 'ANNUAL', TRUE, TRUE, 'FY_COMPACT'
      WHERE NOT EXISTS (SELECT 1 FROM %1$I.prefix_configs WHERE doc_type = 'TKT' OR UPPER(prefix) = 'TKT')
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
