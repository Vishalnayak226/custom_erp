-- ---------------------------------------------------------------------------
-- Stage 37.6: Deferred revenue, prepaid amortisation, recurring billing,
-- price-list versioning.
--
-- Pre-build audit found all four completely absent, plus one structural
-- finding: SalesInvoice is lump-sum only (no lines/JSONTable field, per
-- Stage 37.4's own audit of engines/order_invoice.go) - deferred revenue
-- here is therefore recognised at the WHOLE-INVOICE level, not per line.
--
-- Four additive pieces:
--   1. SalesInvoice.deferred_revenue/deferred_term_months + DeferredRevenueSchedule
--      (system-created the moment a flagged invoice posts - see
--      engines/deferred_prepaid.go's PostSalesInvoice hook).
--   2. PrepaidExpenseSchedule - the mirror on the expense side, its own
--      Draft->Approved lifecycle since it is an independent decision (not
--      derived from an already-approved posting the way #1 is).
--   3. RecurringSalesContract - the JournalVoucher recurring-template shape
--      (Stage 26.6.4's CreateRecurringJournalTemplate), spawning a Draft
--      SalesInvoice instead of a Draft JournalVoucher. Each spawned invoice
--      still goes through PostSalesInvoice's own credit-limit gate (37.4.2)
--      when a human chooses to post it - the contract itself needs no
--      separate approval workflow, mirroring how a JournalVoucher recurring
--      template needs none either.
--   4. PriceListVersion - ExchangeRate's own effective-dated-row pattern
--      (Stage 37.1.1), not the ProductContent snapshot-table pattern: each
--      version is its own full, immutable-once-superseded document, resolved
--      by date exactly the way ResolveExchangeRate already resolves rates.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('2600', 'Deferred Revenue', 'Liability'),
('1800', 'Prepaid Expense', 'Asset')
ON CONFLICT (account_code) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SalesInvoice', 'deferred_revenue', 'Deferred Revenue', 'Select', FALSE, 'No,Yes', 93),
('SalesInvoice', 'deferred_term_months', 'Deferred Term (months)', 'Number', FALSE, NULL, 94)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('DeferredRevenueSchedule', 'Finance', 'finance', 'Transaction'),
('PrepaidExpenseSchedule', 'Finance', 'finance', 'Transaction'),
('RecurringSalesContract', 'Sales', 'sales', 'Master'),
('PriceListVersion', 'Sales', 'sales', 'Master')
ON CONFLICT (name) DO NOTHING;

-- 37.6.1: system-created (Active directly, no maker-checker of its own - the
-- decision was already made when the invoice was flagged deferred and
-- posted). recognized_months/next_recognition_date are engine-managed.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('DeferredRevenueSchedule', 'code', 'Schedule Code', 'Data', TRUE, NULL, 1),
('DeferredRevenueSchedule', 'sales_invoice_id', 'Sales Invoice', 'Link', TRUE, 'SalesInvoice', 2),
('DeferredRevenueSchedule', 'total_amount', 'Total Amount', 'Number', TRUE, NULL, 3),
('DeferredRevenueSchedule', 'term_months', 'Term (months)', 'Number', TRUE, NULL, 4),
('DeferredRevenueSchedule', 'recognized_months', 'Recognized Months (system-managed)', 'Number', FALSE, NULL, 5),
('DeferredRevenueSchedule', 'next_recognition_date', 'Next Recognition Date (system-managed)', 'Data', FALSE, NULL, 6),
('DeferredRevenueSchedule', 'status', 'Status', 'Select', TRUE, 'Active,Completed', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 37.6.2: an independent decision (a prepaid payment made outside the GRN/
-- VendorInvoice 3-way-match flow, e.g. annual insurance/rent), so it keeps
-- its own Draft -> Pending Approval -> Approved lifecycle.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PrepaidExpenseSchedule', 'code', 'Schedule Code', 'Data', TRUE, NULL, 1),
('PrepaidExpenseSchedule', 'description', 'Description', 'Data', TRUE, NULL, 2),
('PrepaidExpenseSchedule', 'total_amount', 'Total Amount', 'Number', TRUE, NULL, 3),
('PrepaidExpenseSchedule', 'expense_account_code', 'Expense Account', 'Data', TRUE, NULL, 4),
('PrepaidExpenseSchedule', 'term_months', 'Term (months)', 'Number', TRUE, NULL, 5),
('PrepaidExpenseSchedule', 'recognized_months', 'Recognized Months (system-managed)', 'Number', FALSE, NULL, 6),
('PrepaidExpenseSchedule', 'next_recognition_date', 'Next Recognition Date (system-managed)', 'Data', FALSE, NULL, 7),
('PrepaidExpenseSchedule', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Completed', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 37.6.3
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('RecurringSalesContract', 'code', 'Contract Code', 'Data', TRUE, NULL, 1),
('RecurringSalesContract', 'customer', 'Customer', 'Data', TRUE, NULL, 2),
('RecurringSalesContract', 'description', 'Description', 'Data', TRUE, NULL, 3),
('RecurringSalesContract', 'amount', 'Amount Per Cycle', 'Number', TRUE, NULL, 4),
('RecurringSalesContract', 'billing_frequency', 'Billing Frequency', 'Select', TRUE, 'Monthly,Quarterly,Yearly', 5),
('RecurringSalesContract', 'next_billing_date', 'Next Billing Date', 'Date', TRUE, NULL, 6),
('RecurringSalesContract', 'status', 'Status', 'Select', TRUE, 'Active,Paused,Cancelled', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 37.6.4: items is a JSONTable, the same "only ever read as part of its
-- parent document" convention every other line-item JSONTable already uses.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PriceListVersion', 'code', 'Version Code', 'Data', TRUE, NULL, 1),
('PriceListVersion', 'price_list_code', 'Price List', 'Data', TRUE, NULL, 2),
('PriceListVersion', 'currency', 'Currency', 'Link', FALSE, 'Currency', 3),
('PriceListVersion', 'effective_from', 'Effective From', 'Date', TRUE, NULL, 4),
('PriceListVersion', 'effective_to', 'Effective To (blank = open-ended)', 'Date', FALSE, NULL, 5),
('PriceListVersion', 'items', 'Prices', 'JSONTable', TRUE,
 '[{"key":"sku","label":"SKU","type":"text","required":true},
   {"key":"price","label":"Price","type":"number","required":true}]', 6),
('PriceListVersion', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Superseded', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'DeferredRevenueSchedule', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'DeferredRevenueSchedule', TRUE, FALSE, FALSE, FALSE),
('HR/Admin', 'PrepaidExpenseSchedule', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PrepaidExpenseSchedule', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'RecurringSalesContract', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'RecurringSalesContract', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'PriceListVersion', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PriceListVersion', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('PrepaidExpenseSchedule', 0, NULL, 'HR/Admin'),
('PriceListVersion', 0, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- Stage 35-37 migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
  field_doctypes TEXT[] := ARRAY['SalesInvoice', 'DeferredRevenueSchedule', 'PrepaidExpenseSchedule', 'RecurringSalesContract', 'PriceListVersion'];
  meta_doctypes TEXT[] := ARRAY['DeferredRevenueSchedule', 'PrepaidExpenseSchedule', 'RecurringSalesContract', 'PriceListVersion'];
  perm_doctypes TEXT[] := ARRAY['DeferredRevenueSchedule', 'PrepaidExpenseSchedule', 'RecurringSalesContract', 'PriceListVersion'];
  rule_doctypes TEXT[] := ARRAY['PrepaidExpenseSchedule', 'PriceListVersion'];
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES '
      '(''2600'', ''Deferred Revenue'', ''Liability''), '
      '(''1800'', ''Prepaid Expense'', ''Asset'') '
      'ON CONFLICT (account_code) DO NOTHING',
      schema_rec.schema_name
    );

    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = ANY($1)
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module, module_key = EXCLUDED.module_key, document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name) USING meta_doctypes;

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = ANY($1)
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label, fieldtype = EXCLUDED.fieldtype, mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options, display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name) USING field_doctypes;

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = ANY($1)
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read, allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update, allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name) USING perm_doctypes;

    EXECUTE format($f$
      INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
      SELECT doctype, min_amount, max_amount, required_role
        FROM tenant_default.approval_rules WHERE doctype = ANY($1)
      ON CONFLICT (doctype, min_amount) DO NOTHING
    $f$, schema_rec.schema_name) USING rule_doctypes;
  END LOOP;
END $$;
