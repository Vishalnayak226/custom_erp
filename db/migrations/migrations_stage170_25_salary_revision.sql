-- ---------------------------------------------------------------------------
-- Stage 170.25: salary revision through the maker-checker approval engine.
-- Once Approved, a revision is the salary for payroll months on or after its
-- effective date (the engine reads it at run time; the SalaryStructure row is
-- never edited). Only HR / payroll roles can read it. Additive only.
-- ---------------------------------------------------------------------------

DO $$
DECLARE
  s RECORD;
BEGIN
  FOR s IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default'
        OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = s.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('SalaryRevision', 'HR', 'hr', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, s.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('SalaryRevision', 'code',             'Code',            'Data',   TRUE,  NULL, 1),
        ('SalaryRevision', 'employee_id',      'Employee',        'Link',   TRUE,  'Employee', 2),
        ('SalaryRevision', 'effective_date',   'Effective date',  'Date',   TRUE,  NULL, 3),
        ('SalaryRevision', 'basic',            'New basic',       'Number', FALSE, NULL, 4),
        ('SalaryRevision', 'hra',              'New HRA',         'Number', FALSE, NULL, 5),
        ('SalaryRevision', 'other_allowances', 'New other allowances', 'Number', FALSE, NULL, 6),
        ('SalaryRevision', 'pf_percent',       'New PF %% of basic', 'Number', FALSE, NULL, 7),
        ('SalaryRevision', 'esi_percent',      'New ESI %% of gross', 'Number', FALSE, NULL, 8),
        ('SalaryRevision', 'pt_amount',        'New professional tax', 'Number', FALSE, NULL, 9),
        ('SalaryRevision', 'reason',           'Reason',          'Data',   TRUE,  NULL, 10),
        ('SalaryRevision', 'status',           'Status',          'Select', TRUE,  'Draft,Pending Approval,Approved,Rejected', 11)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, s.schema_name);

    IF to_regclass(format('%I.role_permissions', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('Super Admin', 'SalaryRevision', TRUE, TRUE, TRUE, TRUE),
          ('HR/Admin',    'SalaryRevision', TRUE, TRUE, TRUE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, s.schema_name);
    END IF;

    IF to_regclass(format('%I.approval_rules', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT 'SalaryRevision', 0, NULL, 'HR/Admin'
         WHERE NOT EXISTS (SELECT 1 FROM %1$I.approval_rules WHERE doctype = 'SalaryRevision')
      $sql$, s.schema_name);
    END IF;
  END LOOP;
END $$;
