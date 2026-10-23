-- Stage 130.15 (74.2.4, BRD 4.3; DEC-06 / DEC-07 at the BRD defaults): the
-- internal Material Issue (request -> approve -> issue, MI series) and the
-- Material Return against it (MR series). Engine-written (their routes
-- validate and post); the record API reads them. Additive.
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
        ('MaterialIssue', 'Inventory', 'inventory', 'Transaction'),
        ('MaterialReturn', 'Inventory', 'inventory', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('MaterialIssue', 'code',        'Material issue no', 'Data',      TRUE,  NULL, 1),
        ('MaterialIssue', 'location',    'Store',             'Link',      TRUE,  'Location', 2),
        ('MaterialIssue', 'department',  'Department',        'Link',      TRUE,  'Department', 3),
        ('MaterialIssue', 'employee',    'Employee',          'Data',      FALSE, NULL, 4),
        ('MaterialIssue', 'cost_center', 'Cost centre',       'Link',      FALSE, 'CostCenter', 5),
        ('MaterialIssue', 'project',     'Project',           'Data',      FALSE, NULL, 6),
        ('MaterialIssue', 'purpose',     'Purpose',           'Data',      TRUE,  NULL, 7),
        ('MaterialIssue', 'items',       'Items',             'JSONTable', TRUE,
         '[{"key":"sku","label":"Item","type":"data","required":true},{"key":"qty","label":"Qty","type":"number","required":true},{"key":"issued_qty","label":"Issued","type":"number"},{"key":"returned_qty","label":"Returned","type":"number"}]', 8),
        ('MaterialIssue', 'amount',      'Value',             'Number',    FALSE, NULL, 9),
        ('MaterialIssue', 'status',      'Status',            'Select',    TRUE,  'Draft,Pending Approval,Approved,Rejected,Issued,Partly Returned,Returned', 10),
        ('MaterialReturn', 'code',       'Material return no','Data',      TRUE,  NULL, 1),
        ('MaterialReturn', 'issue_id',   'Against issue',     'Data',      TRUE,  NULL, 2),
        ('MaterialReturn', 'location',   'Store',             'Link',      TRUE,  'Location', 3),
        ('MaterialReturn', 'items',      'Items',             'JSONTable', TRUE,
         '[{"key":"sku","label":"Item","type":"data","required":true},{"key":"qty","label":"Qty","type":"number","required":true},{"key":"condition","label":"Condition","type":"data"}]', 4),
        ('MaterialReturn', 'status',     'Status',            'Select',    TRUE,  'Posted', 5)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('HR/Admin', 'MaterialIssue', TRUE, TRUE, FALSE, FALSE),
          ('Store Manager', 'MaterialIssue', TRUE, TRUE, FALSE, FALSE),
          ('Cashier', 'MaterialIssue', TRUE, TRUE, FALSE, FALSE),
          ('HR/Admin', 'MaterialReturn', TRUE, TRUE, FALSE, FALSE),
          ('Store Manager', 'MaterialReturn', TRUE, TRUE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
