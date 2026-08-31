-- ---------------------------------------------------------------------------
-- Stage 37.4: Budgeting, cash-flow forecast, credit limits, dunning.
--
-- Pre-build audit found all four completely absent: no Budget concept, no
-- credit_limit anywhere, no due_date on either invoice doctype (ageing today
-- approximates off created_at), and GetCashFlowStatement is historical only.
-- Also found: SalesOrder/SalesInvoice have no real Customer Link - customer
-- identity flows as a free-text name throughout (SalesInvoice.customer is
-- literally SalesOrder's customer_name, per engines/order_invoice.go), the
-- same convention GetCustomerLedgerReport's own customerFilter already
-- relies on. Credit-limit checking in engines/budgeting.go therefore matches
-- BY NAME, inheriting that existing convention rather than introducing a
-- second, inconsistent identity model.
--
-- Five additive pieces, all optional/blank-safe so no existing tenant sees
-- any behaviour change until they opt in:
--   1. Customer.credit_limit
--   2. SalesInvoice.due_date / VendorInvoice.due_date
--   3. Budget doctype (pure generic-document doctype - Currency/ExchangeRate
--      precedent - no dedicated create/apply engine function needed)
--   4. SalesInvoice.dunning_last_notified_tier / _at (engine-managed)
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Customer', 'credit_limit', 'Credit Limit (blank = unlimited)', 'Number', FALSE, NULL, 10),
('SalesInvoice', 'due_date', 'Due Date', 'Date', FALSE, NULL, 90),
('SalesInvoice', 'dunning_last_notified_tier', 'Dunning Last Notified Tier (system-managed)', 'Data', FALSE, NULL, 91),
('SalesInvoice', 'dunning_last_notified_at', 'Dunning Last Notified At (system-managed)', 'Data', FALSE, NULL, 92),
('VendorInvoice', 'due_date', 'Due Date', 'Date', FALSE, NULL, 90)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('Budget', 'Finance', 'finance', 'Master')
ON CONFLICT (name) DO NOTHING;

-- 37.4.1: Budget is a pure generic-document doctype - like Currency/
-- ExchangeRate, it needs a business-rule validator (ValidateBudgetDocument,
-- engines/budgeting.go, joined at ValidateParityFoundationDocument) but no
-- dedicated create/apply engine function or endpoint. Never posted to the
-- GL - it is compared against gl_postings by GetBudgetVarianceReport, not
-- written into it, so an Approved budget never changes any existing report.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Budget', 'code', 'Budget Code', 'Data', TRUE, NULL, 1),
('Budget', 'cost_center', 'Cost Center (blank = whole company)', 'Link', FALSE, 'CostCenter', 2),
('Budget', 'account_code', 'GL Account', 'Data', TRUE, NULL, 3),
('Budget', 'period_start', 'Period Start', 'Date', TRUE, NULL, 4),
('Budget', 'period_end', 'Period End', 'Date', TRUE, NULL, 5),
('Budget', 'planned_amount', 'Planned Amount', 'Number', TRUE, NULL, 6),
('Budget', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Budget', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Budget', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Every amount routes to HR/Admin - a spending plan is the same class of
-- action JournalVoucher's own blanket rule already covers.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('Budget', 0, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- Stage 35-37 migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
  field_doctypes TEXT[] := ARRAY['Customer', 'SalesInvoice', 'VendorInvoice', 'Budget'];
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = 'Budget'
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module, module_key = EXCLUDED.module_key, document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name);

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
        FROM tenant_default.role_permissions WHERE doctype_name = 'Budget'
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read, allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update, allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
      SELECT doctype, min_amount, max_amount, required_role
        FROM tenant_default.approval_rules WHERE doctype = 'Budget'
      ON CONFLICT (doctype, min_amount) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
