-- ---------------------------------------------------------------------------
-- Stage 170.5: leases (IND AS 116). Lessee: right-of-use asset and lease
-- liability at the present value of the payments, interest / payment /
-- depreciation posted monthly. Lessor (operating): rent receivable and income
-- while the leased-out asset stays on the books.
--
-- Adds five chart accounts (ON CONFLICT DO NOTHING, so a client's own row of
-- the same code is kept) and the Lease doctype. Additive and safe to run twice.
-- The release counts the new gl_accounts rows as a data change.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
          ('1420', 'Right-of-Use Assets', 'Asset'),
          ('2450', 'Lease Liability', 'Liability'),
          ('4310', 'Lease Rental Income', 'Revenue'),
          ('5311', 'Right-of-Use Depreciation', 'Expense'),
          ('5312', 'Lease Interest Expense', 'Expense')
        ON CONFLICT DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('Lease', 'Finance', 'assets', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('Lease', 'code',              'Lease no',          'Data',   TRUE,  NULL, 1),
        ('Lease', 'lease_type',        'Type',              'Select', TRUE,  'Lessee,Lessor', 2),
        ('Lease', 'vendor',            'Vendor',            'Link',   FALSE, 'Vendor', 3),
        ('Lease', 'customer',          'Customer',          'Link',   FALSE, 'Customer', 4),
        ('Lease', 'asset',             'Asset leased out',  'Link',   FALSE, 'Asset', 5),
        ('Lease', 'description',       'Description',       'Data',   FALSE, NULL, 6),
        ('Lease', 'start_date',        'Start date',        'Date',   TRUE,  NULL, 7),
        ('Lease', 'term_months',       'Term (months)',     'Number', TRUE,  NULL, 8),
        ('Lease', 'payment_amount',    'Payment per month', 'Number', TRUE,  NULL, 9),
        ('Lease', 'payment_timing',    'Paid',              'Select', FALSE, 'Arrears,Advance', 10),
        ('Lease', 'discount_rate',     'Discount rate %% a year', 'Number', FALSE, NULL, 11),
        ('Lease', 'rou_account',       'Right-of-use account',   'Data', FALSE, NULL, 12),
        ('Lease', 'liability_account', 'Lease liability account','Data', FALSE, NULL, 13),
        ('Lease', 'interest_account',  'Interest account',       'Data', FALSE, NULL, 14),
        ('Lease', 'depreciation_account', 'Depreciation account','Data', FALSE, NULL, 15),
        ('Lease', 'payment_account',   'Payment credit account', 'Data', FALSE, NULL, 16),
        ('Lease', 'liability_opening', 'Opening liability', 'Number', FALSE, NULL, 17),
        ('Lease', 'periods_posted',    'Months posted',     'Number', FALSE, NULL, 18),
        ('Lease', 'schedule',          'Schedule',          'Data',   FALSE, NULL, 19),
        ('Lease', 'status',            'Status',            'Select', TRUE,  'Draft,Active,Closed', 20)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin', 'Lease', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin',    'Lease', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'Lease', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

  END LOOP;
END $mig$;
