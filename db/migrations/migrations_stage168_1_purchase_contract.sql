-- ---------------------------------------------------------------------------
-- Stage 168.1: blanket / rate contracts with release orders.
--
-- PurchaseContract is a plain Transaction doctype: vendor, kind (Rate contract
-- = rates only, Blanket = rates plus a cap), validity, optional store, the
-- agreed lines. A release order is an ordinary PO whose new `contract` field
-- names it. Additive only: no existing row is touched, and a PO with no
-- contract behaves exactly as before.
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
        ('PurchaseContract', 'Procurement', 'procurement', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('PurchaseContract', 'code',          'Contract no',   'Data',      TRUE,  NULL, 1),
        ('PurchaseContract', 'vendor',        'Vendor',        'Link',      TRUE,  'Vendor', 2),
        ('PurchaseContract', 'contract_kind', 'Kind',          'Select',    TRUE,  'Rate contract,Blanket', 3),
        ('PurchaseContract', 'valid_from',    'Valid from',    'Date',      TRUE,  NULL, 4),
        ('PurchaseContract', 'valid_to',      'Valid to',      'Date',      TRUE,  NULL, 5),
        ('PurchaseContract', 'location',      'Store',         'Link',      FALSE, 'Location', 6),
        ('PurchaseContract', 'cap_value',     'Cap value',     'Number',    FALSE, NULL, 7),
        ('PurchaseContract', 'lines',         'Items',         'JSONTable', TRUE,
         '[{"key":"sku","label":"Item","type":"data","required":true},{"key":"rate","label":"Rate","type":"number","required":true},{"key":"cap_qty","label":"Cap qty","type":"number"}]', 8),
        ('PurchaseContract', 'notes',         'Notes',         'Data',      FALSE, NULL, 9),
        ('PurchaseContract', 'status',        'Status',        'Select',    TRUE,  'Draft,Active,Closed', 10),
        ('PurchaseOrder',    'contract',      'Contract',      'Link',      FALSE, 'PurchaseContract', 105)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'PurchaseContract', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'PurchaseContract', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
