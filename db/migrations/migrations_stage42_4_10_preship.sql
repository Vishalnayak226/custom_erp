-- ---------------------------------------------------------------------------
-- Stage 42.4.10 - Pre-ship validation rules (Infor Sec17-style): the final
-- gate before a LoadingTask can move Loading -> Loaded (engines/
-- wms_preship.go's EvaluatePreShipGate, called from CompleteLoadingTask).
-- A tenant with no Active PreShipValidationRule for a location gets today's
-- behaviour unchanged (CompleteLoadingTask only checks the carton count) -
-- the same "inert until configured" posture every gate this Stage added
-- (ReceiptValidationRule, LottableConstraint, hold) already has.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PreShipValidationRule', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PreShipValidationRule', 'code', 'Rule Code', 'Data', TRUE, NULL, 1),
('PreShipValidationRule', 'location_code', 'Location (optional - blank applies everywhere)', 'Link', FALSE, 'Location', 2),
('PreShipValidationRule', 'require_all_cartons_scanned', 'Require All Cartons Scanned', 'Select', TRUE, 'Yes,No', 3),
('PreShipValidationRule', 'require_hold_free', 'Require Hold-Free Order', 'Select', TRUE, 'Yes,No', 4),
('PreShipValidationRule', 'require_documents_present', 'Require Label + Invoice Present', 'Select', TRUE, 'Yes,No', 5),
('PreShipValidationRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'PreShipValidationRule', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'PreShipValidationRule', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'PreShipValidationRule', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
