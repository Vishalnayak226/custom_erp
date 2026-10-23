-- Stage 130.9 (72.D d, user decision 2026-10-22): sellable / stocked per
-- store. Two optional fields on the store (Location) record list the items -
-- by item code, category or product type, comma-separated - that this store
-- does not sell, and that it does not stock. Blank, the store sells and stocks
-- everything as before. Additive.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL;
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('Location', 'items_not_sold_here', 'Items not sold here (codes, categories or product types, comma-separated)', 'Data', FALSE, NULL, 31),
        ('Location', 'items_not_stocked_here', 'Items not stocked here (codes, categories or product types, comma-separated)', 'Data', FALSE, NULL, 32)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
