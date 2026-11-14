-- ---------------------------------------------------------------------------
-- Stage 168.2: service purchase orders and the service entry sheet.
--
-- A PO gains po_kind (Goods / Service; absent means Goods). A service PO is
-- never received on a GRN: a ServiceEntrySheet confirms the work done and,
-- once accepted, stands in for the GRN in the vendor bill's three-way match.
-- VendorInvoice gains an optional service_entry_id. Additive only; every
-- existing PO stays a goods PO.
-- ---------------------------------------------------------------------------

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default'
        OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('ServiceEntrySheet', 'Procurement', 'procurement', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('ServiceEntrySheet', 'code',        'Sheet no',  'Data',      TRUE,  NULL, 1),
        ('ServiceEntrySheet', 'po_id',       'Service PO', 'Link',     TRUE,  'PurchaseOrder', 2),
        ('ServiceEntrySheet', 'entry_date',  'Date',      'Date',      TRUE,  NULL, 3),
        ('ServiceEntrySheet', 'location',    'Store',     'Link',      FALSE, 'Location', 4),
        ('ServiceEntrySheet', 'lines',       'Work done', 'JSONTable', TRUE,
         '[{"key":"sku","label":"Item","type":"data","required":true},{"key":"qty","label":"Qty done","type":"number","required":true},{"key":"remark","label":"Remark","type":"data"}]', 5),
        ('ServiceEntrySheet', 'notes',       'Notes',     'Data',      FALSE, NULL, 6),
        ('ServiceEntrySheet', 'status',      'Status',    'Select',    TRUE,  'Draft,Accepted', 7),
        ('PurchaseOrder',     'po_kind',     'PO type',   'Select',    FALSE, 'Goods,Service', 106),
        ('VendorInvoice',     'service_entry_id', 'Service entry', 'Link', FALSE, 'ServiceEntrySheet', 100)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'ServiceEntrySheet', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'ServiceEntrySheet', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
