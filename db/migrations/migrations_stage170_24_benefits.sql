-- ---------------------------------------------------------------------------
-- Stage 170.24: benefit plans and employee enrolment. Enrolled benefits appear on
-- the payslip and in its GL posting (new account 2405 Benefits Payable). Additive
-- only: an employee with no enrolment gets a payslip exactly as before.
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
        ('BenefitPlan',      'HR', 'hr', 'Master'),
        ('BenefitEnrolment', 'HR', 'hr', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, s.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('BenefitPlan', 'code',         'Code',        'Data',   TRUE,  NULL, 1),
        ('BenefitPlan', 'name',         'Name',        'Data',   TRUE,  NULL, 2),
        ('BenefitPlan', 'benefit_type', 'Type',        'Select', TRUE,  'Earning,Deduction,Employer-paid', 3),
        ('BenefitPlan', 'calc_method',  'Calculated as', 'Select', TRUE, 'Amount,Percent of basic', 4),
        ('BenefitPlan', 'value',        'Amount or %%', 'Number', TRUE,  NULL, 5),
        ('BenefitPlan', 'status',       'Status',      'Select', TRUE,  'Active,Inactive', 6),
        ('BenefitEnrolment', 'code',           'Code',        'Data',   TRUE,  NULL, 1),
        ('BenefitEnrolment', 'employee_id',    'Employee',    'Link',   TRUE,  'Employee', 2),
        ('BenefitEnrolment', 'plan_id',        'Plan',        'Link',   TRUE,  'BenefitPlan', 3),
        ('BenefitEnrolment', 'start_date',     'Start date',  'Date',   TRUE,  NULL, 4),
        ('BenefitEnrolment', 'end_date',       'End date',    'Date',   FALSE, NULL, 5),
        ('BenefitEnrolment', 'value_override', 'Own amount or %%', 'Number', FALSE, NULL, 6),
        ('BenefitEnrolment', 'status',         'Status',      'Select', TRUE,  'Active,Ended', 7),
        ('Payslip', 'benefit_earnings',   'Benefits (earning)',       'Number', FALSE, NULL, 30),
        ('Payslip', 'benefit_deductions', 'Benefits (deduction)',     'Number', FALSE, NULL, 31),
        ('Payslip', 'benefit_employer',   'Benefits (employer-paid)', 'Number', FALSE, NULL, 32),
        ('Payslip', 'benefit_lines',      'Benefit lines',            'Data',   FALSE, NULL, 33)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, s.schema_name);

    IF to_regclass(format('%I.gl_accounts', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
          ('2405', 'Benefits Payable', 'Liability')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, s.schema_name);
    END IF;

    IF to_regclass(format('%I.role_permissions', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('Super Admin', 'BenefitPlan',      TRUE, TRUE, TRUE, TRUE),
          ('HR/Admin',    'BenefitPlan',      TRUE, TRUE, TRUE, TRUE),
          ('Super Admin', 'BenefitEnrolment', TRUE, TRUE, TRUE, TRUE),
          ('HR/Admin',    'BenefitEnrolment', TRUE, TRUE, TRUE, TRUE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, s.schema_name);
    END IF;
  END LOOP;
END $$;
