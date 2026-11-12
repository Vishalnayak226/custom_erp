-- Stage 29.6: reusable requirement-description master for Purchase
-- Requisitions. Department already exists as the Core Department master
-- (Stage 17.9), so this adds only the genuinely missing catalogue.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('PurchaseRequisitionDescription', 'Procurement', 'Master', 'procurement')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PurchaseRequisitionDescription', 'code', 'Description Code', 'Data', TRUE, NULL, 1),
('PurchaseRequisitionDescription', 'description', 'Requirement Description', 'Data', TRUE, NULL, 2),
('PurchaseRequisitionDescription', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Descriptions are created by the requisition save engine, not manually by a
-- requester. Read access lets requesters receive autosuggestions; HR/Admin
-- can still maintain entries from the generic Master screen if needed.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PurchaseRequisitionDescription', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PurchaseRequisitionDescription', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
