-- Stage 130.16 (74.3.3, BRD 4.5, DEC-09/10): job work on the existing
-- SubcontractOrder. An order now has many lines (lines: item, qty, lot,
-- serials, gross / net / fine weight, purity, value, the item that comes back
-- and its yield), a due date, whether the goods are capital goods (ITC-04's
-- three-year limit instead of one), and its delivery challan. The old
-- single-item fields stay (an older order is a one-line order) but are no
-- longer required, and two statuses join: Challan Pending, Partly Received.
-- Additive: new fields, a loosened requirement, appended statuses.
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
        ('SubcontractOrder', 'lines', 'Lines', 'JSONTable', FALSE,
         '[{"key":"sku","label":"Item sent","type":"data","required":true},{"key":"qty","label":"Qty","type":"number","required":true},{"key":"batch_no","label":"Lot","type":"data"},{"key":"gross_weight","label":"Gross wt","type":"number"},{"key":"net_weight","label":"Net wt","type":"number"},{"key":"fine_weight","label":"Fine wt","type":"number"},{"key":"purity","label":"Purity","type":"data"},{"key":"output_sku","label":"Comes back as","type":"data"},{"key":"output_per_unit","label":"Output per unit","type":"number"}]', 20),
        ('SubcontractOrder', 'due_date', 'Due back', 'Date', FALSE, NULL, 21),
        ('SubcontractOrder', 'capital_goods', 'Capital goods', 'Select', FALSE, 'No,Yes', 22),
        ('SubcontractOrder', 'challan_number', 'Delivery challan', 'Data', FALSE, NULL, 23)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET mandatory = FALSE
       WHERE doctype_name = 'SubcontractOrder' AND fieldname IN ('sent_item_id', 'sent_qty', 'received_item_id') AND mandatory
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET options = options || ',Challan Pending,Partly Received'
       WHERE doctype_name = 'SubcontractOrder' AND fieldname = 'status' AND options NOT LIKE '%%Partly Received%%'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
