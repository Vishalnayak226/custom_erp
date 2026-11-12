-- ---------------------------------------------------------------------------
-- Stage 60.5 - "no code in UI" (the user, 2026-10-14). Two field labels still
-- told people to expect a code where the screen now shows a name: Vendor
-- Invoice's vendor picker (57.26 f) and Purchase Order's derived vendor field.
-- Only a label still at its shipped text is changed.
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
      UPDATE %I.doctype_fields SET label = 'Vendor'
       WHERE doctype_name = 'VendorInvoice' AND fieldname = 'vendor_id' AND label = 'Vendor Code'
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET label = 'Vendor (auto)'
       WHERE doctype_name = 'PurchaseOrder' AND fieldname = 'vendor_id' AND label = 'Vendor Code (auto)'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
