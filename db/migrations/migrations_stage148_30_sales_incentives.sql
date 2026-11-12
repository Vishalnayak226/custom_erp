-- ---------------------------------------------------------------------------
-- Stage 148.30: sales incentives (module sales_incentives, off by default).
--
--   sales_incentive_entries   one row per salesman per sold line: kind Accrual
--                             (a share of the line's margin) or Clawback (a
--                             negative row against an Accrual, after a return or
--                             a cancelled bill). paid_payslip is set when a
--                             payslip pays the row out.
--   IncentiveRule             a slab: category (blank = all), margin % from, pct.
--   IncentiveProfile          which login and employee a salesman is.
--   SalesInvoice.salesman_id / salesman_split   shown only while the module is on.
--
-- The module is registered switched OFF; a tenant with no entitlement row reads
-- as off. No per-client business row is written (no gl_accounts row either: the
-- two accounts are settings, defaulting to 5500 and 2400). Additive and
-- idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('sales_incentives', 'Sales incentives', 'A share of each sale''s margin for the salesmen, clawed back on returns, paid through payroll.', FALSE, FALSE)
ON CONFLICT (module_key) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.sales_incentive_entries (
        id             BIGSERIAL PRIMARY KEY,
        kind           VARCHAR(10) NOT NULL DEFAULT 'Accrual',
        source_doctype VARCHAR(100) NOT NULL,
        source_id      VARCHAR(200) NOT NULL,
        line_no        INT NOT NULL DEFAULT 0,
        ref            VARCHAR(200) NOT NULL DEFAULT '',
        salesman_id    VARCHAR(200) NOT NULL,
        sku            VARCHAR(200) NOT NULL DEFAULT '',
        category       VARCHAR(200) NOT NULL DEFAULT '',
        qty            NUMERIC NOT NULL DEFAULT 0,
        margin_paise   BIGINT NOT NULL DEFAULT 0,
        pct            NUMERIC NOT NULL DEFAULT 0,
        split_pct      NUMERIC NOT NULL DEFAULT 100,
        amount_paise   BIGINT NOT NULL DEFAULT 0,
        accrual_id     BIGINT,
        entry_date     DATE NOT NULL DEFAULT CURRENT_DATE,
        paid_payslip   VARCHAR(200) NOT NULL DEFAULT '',
        created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE UNIQUE INDEX IF NOT EXISTS sales_incentive_accrual_uq ON %I.sales_incentive_entries (source_doctype, source_id, line_no, salesman_id) WHERE kind = 'Accrual'
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE UNIQUE INDEX IF NOT EXISTS sales_incentive_clawback_uq ON %I.sales_incentive_entries (accrual_id, ref) WHERE kind = 'Clawback'
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS sales_incentive_salesman_idx ON %I.sales_incentive_entries (salesman_id, paid_payslip, entry_date)
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS sales_incentive_source_idx ON %I.sales_incentive_entries (source_id)
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('IncentiveRule',    'Sales', 'sales_incentives', 'Master'),
        ('IncentiveProfile', 'Sales', 'sales_incentives', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('IncentiveRule',    'category',       'Category (blank = all)', 'Link',   FALSE, 'Category', 1),
        ('IncentiveRule',    'min_margin_pct', 'Margin from %%',         'Number', TRUE,  NULL, 2),
        ('IncentiveRule',    'pct',            'Incentive %% of margin', 'Number', TRUE,  NULL, 3),
        ('IncentiveRule',    'status',         'Status',                 'Select', TRUE,  'Active,Inactive', 4),
        ('IncentiveProfile', 'salesman_id',    'Salesman',               'Link',   TRUE,  'Salesman', 1),
        ('IncentiveProfile', 'user',           'Login',                  'Data',   FALSE, NULL, 2),
        ('IncentiveProfile', 'employee_id',    'Employee',               'Link',   FALSE, 'Employee', 3),
        ('IncentiveProfile', 'status',         'Status',                 'Select', TRUE,  'Active,Inactive', 4)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT 'SalesInvoice', 'salesman_id', 'Salesman', 'Link', FALSE, 'Salesman', 126
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = 'SalesInvoice')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT 'SalesInvoice', 'salesman_split', 'Salesman split', 'JSONTable', FALSE,
             '[{"key":"salesman_id","label":"Salesman","type":"link","link":"Salesman","required":true},{"key":"pct","label":"%%","type":"number","required":true}]', 127
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = 'SalesInvoice')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('HR/Admin', 'IncentiveRule', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'IncentiveRule', TRUE, FALSE, FALSE, FALSE),
        ('HR/Admin', 'IncentiveProfile', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'IncentiveProfile', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
