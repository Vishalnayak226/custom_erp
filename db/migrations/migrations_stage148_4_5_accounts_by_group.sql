-- ---------------------------------------------------------------------------
-- Stage 148.4 / 148.5 (builder c): accounts by item / customer / vendor group,
-- and the chart of accounts maintained from the app.
--
-- 148.4: five more posting-profile events (sales invoice revenue and COGS, POS
-- sale revenue and COGS, goods receipt inventory) and three group columns on a
-- profile (item group = the Item's category, customer group, vendor group), plus
-- the customer_group / vendor_group fields on Customer / Vendor. With no
-- Approved profile every event posts to the account it always has.
--
-- 148.5: gl_accounts gets parent_code (report grouping), blocked and
-- no_manual_posting (all NULL / FALSE, so no account changes behaviour), and the
-- GLAccountChange doctype - an add / change / block / unblock request that goes
-- through the approval engine and is applied when approved.
--
-- Additive and idempotent, replayed into every tenant schema. No existing
-- account row, code or posting is changed. The event Select options of the
-- PostingProfile field are rewritten (metadata only).
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format('ALTER TABLE %I.gl_accounts ADD COLUMN IF NOT EXISTS parent_code VARCHAR(50)', schema_rec.schema_name);
      EXECUTE format('ALTER TABLE %I.gl_accounts ADD COLUMN IF NOT EXISTS blocked BOOLEAN NOT NULL DEFAULT FALSE', schema_rec.schema_name);
      EXECUTE format('ALTER TABLE %I.gl_accounts ADD COLUMN IF NOT EXISTS no_manual_posting BOOLEAN NOT NULL DEFAULT FALSE', schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    -- 148.4: the new events and group columns on a posting profile.
    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = 'advance_collected,advance_applied,advance_refunded,store_credit_issued,store_credit_redeemed,store_credit_expired,material_issue,material_return,adjustment_gain,adjustment_loss,adjustment_damage,adjustment_revaluation,adjustment_expiry,jobwork_charge,jobwork_loss,challan_nonreturnable_issue,sales_invoice_revenue,sales_invoice_cogs,pos_sale_revenue,pos_sale_cogs,grn_receipt'
       WHERE doctype_name = 'PostingProfile' AND fieldname = 'event'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('PostingProfile', 'item_group', 'Only for item group', 'Data', FALSE, NULL, 20),
        ('PostingProfile', 'customer_group', 'Only for customer group', 'Data', FALSE, NULL, 21),
        ('PostingProfile', 'vendor_group', 'Only for vendor group', 'Data', FALSE, NULL, 22),
        ('Customer', 'customer_group', 'Customer group', 'Data', FALSE, NULL, 80),
        ('Vendor', 'vendor_group', 'Vendor group', 'Data', FALSE, NULL, 80)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- 148.5: the chart-of-accounts change request.
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('GLAccountChange', 'Finance', 'finance', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('GLAccountChange', 'action', 'Action', 'Select', TRUE, 'Add,Change,Block,Unblock', 1),
        ('GLAccountChange', 'account_code', 'Account code', 'Data', TRUE, NULL, 2),
        ('GLAccountChange', 'account_name', 'Account name', 'Data', FALSE, NULL, 3),
        ('GLAccountChange', 'account_type', 'Type', 'Select', FALSE, ',Asset,Liability,Equity,Revenue,Expense', 4),
        ('GLAccountChange', 'parent_code', 'Parent account', 'Data', FALSE, NULL, 5),
        ('GLAccountChange', 'clear_parent', 'Remove parent', 'Select', FALSE, ',No,Yes', 6),
        ('GLAccountChange', 'no_manual_posting', 'No manual posting', 'Select', FALSE, ',No,Yes', 7),
        ('GLAccountChange', 'reason', 'Reason', 'Data', FALSE, NULL, 8),
        ('GLAccountChange', 'applied_at', 'Applied at (system)', 'Data', FALSE, NULL, 9),
        ('GLAccountChange', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 10)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'GLAccountChange', TRUE, TRUE, TRUE, FALSE),
        ('Accountant', 'GLAccountChange', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('GLAccountChange', 0, NULL, 'Accountant')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('GLAccountChange', 'GLC', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
