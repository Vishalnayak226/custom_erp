-- Ask 170.36. Additive and idempotent, replayed into every tenant schema.
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
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('Gstr2bLine', 'Finance', 'finance', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('Gstr2bLine', 'month',          'Month',            'Data',   TRUE,  NULL, 1),
        ('Gstr2bLine', 'status',         'Status',           'Select', FALSE, 'Matched,Mismatch,In 2B not in books,In books not in 2B', 2),
        ('Gstr2bLine', 'supplier_gstin', 'Supplier GSTIN',   'Data',   FALSE, NULL, 3),
        ('Gstr2bLine', 'supplier_name',  'Supplier',         'Data',   FALSE, NULL, 4),
        ('Gstr2bLine', 'doc_type',       'Type',             'Select', FALSE, 'Invoice,Credit Note', 5),
        ('Gstr2bLine', 'doc_number',     'Invoice',          'Data',   FALSE, NULL, 6),
        ('Gstr2bLine', 'doc_date',       'Date',             'Date',   FALSE, NULL, 7),
        ('Gstr2bLine', 'total',          'Value',            'Currency', FALSE, NULL, 8),
        ('Gstr2bLine', 'tax',            'Tax',              'Currency', FALSE, NULL, 9),
        ('Gstr2bLine', 'bill_id',        'Vendor bill',      'Link',   FALSE, 'VendorInvoice', 10),
        ('Gstr2bLine', 'reason',         'Why',              'Data',   FALSE, NULL, 11),
        ('Gstr2bLine', 'decision',       'Decision',         'Select', FALSE, 'Accepted,Rejected,Query raised', 12),
        ('Gstr2bLine', 'decision_note',  'Note',             'Data',   FALSE, NULL, 13)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('HR/Admin', 'Gstr2bLine', TRUE, FALSE, TRUE, FALSE),
        ('Store Manager', 'Gstr2bLine', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
