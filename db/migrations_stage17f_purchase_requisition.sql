-- Stage 17.7: Purchase Requisition - an approval-backed doctype that
-- converts, once Approved, into a Draft RFQ or PO (engines/procurement.go).
-- Reuses the existing generic SubmitForApproval/DecideApproval engine - no
-- new approval code needed, same pattern as ExpenseClaim (Stage 13.13c).
-- The 'PR' prefix_configs row already existed (db/migration.sql) as a
-- numbering-only stub with no doctype behind it until now.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('PurchaseRequisition', 'Procurement', 'Transaction', 'procurement')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PurchaseRequisition', 'code', 'Requisition Number', 'Data', TRUE, NULL, 1),
('PurchaseRequisition', 'description', 'Item / Requirement Description', 'Data', TRUE, NULL, 2),
('PurchaseRequisition', 'quantity', 'Quantity', 'Number', TRUE, NULL, 3),
('PurchaseRequisition', 'department', 'Department', 'Data', TRUE, NULL, 4),
('PurchaseRequisition', 'total_amount', 'Estimated Amount', 'Number', TRUE, NULL, 5),
('PurchaseRequisition', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Converted,Rejected', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PurchaseRequisition', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PurchaseRequisition', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('PurchaseRequisition', 0, 49999, 'Store Manager'),
('PurchaseRequisition', 50000, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;

-- RFQ never had a prefix_configs row (its "code" field was always
-- client-supplied) - needed now so "convert to RFQ" can generate a proper
-- sequence number the same way "convert to PO" does via the existing 'PO' row.
INSERT INTO tenant_default.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency)
VALUES ('RFQ', 'RFQ', '/', 6, 'ANNUAL')
ON CONFLICT (doc_type) DO NOTHING;
