-- ---------------------------------------------------------------------------
-- Stage 42.3.6 - Configurable receipt validation rules (Infor Sec 17).
--
-- validateGRNRules (engines/transactional_validation.go) already hard-blocks
-- the two things Infor's Sec 17 profile makes tenant-configurable:
-- PURCHA-0087 (any qty over the open PO qty) and PURCHA-0088 (any SKU not on
-- the PO), both at an implicit 0% tolerance / zero-exceptions default. This
-- doctype lets a tenant loosen either, scoped to a vendor or left blank for
-- a tenant-wide default - resolved by getReceiptValidationRule, vendor-
-- specific row winning over the blank/default row when both exist. No rule
-- configured (every tenant before this Stage) reproduces today's behavior
-- exactly: 0% tolerance, unexpected items blocked - so this is purely
-- additive, never a behavior change for an existing tenant that never
-- touches the new master.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ReceiptValidationRule', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ReceiptValidationRule', 'vendor', 'Vendor (optional, blank = tenant default)', 'Link', FALSE, 'Vendor', 1),
('ReceiptValidationRule', 'over_receipt_tolerance_pct', 'Over-Receipt Tolerance % (optional, default 0)', 'Number', FALSE, NULL, 2),
('ReceiptValidationRule', 'allow_unexpected_items', 'Allow Items Not On The PO', 'Select', TRUE, 'Yes,No', 3),
('ReceiptValidationRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'ReceiptValidationRule', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'ReceiptValidationRule', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'ReceiptValidationRule', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
