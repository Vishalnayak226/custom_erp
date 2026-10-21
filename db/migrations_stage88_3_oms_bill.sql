-- ---------------------------------------------------------------------------
-- Stage 88.3.2 / 88.3.3 (D4, D5, user 2026-10-18): the OMS bill.
--
-- Pack makes one OMS bill per package (engines/oms_bill.go), numbered from
-- its own series, OB/<store>/2627/00001 - 16 characters at a two-letter store
-- code, GST's limit (CGST Rule 46) - because its number is the GST invoice
-- number. The ERP bill (SalesInvoice) is made from it at manifest handover and
-- links back through oms_bill_id.
--
-- OMSBill is engine-written: the generic record API only reads it
-- (handlers_core_doc_engine.go), so every role here gets read alone.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('OMSBill', 'OMS', 'oms', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('OMSBill', 'bill_number',         'Bill number',      'Data',   TRUE,  NULL, 1),
        ('OMSBill', 'sales_order_id',      'Order',            'Link',   TRUE,  'SalesOrder', 2),
        ('OMSBill', 'shipping_package_id', 'Package',          'Link',   TRUE,  'ShippingPackage', 3),
        ('OMSBill', 'customer',            'Customer',         'Data',   FALSE, NULL, 4),
        ('OMSBill', 'location',            'Store',            'Data',   FALSE, NULL, 5),
        ('OMSBill', 'taxable_amount',      'Taxable value',    'Number', FALSE, NULL, 6),
        ('OMSBill', 'total_tax',           'GST',              'Number', FALSE, NULL, 7),
        ('OMSBill', 'total_amount',        'Bill total',       'Number', TRUE,  NULL, 8),
        ('OMSBill', 'sales_invoice_id',    'ERP bill',         'Link',   FALSE, 'SalesInvoice', 9),
        ('OMSBill', 'status',              'Status',           'Select', TRUE,  'Issued,Billed', 10),
        ('ShippingPackage', 'oms_bill_id', 'OMS bill',         'Link',   FALSE, 'OMSBill', 14),
        ('SalesInvoice',    'oms_bill_id', 'OMS bill',         'Link',   FALSE, 'OMSBill', 90)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        VALUES ('HR/Admin', 'OMSBill', TRUE, FALSE, FALSE, FALSE),
               ('Store Manager', 'OMSBill', TRUE, FALSE, FALSE, FALSE),
               ('Cashier', 'OMSBill', TRUE, FALSE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format('ALTER TABLE %I.prefix_configs ADD COLUMN IF NOT EXISTS year_format VARCHAR(12) NOT NULL DEFAULT ''''', schema_rec.schema_name);
      EXECUTE format($sql$
        INSERT INTO %1$I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store, year_format)
        SELECT 'OB', 'OB', '/', 5, 'ANNUAL', TRUE, TRUE, 'FY_COMPACT'
        WHERE NOT EXISTS (SELECT 1 FROM %1$I.prefix_configs WHERE doc_type = 'OB' OR UPPER(prefix) = 'OB')
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
