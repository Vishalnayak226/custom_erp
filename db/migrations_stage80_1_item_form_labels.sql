-- ---------------------------------------------------------------------------
-- Stage 80.1 / 80.2 (user, 2026-10-18, on the New Item form): "Make it easy
-- and co-related ... Variant key key need fix."
--
-- The Item form now edits variant options as option + value rows (the stored
-- "Key:Value;Key:Value" string is unchanged), so the label no longer has to
-- spell the format out. "(optional)" goes too: a box without the red asterisk
-- is already optional, and the suffix only lengthened the labels.
--
-- Labels only, and only where a tenant still has the shipped wording - a
-- label a tenant renamed is left alone. Idempotent, replayed into every
-- tenant schema.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      UPDATE %I.doctype_fields AS f
         SET label = v.new_label
        FROM (VALUES
          ('variant_option_values', 'Variant Options (Key:Value;Key:Value)', 'Variant Options'),
          ('hazmat_class', 'Hazmat Class (optional)', 'Hazmat Class'),
          ('temperature_class', 'Temperature Class (optional)', 'Temperature Class'),
          ('is_catch_weight', 'Catch Weight Item (optional)', 'Catch Weight Item'),
          ('gtin', 'GTIN (optional)', 'GTIN'),
          ('country_of_origin', 'Country of Origin (optional)', 'Country of Origin')
        ) AS v(fieldname, old_label, new_label)
       WHERE f.doctype_name = 'Item' AND f.fieldname = v.fieldname AND f.label = v.old_label
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
