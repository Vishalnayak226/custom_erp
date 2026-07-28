-- Stage 28.3: ReportColumnProfile - a named, saveable column layout (which
-- columns show, in what order) for a report, in two scopes:
--   * Personal  - visible only to its owner (filtered client-side by owner)
--   * Universal - shared with everyone; creatable only by privileged roles,
--                 enforced client-side AND server-side (handlers_core_doc_engine.go).
--
-- Mirrors ReportFilterPreset (migrations_stage20d_reports_engine.sql) exactly:
-- a generic Master doctype driven through the standard /api/v1/doc API, no
-- dedicated handler. doctype_meta/doctype_fields/role_permissions are all in
-- ProvisionTenantSchema's seed list, so new tenants inherit these rows.

INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('ReportColumnProfile', 'Reports', 'Master', 'reports')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ReportColumnProfile', 'report_id', 'Report', 'Data', TRUE, NULL, 1),
('ReportColumnProfile', 'name', 'Profile Name', 'Data', TRUE, NULL, 2),
('ReportColumnProfile', 'columns', 'Columns (JSON)', 'Data', TRUE, NULL, 3),
('ReportColumnProfile', 'scope', 'Scope', 'Select', TRUE, 'Personal,Universal', 4),
('ReportColumnProfile', 'owner', 'Owner Username', 'Data', TRUE, NULL, 5),
('ReportColumnProfile', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ReportColumnProfile', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ReportColumnProfile', TRUE, TRUE, TRUE, TRUE),
('Cashier', 'ReportColumnProfile', TRUE, TRUE, TRUE, TRUE)
ON CONFLICT (role, doctype_name) DO NOTHING;
