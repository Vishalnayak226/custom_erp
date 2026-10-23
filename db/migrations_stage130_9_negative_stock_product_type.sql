-- Stage 130.9 (72.D f, user decision 2026-10-22): negative stock per product
-- type. "Allow negative stock" on a Product Type (No by default) lets the till
-- sell its items past zero, recording the shortfall as a negative-stock event.
-- Additive.
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
      VALUES ('ProductType', 'allow_negative_stock', 'Allow negative stock (sell before it is booked in)', 'Select', FALSE, 'No,Yes', 20)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
