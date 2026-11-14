-- Ask 170.37. Additive and idempotent, replayed into every tenant schema.
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
        ('LutRegister', 'Finance', 'finance', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('LutRegister', 'lut_number',     'LUT / ARN number', 'Data',   TRUE,  NULL, 1),
        ('LutRegister', 'financial_year', 'Financial year',   'Data',   TRUE,  NULL, 2),
        ('LutRegister', 'valid_from',     'Valid from',       'Date',   TRUE,  NULL, 3),
        ('LutRegister', 'valid_to',       'Valid to',         'Date',   TRUE,  NULL, 4),
        ('LutRegister', 'status',         'Status',           'Select', TRUE,  'Active,Withdrawn', 5),
        ('LutRegister', 'notes',          'Notes',            'Data',   FALSE, NULL, 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT 'SalesInvoice', f.fieldname, f.label, f.fieldtype, FALSE, f.options, f.ord
          FROM (VALUES
            ('export_type',        'Export type',          'Select', 'None,Export under LUT,Export with payment of IGST', 120),
            ('shipping_bill_no',   'Shipping bill number', 'Data',   NULL, 121),
            ('shipping_bill_date', 'Shipping bill date',   'Date',   NULL, 122),
            ('port_code',          'Port code',            'Data',   NULL, 123),
            ('destination_country','Destination country',  'Data',   NULL, 124)
          ) AS f(fieldname, label, fieldtype, options, ord)
         WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = 'SalesInvoice')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('HR/Admin', 'LutRegister', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'LutRegister', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
