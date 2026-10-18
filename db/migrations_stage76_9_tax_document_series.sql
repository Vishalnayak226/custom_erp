-- ---------------------------------------------------------------------------
-- Stage 76.9: tax documents take their number from their store's series
-- (engines/document_numbering.go - SalesInvoice SI, CreditNote CN, DebitNote
-- DN, VendorCreditNote VCN).
--
-- * CN - customer credit notes had no series (they carried a 32-character
--   internal id). Added as CN/<store>/2627/0001: 16 characters at a
--   two-letter store code, GST's limit (CGST Rule 46).
-- * SI - the sales invoice series was shipped in db/migration.sql but never
--   used. As shipped it prints SI/<store>/26-27/000001, 18 characters. Where it
--   is still exactly as shipped and has issued nothing, it becomes
--   SI/<store>/2627/00001 (16). A client's own setting, or a series that has
--   issued numbers, is never changed.
--
-- year_format is added here too, exactly as migrations_stage86_2 adds it,
-- because this file sorts before that one on a database built from zero.
--
-- Additive and idempotent, replayed into every tenant schema.
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
      SELECT 'CN', 'CN', '/', 4, 'ANNUAL', TRUE, TRUE, 'FY_COMPACT'
      WHERE NOT EXISTS (SELECT 1 FROM %1$I.prefix_configs WHERE doc_type = 'CN' OR UPPER(prefix) = 'CN')
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'sequence_counters');

    EXECUTE format($sql$
      UPDATE %1$I.prefix_configs
      SET padding_width = 5, year_format = 'FY_COMPACT'
      WHERE doc_type = 'SI' AND prefix = 'SI' AND separator = '/' AND padding_width = 6
        AND year_format = '' AND COALESCE(suffix, '') = '' AND COALESCE(store_overrides, '{}'::jsonb) = '{}'::jsonb
        AND NOT EXISTS (SELECT 1 FROM %1$I.sequence_counters WHERE doc_type = 'SI')
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
