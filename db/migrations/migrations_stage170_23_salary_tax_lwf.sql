-- ---------------------------------------------------------------------------
-- Stage 170.23: salary TDS by slab, labour welfare fund (LWF), PF ECR, Form 24Q.
--
-- TaxSlab / TaxRegimeRule are editable masters per regime (Old / New) and
-- financial year. The FY 2025-26 rows below are the published figures, seeded
-- as ordinary editable rows. A CA must confirm every statutory figure before
-- use; nothing computes from them until setting hr.tds_by_slab is switched on.
-- LWFRate is an editable master with NO seed rows (state rates differ and are
-- revised often). Additive only: no existing row is touched.
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
        ('TaxSlab',       'HR', 'hr', 'Master'),
        ('TaxRegimeRule', 'HR', 'hr', 'Master'),
        ('LWFRate',       'HR', 'hr', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, s.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('TaxSlab', 'code',         'Code',            'Data',   TRUE,  NULL, 1),
        ('TaxSlab', 'regime',       'Regime',          'Select', TRUE,  'Old,New', 2),
        ('TaxSlab', 'fin_year',     'Financial year',  'Data',   TRUE,  NULL, 3),
        ('TaxSlab', 'from_amount',  'Income from',     'Number', TRUE,  NULL, 4),
        ('TaxSlab', 'to_amount',    'Income up to',    'Number', FALSE, NULL, 5),
        ('TaxSlab', 'rate_percent', 'Rate %%',          'Number', TRUE,  NULL, 6),
        ('TaxSlab', 'status',       'Status',          'Select', TRUE,  'Active,Inactive', 7),
        ('TaxRegimeRule', 'code',                 'Code',                  'Data',   TRUE,  NULL, 1),
        ('TaxRegimeRule', 'regime',               'Regime',                'Select', TRUE,  'Old,New', 2),
        ('TaxRegimeRule', 'fin_year',             'Financial year',        'Data',   TRUE,  NULL, 3),
        ('TaxRegimeRule', 'standard_deduction',   'Standard deduction',    'Number', TRUE,  NULL, 4),
        ('TaxRegimeRule', 'rebate_income_limit',  'Rebate income limit',   'Number', FALSE, NULL, 5),
        ('TaxRegimeRule', 'rebate_max',           'Rebate up to',          'Number', FALSE, NULL, 6),
        ('TaxRegimeRule', 'marginal_relief',      'Marginal relief',       'Select', FALSE, 'Yes,No', 7),
        ('TaxRegimeRule', 'cess_percent',         'Cess %%',                'Number', FALSE, NULL, 8),
        ('TaxRegimeRule', 'status',               'Status',                'Select', TRUE,  'Active,Inactive', 9),
        ('LWFRate', 'code',            'Code',               'Data',   TRUE,  NULL, 1),
        ('LWFRate', 'state',           'State',              'Data',   TRUE,  NULL, 2),
        ('LWFRate', 'employee_amount', 'Employee amount',    'Number', TRUE,  NULL, 3),
        ('LWFRate', 'employer_amount', 'Employer amount',    'Number', TRUE,  NULL, 4),
        ('LWFRate', 'months',          'Months (e.g. 6,12)', 'Data',   TRUE,  NULL, 5),
        ('LWFRate', 'status',          'Status',             'Select', TRUE,  'Active,Inactive', 6),
        ('Employee', 'tax_regime',              'Tax regime',                   'Select', FALSE, 'New,Old', 160),
        ('Employee', 'tax_declared_deductions', 'Declared deductions (yearly)', 'Number', FALSE, NULL, 161),
        ('Employee', 'pan',                     'PAN',                          'Data',   FALSE, NULL, 162),
        ('Employee', 'uan',                     'UAN',                          'Data',   FALSE, NULL, 163),
        ('Employee', 'work_state',              'Work state (for LWF)',         'Data',   FALSE, NULL, 164),
        ('Payslip',  'lwf_deduction',           'LWF (employee)',               'Number', FALSE, NULL, 20),
        ('Payslip',  'lwf_employer',            'LWF (employer)',               'Number', FALSE, NULL, 21),
        ('Payslip',  'tax_regime',              'Tax regime',                   'Data',   FALSE, NULL, 22)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, s.schema_name);

    IF to_regclass(format('%I.gl_accounts', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
          ('2404', 'LWF Payable', 'Liability')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, s.schema_name);
    END IF;

    IF to_regclass(format('%I.role_permissions', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('Super Admin', 'TaxSlab',       TRUE, TRUE, TRUE, TRUE),
          ('HR/Admin',    'TaxSlab',       TRUE, TRUE, TRUE, TRUE),
          ('Super Admin', 'TaxRegimeRule', TRUE, TRUE, TRUE, TRUE),
          ('HR/Admin',    'TaxRegimeRule', TRUE, TRUE, TRUE, TRUE),
          ('Super Admin', 'LWFRate',       TRUE, TRUE, TRUE, TRUE),
          ('HR/Admin',    'LWFRate',       TRUE, TRUE, TRUE, TRUE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, s.schema_name);
    END IF;

    -- FY 2025-26 published figures as editable rows (they need a user to own them).
    IF to_regclass(format('%I.users', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.documents (id, doctype, data, status, created_by)
        SELECT v.id, v.doctype, v.data::jsonb, 'Active',
               (SELECT id FROM %1$I.users ORDER BY (id = 'system') DESC, created_at LIMIT 1)
          FROM (VALUES
            ('TAXSLAB-2025-26-NEW-1', 'TaxSlab', '{"code":"TAXSLAB-2025-26-NEW-1","regime":"New","fin_year":"2025-26","from_amount":0,"to_amount":400000,"rate_percent":0,"status":"Active"}'),
            ('TAXSLAB-2025-26-NEW-2', 'TaxSlab', '{"code":"TAXSLAB-2025-26-NEW-2","regime":"New","fin_year":"2025-26","from_amount":400000,"to_amount":800000,"rate_percent":5,"status":"Active"}'),
            ('TAXSLAB-2025-26-NEW-3', 'TaxSlab', '{"code":"TAXSLAB-2025-26-NEW-3","regime":"New","fin_year":"2025-26","from_amount":800000,"to_amount":1200000,"rate_percent":10,"status":"Active"}'),
            ('TAXSLAB-2025-26-NEW-4', 'TaxSlab', '{"code":"TAXSLAB-2025-26-NEW-4","regime":"New","fin_year":"2025-26","from_amount":1200000,"to_amount":1600000,"rate_percent":15,"status":"Active"}'),
            ('TAXSLAB-2025-26-NEW-5', 'TaxSlab', '{"code":"TAXSLAB-2025-26-NEW-5","regime":"New","fin_year":"2025-26","from_amount":1600000,"to_amount":2000000,"rate_percent":20,"status":"Active"}'),
            ('TAXSLAB-2025-26-NEW-6', 'TaxSlab', '{"code":"TAXSLAB-2025-26-NEW-6","regime":"New","fin_year":"2025-26","from_amount":2000000,"to_amount":2400000,"rate_percent":25,"status":"Active"}'),
            ('TAXSLAB-2025-26-NEW-7', 'TaxSlab', '{"code":"TAXSLAB-2025-26-NEW-7","regime":"New","fin_year":"2025-26","from_amount":2400000,"rate_percent":30,"status":"Active"}'),
            ('TAXSLAB-2025-26-OLD-1', 'TaxSlab', '{"code":"TAXSLAB-2025-26-OLD-1","regime":"Old","fin_year":"2025-26","from_amount":0,"to_amount":250000,"rate_percent":0,"status":"Active"}'),
            ('TAXSLAB-2025-26-OLD-2', 'TaxSlab', '{"code":"TAXSLAB-2025-26-OLD-2","regime":"Old","fin_year":"2025-26","from_amount":250000,"to_amount":500000,"rate_percent":5,"status":"Active"}'),
            ('TAXSLAB-2025-26-OLD-3', 'TaxSlab', '{"code":"TAXSLAB-2025-26-OLD-3","regime":"Old","fin_year":"2025-26","from_amount":500000,"to_amount":1000000,"rate_percent":20,"status":"Active"}'),
            ('TAXSLAB-2025-26-OLD-4', 'TaxSlab', '{"code":"TAXSLAB-2025-26-OLD-4","regime":"Old","fin_year":"2025-26","from_amount":1000000,"rate_percent":30,"status":"Active"}'),
            ('TAXRULE-2025-26-NEW', 'TaxRegimeRule', '{"code":"TAXRULE-2025-26-NEW","regime":"New","fin_year":"2025-26","standard_deduction":75000,"rebate_income_limit":1200000,"rebate_max":60000,"marginal_relief":"Yes","cess_percent":4,"status":"Active"}'),
            ('TAXRULE-2025-26-OLD', 'TaxRegimeRule', '{"code":"TAXRULE-2025-26-OLD","regime":"Old","fin_year":"2025-26","standard_deduction":50000,"rebate_income_limit":500000,"rebate_max":12500,"marginal_relief":"No","cess_percent":4,"status":"Active"}')
          ) AS v(id, doctype, data)
         WHERE EXISTS (SELECT 1 FROM %1$I.users)
        ON CONFLICT (id) DO NOTHING
      $sql$, s.schema_name);
    END IF;
  END LOOP;
END $$;
