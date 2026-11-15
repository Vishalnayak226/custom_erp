-- Item 146.4.1: the invoice number a marketplace (Unicommerce) issued is kept on
-- the sale order it created, shown as "Marketplace invoice". Additive, safe to run twice.
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
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('SalesOrder', 'marketplace_invoice_no', 'Marketplace invoice', 'Data', FALSE, NULL, 101)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
