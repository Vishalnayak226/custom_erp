-- ---------------------------------------------------------------------------
-- Ask 170 item 170.32: project milestones and budget.
-- ProjectMilestone: a named stage of a project with an amount (or a share of
-- the contract value), a due date and a status; completing a billable one
-- raises the customer's invoice. ProjectBudget: a budget by cost type for the
-- budget-vs-actual report. Project gains contract value, estimated cost and
-- the revenue method (used by 170.33). Additive only.
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
        ('ProjectMilestone', 'Finance', 'finance', 'Transaction'),
        ('ProjectBudget',    'Finance', 'finance', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('Project',          'contract_value',  'Contract value', 'Number', FALSE, NULL, 20),
        ('Project',          'estimated_cost',  'Estimated cost', 'Number', FALSE, NULL, 21),
        ('Project',          'rev_rec_method',  'Revenue method', 'Select', FALSE, ',Milestone,Cost to complete', 22),
        ('ProjectMilestone', 'project',         'Project',   'Link',   TRUE,  'Project', 1),
        ('ProjectMilestone', 'name',            'Milestone', 'Data',   TRUE,  NULL, 2),
        ('ProjectMilestone', 'amount',          'Amount',    'Number', FALSE, NULL, 3),
        ('ProjectMilestone', 'percent',         'Percent of contract', 'Number', FALSE, NULL, 4),
        ('ProjectMilestone', 'due_date',        'Due date',  'Date',   FALSE, NULL, 5),
        ('ProjectMilestone', 'billable',        'Billable',  'Select', TRUE,  'Yes,No', 6),
        ('ProjectMilestone', 'invoice_id',      'Invoice',   'Data',   FALSE, NULL, 7),
        ('ProjectMilestone', 'status',          'Status',    'Select', TRUE,  'Planned,Completed,Invoiced', 8),
        ('ProjectBudget',    'project',         'Project',   'Link',   TRUE,  'Project', 1),
        ('ProjectBudget',    'lines',           'Budget',    'JSONTable', TRUE,
         '[{"key":"cost_type","label":"Cost type (Labour, Materials, Expenses, Other)","type":"data","required":true},{"key":"amount","label":"Amount","type":"number","required":true}]', 2),
        ('ProjectBudget',    'status',          'Status',    'Select', TRUE,  'Active,Closed', 3)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'ProjectMilestone', TRUE, TRUE, TRUE, FALSE),
        ('Accountant',    'ProjectMilestone', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'ProjectMilestone', TRUE, FALSE, FALSE, FALSE),
        ('Super Admin',   'ProjectBudget',    TRUE, TRUE, TRUE, FALSE),
        ('Accountant',    'ProjectBudget',    TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'ProjectBudget',    TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
