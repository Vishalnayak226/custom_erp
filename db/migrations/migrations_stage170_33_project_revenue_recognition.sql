-- ---------------------------------------------------------------------------
-- Ask 170 item 170.33: project revenue recognition. A monthly run books, per
-- project, the revenue earned so far against what has been billed: unbilled
-- revenue (1330 Contract Asset) when earned is ahead, deferred revenue (2600)
-- when billing is ahead. ProjectRevenueEntry holds each project-month's result
-- (and is the once-per-month guard). Behind projects.revenue_recognition,
-- default off. Additive only.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
  ('1330', 'Unbilled Revenue (Contract Asset)', 'Asset'),
  ('2600', 'Deferred Revenue', 'Liability')
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
        ('1330', 'Unbilled Revenue (Contract Asset)', 'Asset'),
        ('2600', 'Deferred Revenue', 'Liability')
      ON CONFLICT (account_code) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('ProjectRevenueEntry', 'Finance', 'finance', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('ProjectRevenueEntry', 'project',   'Project',   'Link',   TRUE,  'Project', 1),
        ('ProjectRevenueEntry', 'period',    'Month',     'Data',   TRUE,  NULL, 2),
        ('ProjectRevenueEntry', 'earned',    'Earned',    'Number', FALSE, NULL, 3),
        ('ProjectRevenueEntry', 'billed',    'Billed',    'Number', FALSE, NULL, 4),
        ('ProjectRevenueEntry', 'asset',     'Unbilled',  'Number', FALSE, NULL, 5),
        ('ProjectRevenueEntry', 'liability', 'Deferred',  'Number', FALSE, NULL, 6),
        ('ProjectRevenueEntry', 'status',    'Status',    'Select', TRUE,  'Posted', 7)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = options || ',project_revenue_asset,project_revenue_deferral'
       WHERE doctype_name = 'PostingProfile' AND fieldname = 'event'
         AND options IS NOT NULL AND options NOT LIKE '%%project_revenue_asset%%'
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin', 'ProjectRevenueEntry', TRUE, FALSE, FALSE, FALSE),
        ('Accountant',  'ProjectRevenueEntry', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
