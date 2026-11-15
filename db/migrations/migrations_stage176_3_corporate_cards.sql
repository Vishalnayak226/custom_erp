-- ---------------------------------------------------------------------------
-- Ask 176 item 176.3: corporate cards. A CorporateCard is held by one
-- employee; its statement lines (CardStatementLine) are loaded with the
-- record list's CSV import. On every save a line is matched to the card
-- holder's expense claim of the same amount, dated within 7 days, that no
-- other line holds - or to the claim typed on it, if it fits
-- (engines/expense_corporate_card.go). The "Card lines not matched" report
-- lists the rest.
-- Additive and idempotent, replayed into every tenant schema.
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
        ('CorporateCard',     'Finance', 'expenses', 'Master'),
        ('CardStatementLine', 'Finance', 'expenses', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('CorporateCard', 'code',        'Code',          'Data',   TRUE,  NULL, 1),
        ('CorporateCard', 'name',        'Card name',     'Data',   TRUE,  NULL, 2),
        ('CorporateCard', 'employee_id', 'Card holder',   'Link',   TRUE,  'Employee', 3),
        ('CorporateCard', 'last4',       'Last 4 digits', 'Data',   TRUE,  NULL, 4),
        ('CorporateCard', 'issuer',      'Issuer',        'Data',   FALSE, NULL, 5),
        ('CorporateCard', 'status',      'Status',        'Select', TRUE,  'Active,Blocked,Closed', 6),
        ('CardStatementLine', 'card',          'Card',        'Link',   TRUE,  'CorporateCard', 1),
        ('CardStatementLine', 'txn_date',      'Date',        'Date',   TRUE,  NULL, 2),
        ('CardStatementLine', 'description',   'Description', 'Data',   TRUE,  NULL, 3),
        ('CardStatementLine', 'amount',        'Amount',      'Number', TRUE,  NULL, 4),
        ('CardStatementLine', 'reference',     'Reference',   'Data',   FALSE, NULL, 5),
        ('CardStatementLine', 'matched_claim', 'Claim',       'Link',   FALSE, 'ExpenseClaim', 6),
        ('CardStatementLine', 'status',        'Status',      'Select', TRUE,  'Unmatched,Matched', 7)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin', 'CorporateCard',     TRUE, TRUE, TRUE, FALSE),
        ('Accountant',  'CorporateCard',     TRUE, TRUE, TRUE, FALSE),
        ('HR/Admin',    'CorporateCard',     TRUE, TRUE, TRUE, FALSE),
        ('Super Admin', 'CardStatementLine', TRUE, TRUE, TRUE, TRUE),
        ('Accountant',  'CardStatementLine', TRUE, TRUE, TRUE, TRUE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
