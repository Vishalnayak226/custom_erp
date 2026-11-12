-- ---------------------------------------------------------------------------
-- Stage 73.2.11 / 73.2.12 / 73.3.1 - loyalty administration.
--
--   * LoyaltyPointAdjustment (73.2.11): a manual credit or debit of a
--     customer's points with a reason - posted as a new ledger row, never an
--     edit; above the approval value (LoyaltyPointAdjustment approval rule,
--     shipped 500+ Store Manager) it waits for a manager. ReasonCode category
--     "Loyalty Adjustment".
--   * GL 2630 Loyalty Points Liability and 5260 Loyalty Points Expense
--     (73.2.12): used only when loyalty.liability_accounting is set to
--     "Accrue at earn" - otherwise points are expensed when redeemed (5250),
--     exactly as today.
--   * loyalty_tier_rules.benefits and .grace_days (73.3.1): what a tier gives,
--     and how long a customer keeps it after their spend drops below it.
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
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'loyalty_tier_rules') THEN
      EXECUTE format($sql$ ALTER TABLE %I.loyalty_tier_rules ADD COLUMN IF NOT EXISTS benefits TEXT $sql$, schema_rec.schema_name);
      EXECUTE format($sql$ ALTER TABLE %I.loyalty_tier_rules ADD COLUMN IF NOT EXISTS grace_days INT NOT NULL DEFAULT 0 $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, document_type, module_key)
      VALUES ('LoyaltyPointAdjustment', 'CRM', 'Transaction', 'crm_loyalty')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('LoyaltyPointAdjustment', 'customer_id', 'Customer', 'Data', TRUE, NULL, 1),
        ('LoyaltyPointAdjustment', 'points', 'Points (+ credit, - debit)', 'Number', TRUE, NULL, 2),
        ('LoyaltyPointAdjustment', 'points_value', 'Value', 'Number', TRUE, NULL, 3),
        ('LoyaltyPointAdjustment', 'reason_code', 'Reason', 'Link', TRUE, 'ReasonCode', 4),
        ('LoyaltyPointAdjustment', 'note', 'Note', 'Data', FALSE, NULL, 5),
        ('LoyaltyPointAdjustment', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Posted,Rejected', 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'LoyaltyPointAdjustment', TRUE, FALSE, FALSE, FALSE),
             ('Store Manager', 'LoyaltyPointAdjustment', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = options || ',Loyalty Adjustment'
       WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
         AND options NOT LIKE '%%Loyalty Adjustment%%'
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
          ('2630', 'Loyalty Points Liability', 'Liability'),
          ('5260', 'Loyalty Points Expense', 'Expense')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('LoyaltyPointAdjustment', 500, NULL, 'Store Manager')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
