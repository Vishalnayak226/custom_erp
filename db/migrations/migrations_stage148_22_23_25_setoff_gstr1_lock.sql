-- ---------------------------------------------------------------------------
-- Stage 148.22 / 148.23 / 148.25.
--
-- 148.25: gstr1_filed_months (one row per month marked GSTR-1 filed; a row with
-- unmarked_at set is not a lock) and a third accounting-period status, On Hold.
-- The lock itself is the setting finance.lock_after_gstr1, off by default; with
-- no row and no On Hold period nothing changes.
--
-- 148.23: PartyPair (a customer paired with a vendor) and SetOffVoucher (the
-- set-off between them, posted when approved) with their approval rule. No
-- account row is written: the clearing account is the setting
-- finance.setoff_clearing_account, picked by an admin.
--
-- 148.22 needs no schema: income_account rides on the invoice line's JSON.
--
-- Additive and idempotent, replayed into every tenant schema. Writes no
-- per-client business rows (doctype metadata and one approval rule only).
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'accounting_periods') THEN
      EXECUTE format('ALTER TABLE %I.accounting_periods DROP CONSTRAINT IF EXISTS accounting_periods_status_check', schema_rec.schema_name);
      EXECUTE format($sql$
        ALTER TABLE %I.accounting_periods ADD CONSTRAINT accounting_periods_status_check
          CHECK (status IN ('Open', 'On Hold', 'Closed'))
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        CREATE TABLE IF NOT EXISTS %1$I.gstr1_filed_months (
            month_start DATE PRIMARY KEY,
            filed_by VARCHAR(150) NOT NULL,
            filed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
            unmarked_by VARCHAR(150),
            unmarked_at TIMESTAMPTZ,
            unmark_reason TEXT)
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('PartyPair', 'Finance', 'finance', 'Master'),
             ('SetOffVoucher', 'Finance', 'finance', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('PartyPair', 'customer_id', 'Customer', 'Link', TRUE, 'Customer', 1),
        ('PartyPair', 'vendor_id', 'Vendor', 'Link', TRUE, 'Vendor', 2),
        ('PartyPair', 'basis', 'Matched on', 'Data', FALSE, NULL, 3),
        ('PartyPair', 'status', 'Status', 'Select', TRUE, 'Active,Ended', 4),
        ('SetOffVoucher', 'pair_id', 'Pair', 'Link', TRUE, 'PartyPair', 1),
        ('SetOffVoucher', 'amount', 'Amount', 'Number', TRUE, NULL, 2),
        ('SetOffVoucher', 'voucher_date', 'Date', 'Date', FALSE, NULL, 3),
        ('SetOffVoucher', 'reversal_date', 'Reverse on', 'Date', FALSE, NULL, 4),
        ('SetOffVoucher', 'customer_id', 'Customer (system)', 'Data', FALSE, NULL, 5),
        ('SetOffVoucher', 'vendor_id', 'Vendor (system)', 'Data', FALSE, NULL, 6),
        ('SetOffVoucher', 'customer_balance', 'Customer owes', 'Number', FALSE, NULL, 7),
        ('SetOffVoucher', 'vendor_balance', 'We owe vendor', 'Number', FALSE, NULL, 8),
        ('SetOffVoucher', 'posted_at', 'Posted at (system)', 'Data', FALSE, NULL, 9),
        ('SetOffVoucher', 'reversed_on', 'Reversed on (system)', 'Data', FALSE, NULL, 10),
        ('SetOffVoucher', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Reversed', 11)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'PartyPair', TRUE, TRUE, TRUE, FALSE),
        ('Accountant', 'PartyPair', TRUE, TRUE, TRUE, FALSE),
        ('HR/Admin', 'SetOffVoucher', TRUE, TRUE, TRUE, FALSE),
        ('Accountant', 'SetOffVoucher', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        VALUES ('SetOffVoucher', 0, NULL, 'Accountant')
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('SetOffVoucher', 'SOV', '-', 4, 'ANNUAL', FALSE),
               ('PartyPair', 'PAIR', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
