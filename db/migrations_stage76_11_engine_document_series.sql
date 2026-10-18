-- ---------------------------------------------------------------------------
-- Stage 76.11: documents the engines create under an internal NewDocID take a
-- printed number from a series (engines/document_numbering.go, KeepID - the id
-- stays, the number goes in code):
--
--   SO  Sales Order          SO/<store>/2627/00001   16 at a 2-letter store code
--   SR  Sales Return         SR/<store>/2627/00001
--   RF  Refund               RF/<store>/2627/00001
--   PI  Physical Inventory   PI/<store>/2627/00001
--   GP  Gate Pass            GP/<store>/2627/00001
--   CE  Consumption Entry    CE/<store>/2627/00001
--   KIT Kit / De-kit         KIT/<store>/2627/0001
--   LCV Landed Cost Voucher  LCV/<store>/2627/0001
--   JV  Journal Voucher      JV/2627/000001          company-level, no store
--
-- A series the client already has (same doc_type, or a series already using
-- the prefix) is never touched. Additive and idempotent, replayed into every
-- tenant schema. year_format is added exactly as migrations_stage86_2 adds it,
-- because this file sorts before that one on a database built from zero.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
  s RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs');

    EXECUTE format('ALTER TABLE %I.prefix_configs ADD COLUMN IF NOT EXISTS year_format VARCHAR(12) NOT NULL DEFAULT ''''', schema_rec.schema_name);

    FOR s IN
      SELECT * FROM (VALUES
        ('SO', 5, TRUE), ('SR', 5, TRUE), ('RF', 5, TRUE), ('PI', 5, TRUE), ('GP', 5, TRUE),
        ('CE', 5, TRUE), ('KIT', 4, TRUE), ('LCV', 4, TRUE), ('JV', 6, FALSE)
      ) AS v(key, pad, per_store)
    LOOP
      EXECUTE format($sql$
        INSERT INTO %1$I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store, year_format)
        SELECT $1, $1, '/', $2, 'ANNUAL', TRUE, $3, 'FY_COMPACT'
        WHERE NOT EXISTS (SELECT 1 FROM %1$I.prefix_configs WHERE doc_type = $1 OR UPPER(prefix) = $1)
      $sql$, schema_rec.schema_name) USING s.key, s.pad, s.per_store;
    END LOOP;

    -- Consumption Entry had no number field on its record screen; the other
    -- eight already show code.
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, display_order)
      SELECT 'ConsumptionEntry', 'code', 'Entry Number', 'Data', FALSE, 0
      WHERE EXISTS (SELECT 1 FROM %1$I.doctype_meta WHERE name = 'ConsumptionEntry')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
