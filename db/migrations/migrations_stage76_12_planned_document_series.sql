-- ---------------------------------------------------------------------------
-- Stage 76.12: series rows for the transactions not built yet, so each shows
-- on Settings → Customize → Prefix Configs now and its numbers are set up
-- before the document exists (Stage 74 builds them and numbers from these):
--
--   MI  Material Issue                 MI/<store>/2627/00001   16 at a 2-letter store
--   MR  Material Receipt               MR/<store>/2627/00001
--   DC  Delivery Challan (Rule 55)     DC/<store>/2627/00001
--   JO  Job work out                   JO/<store>/2627/00001
--   JR  Job work return                JR/<store>/2627/00001
--   CV  Store credit voucher           CV/<store>/2627/00001
--   DMG Damage                         DMG/<store>/2627/0001
--   ADJ Inventory / stock adjustment   ADJ/<store>/2627/0001   (not IA: Item
--       Attribute codes already print IA, Stage 86.3)
--
-- Customer Advance (CA) and its refund voucher (AVR) already exist (Stage
-- 85) and are left exactly as they are. A series the client already has
-- (same doc_type, or one already using the prefix) is never touched.
-- Additive and idempotent, replayed into every tenant schema.
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
        ('MI', 5), ('MR', 5), ('DC', 5), ('JO', 5), ('JR', 5), ('CV', 5), ('DMG', 4), ('ADJ', 4)
      ) AS v(key, pad)
    LOOP
      EXECUTE format($sql$
        INSERT INTO %1$I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store, year_format)
        SELECT $1, $1, '/', $2, 'ANNUAL', TRUE, TRUE, 'FY_COMPACT'
        WHERE NOT EXISTS (SELECT 1 FROM %1$I.prefix_configs WHERE doc_type = $1 OR UPPER(prefix) = $1)
      $sql$, schema_rec.schema_name) USING s.key, s.pad;
    END LOOP;
  END LOOP;
END
$mig$;
