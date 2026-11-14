-- ---------------------------------------------------------------------------
-- Ask 170 item 170.31: timesheets cost the project. A Timesheet entry
-- (employee, project, task, date, hours) is approved by the maker-checker
-- engine; on approval hours x the employee's cost rate post to the project
-- (Dr 5450 Project Labour Cost / Cr 5460 Project Labour Absorbed).
-- Employee gains a cost rate. Additive only.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
  ('5450', 'Project Labour Cost',     'Expense'),
  ('5460', 'Project Labour Absorbed', 'Expense')
ON CONFLICT (account_code) DO NOTHING;

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
       WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts');
    EXECUTE format($sql$
      INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
        ('5450', 'Project Labour Cost', 'Expense'),
        ('5460', 'Project Labour Absorbed', 'Expense')
      ON CONFLICT (account_code) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('Timesheet', 'Finance', 'finance', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('Timesheet', 'employee_id', 'Employee',    'Link',   TRUE,  'Employee', 1),
        ('Timesheet', 'project',     'Project',     'Link',   TRUE,  'Project', 2),
        ('Timesheet', 'task',        'Task',        'Data',   FALSE, NULL, 3),
        ('Timesheet', 'work_date',   'Date',        'Date',   TRUE,  NULL, 4),
        ('Timesheet', 'hours',       'Hours',       'Number', TRUE,  NULL, 5),
        ('Timesheet', 'cost_rate',   'Cost rate',   'Number', FALSE, NULL, 6),
        ('Timesheet', 'cost_amount', 'Cost',        'Number', FALSE, NULL, 7),
        ('Timesheet', 'status',      'Status',      'Select', TRUE,  'Draft,Pending Approval,Approved,Rejected', 8),
        ('Employee',  'cost_rate',   'Cost rate per hour', 'Number', FALSE, NULL, 91)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = options || ',project_labour_cost'
       WHERE doctype_name = 'PostingProfile' AND fieldname = 'event'
         AND options IS NOT NULL AND options NOT LIKE '%%project_labour_cost%%'
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'Timesheet', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'Timesheet', TRUE, TRUE, TRUE, FALSE),
        ('Accountant',    'Timesheet', TRUE, FALSE, FALSE, FALSE),
        ('HR/Admin',      'Timesheet', TRUE, TRUE, TRUE, FALSE),
        ('Cashier',       'Timesheet', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('Timesheet', 0, NULL, 'Store Manager')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END $$;
