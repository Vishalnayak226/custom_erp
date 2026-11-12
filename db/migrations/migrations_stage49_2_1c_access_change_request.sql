-- Stage 49.2.1 (joiner / mover / leaver): AccessChangeRequest rides the existing
-- maker-checker engine, behind the setting security.identity_change_approval
-- (Off by default). A joiner is created Inactive and activated on approval, a
-- mover's store change applies on approval, a leaver is already disabled and
-- the request is the second person's review of it. Additive; writes no client
-- data (seed rows for the doctype only), safe to run twice.

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('AccessChangeRequest', 'HR', 'hr', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('AccessChangeRequest', 'name', 'Request', 'Data', FALSE, NULL, 1),
('AccessChangeRequest', 'kind', 'Kind', 'Select', TRUE, 'Joiner,Mover,Leaver', 2),
('AccessChangeRequest', 'target_user_id', 'Target User', 'Link', TRUE, 'User', 3),
('AccessChangeRequest', 'target_username', 'Target Username', 'Data', TRUE, NULL, 4),
('AccessChangeRequest', 'detail', 'What changes', 'Data', FALSE, NULL, 5),
('AccessChangeRequest', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Failed', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'AccessChangeRequest', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('AccessChangeRequest', 0, NULL, 'Super Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;
