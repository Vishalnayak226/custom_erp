-- Stage 148.11: an item can carry its under / over delivery tolerance (%).
-- A PO line starts from it and may change it; receiving and transfers read it
-- live. Blank = no tolerance, exactly as before. Additive: field definitions only.
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
        ('Item', 'under_delivery_pct', 'Under delivery (%%)', 'Number', FALSE, NULL, 93),
        ('Item', 'over_delivery_pct', 'Over delivery (%%)', 'Number', FALSE, NULL, 94)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
