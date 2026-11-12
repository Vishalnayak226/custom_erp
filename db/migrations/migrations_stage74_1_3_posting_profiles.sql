-- ---------------------------------------------------------------------------
-- Stage 74.1.3 posting profiles (BRD §3, §5, DEC-07, DEC-08, DEC-21): which
-- accounts an economic event posts to, set by the client instead of fixed in
-- code. A PostingProfile maps an event + optional legal entity / reason code /
-- loss class / cost centre to a debit and / or credit account, with effective
-- dates; it applies only once Approved (maker-checker through approval_rules,
-- Accountant). With no approved profile an event posts exactly as it does today
-- (DEC-21), so nothing already posting changes.
--
-- The accounts the events need are seeded additively (decided 2026-10-19,
-- ai_handover §6): 2650 Store Credit Liability, 4400 Inventory Gain, 5320
-- Inventory Shrinkage, 5330 Inventory Damage Write-off, 5340 Inventory
-- Revaluation, 5350 Expired Stock Write-off, 5420 Job-Work Conversion Cost,
-- 5430 Job-Work Loss, 5440 Samples / Non-returnable Challan Expense (material
-- issue keeps the existing 5180 Consumables Expense).
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('PostingProfile', 'Finance', 'finance', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('PostingProfile', 'code', 'Profile Code', 'Data', TRUE, NULL, 1),
        ('PostingProfile', 'name', 'Profile Name', 'Data', TRUE, NULL, 2),
        ('PostingProfile', 'event', 'Event', 'Select', TRUE, 'advance_collected,advance_applied,advance_refunded,store_credit_issued,store_credit_redeemed,store_credit_expired,material_issue,material_return,adjustment_gain,adjustment_loss,adjustment_damage,adjustment_revaluation,adjustment_expiry,jobwork_charge,jobwork_loss,challan_nonreturnable_issue', 3),
        ('PostingProfile', 'legal_entity', 'Only for legal entity', 'Link', FALSE, 'LegalEntity', 4),
        ('PostingProfile', 'reason_code', 'Only for reason code', 'Link', FALSE, 'ReasonCode', 5),
        ('PostingProfile', 'loss_class', 'Only for loss class', 'Select', FALSE, ',Shrinkage,Damage,Expiry,Theft,Revaluation', 6),
        ('PostingProfile', 'cost_center', 'Only for cost centre', 'Link', FALSE, 'CostCenter', 7),
        ('PostingProfile', 'debit_account', 'Debit account (blank = as today)', 'Data', FALSE, NULL, 8),
        ('PostingProfile', 'credit_account', 'Credit account (blank = as today)', 'Data', FALSE, NULL, 9),
        ('PostingProfile', 'effective_from', 'Effective from', 'Date', FALSE, NULL, 10),
        ('PostingProfile', 'effective_to', 'Effective to', 'Date', FALSE, NULL, 11),
        ('PostingProfile', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Inactive', 12)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'PostingProfile', TRUE, TRUE, TRUE, TRUE),
        ('Accountant', 'PostingProfile', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'PostingProfile', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('PostingProfile', 0, NULL, 'Accountant')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('PostingProfile', 'PSP', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
          ('2650', 'Store Credit Liability', 'Liability'),
          ('4400', 'Inventory Gain', 'Revenue'),
          ('5320', 'Inventory Shrinkage', 'Expense'),
          ('5330', 'Inventory Damage Write-off', 'Expense'),
          ('5340', 'Inventory Revaluation', 'Expense'),
          ('5350', 'Expired Stock Write-off', 'Expense'),
          ('5420', 'Job-Work Conversion Cost', 'Expense'),
          ('5430', 'Job-Work Loss', 'Expense'),
          ('5440', 'Samples / Non-returnable Challan Expense', 'Expense')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
