-- ---------------------------------------------------------------------------
-- Ask 170 item 170.29: expense limits by category (and employee grade).
-- ExpenseCategory is a master: a limit per claim, optionally per grade.
-- Employee gains a grade. Additive; with no category limit set, claims
-- behave exactly as before.
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
        ('ExpenseCategory', 'Finance', 'expenses', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('ExpenseCategory', 'code',            'Code',            'Data',      TRUE,  NULL, 1),
        ('ExpenseCategory', 'name',            'Name',            'Data',      TRUE,  NULL, 2),
        ('ExpenseCategory', 'limit_per_claim', 'Limit per claim', 'Number',    FALSE, NULL, 3),
        ('ExpenseCategory', 'grade_limits',    'Limit by grade',  'JSONTable', FALSE,
         '[{"key":"grade","label":"Grade","type":"data","required":true},{"key":"limit","label":"Limit","type":"number","required":true}]', 4),
        ('ExpenseCategory', 'status',          'Status',          'Select',    TRUE,  'Active,Inactive', 5),
        ('Employee',        'grade',           'Grade',           'Data',      FALSE, NULL, 90)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'ExpenseCategory', TRUE, TRUE, TRUE, TRUE),
        ('Accountant',    'ExpenseCategory', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'ExpenseCategory', TRUE, FALSE, FALSE, FALSE),
        ('HR/Admin',      'ExpenseCategory', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
