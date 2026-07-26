-- Stage 26.6.4: Journal Voucher (manual GL entry) + reversal + recurring
-- templates. Line items live inside the document's own JSON `data` blob
-- ("lines", a JSON array of {account_code, debit, credit}), the same
-- convention every other line-item transactional doctype in this system
-- already uses (e.g. PurchaseOrder/POSCart's "items") - no new child table.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('JournalVoucher', 'Finance', 'Transaction', 'finance')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('JournalVoucher', 'voucher_number', 'Voucher Number', 'Data', TRUE, NULL, 1),
('JournalVoucher', 'voucher_date', 'Voucher Date', 'Date', TRUE, NULL, 2),
('JournalVoucher', 'narration', 'Narration', 'Data', TRUE, NULL, 3),
('JournalVoucher', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Posted,Reversed,Recurring Template', 4),
('JournalVoucher', 'total_amount', 'Total Amount', 'Number', FALSE, NULL, 5),
('JournalVoucher', 'recurring_frequency', 'Recurring Frequency', 'Select', FALSE, 'Daily,Weekly,Monthly,Yearly', 6),
('JournalVoucher', 'next_run_date', 'Next Run Date', 'Date', FALSE, NULL, 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'JournalVoucher', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'JournalVoucher', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Every amount routes to HR/Admin - a manual GL entry is inherently the
-- kind of action this engine exists to gate, not slab-tiered like
-- PurchaseOrder's Store-Manager/HR-Admin split.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('JournalVoucher', 0, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;
