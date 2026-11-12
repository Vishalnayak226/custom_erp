-- ---------------------------------------------------------------------------
-- Stage 146.2.6: approved transfer of loyalty points between customers.
--
-- LoyaltyPointTransfer is the transfer record (the source customer's one-time
-- code, then - above the approval value - a manager's approval). The points
-- themselves move as new loyalty_point_ledger rows; nothing existing changes.
-- Off by default (setting loyalty.points_transfer). Additive and idempotent,
-- replayed into every tenant schema.
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
      VALUES ('LoyaltyPointTransfer', 'CRM', 'Transaction', 'crm_loyalty')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('LoyaltyPointTransfer', 'from_customer', 'From customer', 'Data', TRUE, NULL, 1),
        ('LoyaltyPointTransfer', 'to_customer', 'To customer', 'Data', TRUE, NULL, 2),
        ('LoyaltyPointTransfer', 'points', 'Points', 'Number', TRUE, NULL, 3),
        ('LoyaltyPointTransfer', 'points_value', 'Value', 'Number', TRUE, NULL, 4),
        ('LoyaltyPointTransfer', 'challenge_id', 'Code sent', 'Data', FALSE, NULL, 5),
        ('LoyaltyPointTransfer', 'status', 'Status', 'Select', TRUE, 'Awaiting OTP,Draft,Pending Approval,Approved,Posted,Rejected', 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'LoyaltyPointTransfer', TRUE, FALSE, FALSE, FALSE),
             ('Store Manager', 'LoyaltyPointTransfer', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('LoyaltyPointTransfer', 500, NULL, 'Store Manager')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
