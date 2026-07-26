-- Stage 26.6.6: Backdated-posting approval. A signed-off override path
-- through the existing maker-checker engine, instead of a blanket
-- period-lock rejection (engines/accounting_periods.go's
-- rejectIfCurrentPeriodClosed, now checks for a matching Approved row here
-- before rejecting). Flat-schema Transaction doctype - create/submit/
-- approve all reuse the existing generic doc/approval endpoints, no bespoke
-- Go code needed for the request itself (only the check it feeds).
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('BackdatedPostingRequest', 'Finance', 'Transaction', 'finance')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('BackdatedPostingRequest', 'target_doctype', 'Target Doctype', 'Data', TRUE, NULL, 1),
('BackdatedPostingRequest', 'target_document_id', 'Target Document ID', 'Data', TRUE, NULL, 2),
('BackdatedPostingRequest', 'transaction_date', 'Transaction Date', 'Date', TRUE, NULL, 3),
('BackdatedPostingRequest', 'reason', 'Reason', 'Data', TRUE, NULL, 4),
('BackdatedPostingRequest', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'BackdatedPostingRequest', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'BackdatedPostingRequest', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Every request routes to HR/Admin regardless of amount (extractAmount
-- finds none of its recognized field names on this doctype and returns 0 -
-- this single 0..NULL rule matches that unconditionally, so routing still
-- works correctly without adding a new field name to extractAmount's list).
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('BackdatedPostingRequest', 0, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;
