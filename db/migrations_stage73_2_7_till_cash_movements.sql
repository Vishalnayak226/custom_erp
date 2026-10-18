-- ---------------------------------------------------------------------------
-- Stage 73.2.7 - till cash movements, an approved close above the variance
-- tolerance, and shift handover.
--
--   * POSCashMovement: Paid In (cash brought into the till from the store's
--     safe), Paid Out (a petty expense paid from the till), Safe Drop (cash
--     taken out of the till into the safe). Engine-written only, read-only
--     everywhere, like POSSession.
--   * GL 5290 Till Petty Cash Expenses: where a Paid Out posts.
--   * ReasonCode category "Cash Movement": every movement names one.
--   * POSSession status gains "Pending Approval": a close whose count is off
--     by more than the tolerance waits for a Store Manager (approval rule
--     POSSession, 0 and over, Store Manager) - only reached above the
--     tolerance, which stays pos.drawer_variance_tolerance.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
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
      INSERT INTO %I.doctype_meta (name, module, document_type, module_key)
      VALUES ('POSCashMovement', 'Sales', 'Transaction', 'sales')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('POSCashMovement', 'session_id', 'Session', 'Data', TRUE, NULL, 1),
        ('POSCashMovement', 'location', 'Store', 'Data', TRUE, NULL, 2),
        ('POSCashMovement', 'cashier', 'Cashier', 'Data', TRUE, NULL, 3),
        ('POSCashMovement', 'movement_type', 'Type', 'Select', TRUE, 'Paid In,Paid Out,Safe Drop', 4),
        ('POSCashMovement', 'amount', 'Amount', 'Number', TRUE, NULL, 5),
        ('POSCashMovement', 'reason_code', 'Reason', 'Link', TRUE, 'ReasonCode', 6),
        ('POSCashMovement', 'reference', 'Reference (voucher, bag no.)', 'Data', FALSE, NULL, 7),
        ('POSCashMovement', 'status', 'Status', 'Select', TRUE, 'Posted', 8),
        ('POSSession', 'variance_reason', 'Variance reason', 'Data', FALSE, NULL, 9),
        ('POSSession', 'handed_over_to', 'Handed over to', 'Data', FALSE, NULL, 10),
        ('POSSession', 'taken_over_from', 'Taken over from session', 'Data', FALSE, NULL, 11)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET options = 'Open,Pending Approval,Closed'
       WHERE doctype_name = 'POSSession' AND fieldname = 'status' AND options = 'Open,Closed'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = options || ',Cash Movement'
       WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
         AND options NOT LIKE '%%Cash Movement%%'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'POSCashMovement', TRUE, FALSE, FALSE, FALSE),
             ('Store Manager', 'POSCashMovement', TRUE, FALSE, FALSE, FALSE),
             ('Cashier', 'POSCashMovement', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type)
        VALUES ('5290', 'Till Petty Cash Expenses', 'Expense')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('POSSession', 0, NULL, 'Store Manager')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
