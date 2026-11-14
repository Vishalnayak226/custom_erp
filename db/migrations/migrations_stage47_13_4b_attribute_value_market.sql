-- 47.13.4 (markets narrow survivorship): a ProductAttributeValue can name the
-- market it is written for, so a stewardship entry's Markets list can narrow its
-- system-of-record rule the way its Channels and Languages do. Blank = all markets.
--
-- Additive and idempotent, replayed into every tenant schema. Metadata only (one
-- field definition per schema): no client document changes, and with the field
-- blank every write behaves as before.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES ('ProductAttributeValue', 'market', 'Market (blank = all markets)', 'Data', FALSE, NULL, 23)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
