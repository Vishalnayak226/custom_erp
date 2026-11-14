-- ---------------------------------------------------------------------------
-- Ask 170 item 170.30: employee advances. An advance is paid to an employee
-- (Dr 1600 Staff Loans & Advances / Cr 1100 Bank) and, behind the setting
-- expense.net_employee_advance (default off), is netted when a claim is paid.
-- Additive only.
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
        ('StaffAdvance', 'Finance', 'expenses', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('StaffAdvance', 'employee_id',    'Employee',  'Link',   TRUE,  'Employee', 1),
        ('StaffAdvance', 'advance_date',   'Date',      'Date',   TRUE,  NULL, 2),
        ('StaffAdvance', 'amount',         'Amount',    'Number', TRUE,  NULL, 3),
        ('StaffAdvance', 'purpose',        'Purpose',   'Data',   FALSE, NULL, 4),
        ('StaffAdvance', 'settled_amount', 'Settled',   'Number', FALSE, NULL, 5),
        ('StaffAdvance', 'status',         'Status',    'Select', TRUE,  'Draft,Paid,Settled,Cancelled', 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin', 'StaffAdvance', TRUE, TRUE, TRUE, FALSE),
        ('Accountant',  'StaffAdvance', TRUE, TRUE, TRUE, FALSE),
        ('HR/Admin',    'StaffAdvance', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
