-- ---------------------------------------------------------------------------
-- Ask 176 item 176.2: travel requests. A TravelRequest (who, where, when,
-- estimated cost) goes through the maker-checker approval; an employee
-- advance (StaffAdvance) and an expense claim may then name it, and only an
-- Approved request of the same employee may be named
-- (engines/expense_travel_request.go). The estimated cost is stored as
-- "amount" so approval slabs read it like every other document.
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
        ('TravelRequest', 'Finance', 'expenses', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('TravelRequest', 'code',           'Request number', 'Data',   TRUE,  NULL, 1),
        ('TravelRequest', 'employee_id',    'Employee',       'Link',   TRUE,  'Employee', 2),
        ('TravelRequest', 'purpose',        'Purpose',        'Data',   TRUE,  NULL, 3),
        ('TravelRequest', 'destination',    'Destination',    'Data',   TRUE,  NULL, 4),
        ('TravelRequest', 'from_date',      'From',           'Date',   TRUE,  NULL, 5),
        ('TravelRequest', 'to_date',        'To',             'Date',   TRUE,  NULL, 6),
        ('TravelRequest', 'amount',         'Estimated cost', 'Number', TRUE,  NULL, 7),
        ('TravelRequest', 'advance_needed', 'Advance needed', 'Number', FALSE, NULL, 8),
        ('TravelRequest', 'status',         'Status',         'Select', TRUE,  'Draft,Pending Approval,Approved,Rejected', 9),
        ('StaffAdvance',  'travel_request', 'Travel request', 'Link',   FALSE, 'TravelRequest', 7),
        ('ExpenseClaim',  'travel_request', 'Travel request', 'Link',   FALSE, 'TravelRequest', 21)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'TravelRequest', TRUE, TRUE,  TRUE,  FALSE),
        ('Store Manager', 'TravelRequest', TRUE, TRUE,  TRUE,  FALSE),
        ('Accountant',    'TravelRequest', TRUE, FALSE, FALSE, FALSE),
        ('HR/Admin',      'TravelRequest', TRUE, TRUE,  TRUE,  FALSE),
        ('Cashier',       'TravelRequest', TRUE, TRUE,  TRUE,  FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('TravelRequest', 0, NULL, 'Store Manager')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END $$;
