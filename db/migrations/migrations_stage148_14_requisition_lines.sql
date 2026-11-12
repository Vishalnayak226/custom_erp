-- Stage 148.14: a purchase requisition may carry item lines, each with its own
-- store (and optionally a vendor and an estimated rate). The open requests of
-- one vendor combine into a Draft PO per store, every line traced both ways.
-- Additive: two new requisition fields, one new PO field. The three header
-- fields (description, quantity, amount) stop being "mandatory" in the field
-- list only so a request made of lines can be saved; the server still asks for
-- all three when there are no lines (PURCHA-5011), so a header-only request
-- behaves as before. No client document is touched.
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
        ('PurchaseRequisition', 'items', 'Item lines', 'JSONTable', FALSE,
         '[{"key":"sku","label":"Item","type":"data","required":true},{"key":"qty","label":"Qty","type":"number","required":true},{"key":"store","label":"Store","type":"data","required":true},{"key":"vendor_id","label":"Vendor","type":"data"},{"key":"est_rate","label":"Est. rate","type":"number"}]', 20),
        ('PurchaseRequisition', 'po_ids', 'Purchase orders', 'Data', FALSE, NULL, 21),
        ('PurchaseOrder', 'source_requisition_ids', 'From requisition(s)', 'Data', FALSE, NULL, 96)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET mandatory = FALSE
       WHERE doctype_name = 'PurchaseRequisition' AND fieldname IN ('description', 'quantity', 'total_amount') AND mandatory
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
