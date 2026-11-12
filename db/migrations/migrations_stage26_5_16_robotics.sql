-- Stage 26.5.16 (WMS Enterprise Maturity Sprint P2 follow-up): robotics/
-- conveyor/scale inbound API integration. Go-ahead given 2026-07-27 for
-- all five P2 bundles previously deferred pending a real warehouse-scale
-- pilot - built generic (no vendor-specific SDK, since no specific vendor
-- is contracted), same "per-tenant shared API key" auth shape 26.7.11's
-- CleverTap segment-sync webhook already uses, but as its own doctype
-- rather than reusing clevertap_credentials (a genuinely different
-- integration, not CleverTap-branded).
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('RoboticsIntegrationCredential', 'Inventory', 'Master', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('RoboticsIntegrationCredential', 'code', 'Label', 'Data', TRUE, NULL, 1),
('RoboticsIntegrationCredential', 'api_key', 'API Key', 'Data', TRUE, NULL, 2),
('RoboticsIntegrationCredential', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'RoboticsIntegrationCredential', TRUE, TRUE, TRUE, TRUE)
ON CONFLICT (role, doctype_name) DO NOTHING;
