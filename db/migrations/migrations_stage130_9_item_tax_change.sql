-- Stage 130.9 (72.D b, user decision 2026-10-22: only tax fields are
-- effective-dated). An item can carry one scheduled tax change: the date it
-- applies from, the new GST rate and, if it changes too, the new HSN code.
-- The server taxes at the new values from that date and folds them into the
-- item's own fields (engines/item_tax_change.go). Additive.
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
        ('Item', 'tax_change_from', 'New tax from (date)', 'Date', FALSE, NULL, 90),
        ('Item', 'tax_change_gst_rate', 'New GST rate (%%) from that date', 'Number', FALSE, NULL, 91),
        ('Item', 'tax_change_hsn', 'New HSN code from that date', 'Data', FALSE, NULL, 92)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
