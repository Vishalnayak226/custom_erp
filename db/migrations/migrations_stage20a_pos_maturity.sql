-- Stage 20 Track B.1: POS Maturity (20.6-20.10).
-- Additive-only, matches db/migrations_stage17h_location_masters.sql's pattern
-- for the two new doctypes; POSSession's own CRUD is deliberately NOT granted
-- to any role here (see engines/pos_session.go) - all writes go through the
-- dedicated /api/v1/pos/session/open and /close endpoints so the cash-variance
-- and cashier-identity logic can never be bypassed via the generic doc API,
-- unlike the existing POSCart create/update grants below it in migration.sql.

-- 20.6: POSProfile master - per-location POS configuration. Pure generic
-- Master-doctype registration (doctype_meta/fields/permissions only, no new
-- backend code) - same pattern as Location/LegalEntity/Department/CostCenter.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('POSProfile', 'Sales', 'Master', 'sales')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('POSProfile', 'profile_name', 'Profile Name', 'Data', TRUE, NULL, 1),
('POSProfile', 'location', 'Location', 'Link', TRUE, 'Location', 2),
('POSProfile', 'default_payment_mode', 'Default Payment Mode', 'Select', TRUE, 'Cash,Card,UPI', 3),
('POSProfile', 'invoice_series', 'Invoice Number Series', 'Data', FALSE, NULL, 4),
('POSProfile', 'opening_cash_float', 'Default Opening Cash Float', 'Number', FALSE, NULL, 5),
('POSProfile', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'POSProfile', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'POSProfile', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 20.7/20.8: POSSession - cashier session lifecycle (Open -> Closed) with
-- cash-variance capture on close. Registered for read-only browsing via the
-- generic doc list (history/audit) - creation and closing are handler-only.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('POSSession', 'Sales', 'Transaction', 'sales')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('POSSession', 'pos_profile', 'POS Profile', 'Link', TRUE, 'POSProfile', 1),
('POSSession', 'location', 'Location', 'Data', TRUE, NULL, 2),
('POSSession', 'cashier', 'Cashier', 'Data', TRUE, NULL, 3),
('POSSession', 'opening_cash', 'Opening Cash', 'Number', TRUE, NULL, 4),
('POSSession', 'expected_cash', 'Expected Cash (Cash-mode sales)', 'Number', FALSE, NULL, 5),
('POSSession', 'closing_counted_cash', 'Closing Counted Cash', 'Number', FALSE, NULL, 6),
('POSSession', 'variance', 'Variance (Counted - Expected)', 'Number', FALSE, NULL, 7),
('POSSession', 'status', 'Status', 'Select', TRUE, 'Open,Closed', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'POSSession', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'POSSession', TRUE, FALSE, FALSE, FALSE),
('Cashier', 'POSSession', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 20.9: payment-mode-aware GL clearing accounts. 1100 stays the Cash account
-- (existing postings and reports already assume it); Card/UPI sales now post
-- to their own clearing account instead of being silently lumped into Cash.
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('1101', 'Card Clearing Account', 'Asset'),
('1102', 'UPI Clearing Account', 'Asset')
ON CONFLICT (account_code) DO NOTHING;

-- 20.10: discount-approval gate, reusing the existing approval_rules engine
-- (engines/approval.go) rather than a new mechanism. "amount" here is the
-- cart's discount percentage (e.g. 10 = 10%), not a rupee amount - extractAmount()
-- in engines/approval.go is extended to also read a "discount_amount" key for
-- this purpose. Default slab: 10%+ needs a Store Manager (or HR/Admin); admins
-- can retune via the existing GET/POST /api/v1/approval/rules screen.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('POSCart', 10, NULL, 'Store Manager')
ON CONFLICT (doctype, min_amount) DO NOTHING;
