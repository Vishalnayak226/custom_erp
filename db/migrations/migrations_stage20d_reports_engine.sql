-- Stage 20 Track B.4: Reports Engine (20.35-20.40).
-- The report-builder framework itself (20.35, engines/report_registry.go +
-- report_definitions.go) is Go-defined, not database-driven - this
-- migration only adds the two doctypes the framework's UI-facing features
-- (20.36 saved filters, 20.37 async export) need to persist state, same
-- generic Master/Transaction-doctype pattern as every other Stage 20 track.

-- 20.36: ReportFilterPreset - a named, reusable set of report params per
-- user. Ownership is enforced the same way as everywhere else in this
-- codebase (created_by), not a new permission concept - any authenticated
-- user with read+create can see/save presets, the frontend only ever shows
-- a user their own (see public/app.js's report catalog panel).
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('ReportFilterPreset', 'Reports', 'Master', 'reports')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ReportFilterPreset', 'report_id', 'Report', 'Data', TRUE, NULL, 1),
('ReportFilterPreset', 'name', 'Preset Name', 'Data', TRUE, NULL, 2),
('ReportFilterPreset', 'params', 'Saved Parameters (JSON)', 'Data', TRUE, NULL, 3),
('ReportFilterPreset', 'owner', 'Owner Username', 'Data', TRUE, NULL, 4),
('ReportFilterPreset', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ReportFilterPreset', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ReportFilterPreset', TRUE, TRUE, TRUE, TRUE),
('Cashier', 'ReportFilterPreset', TRUE, TRUE, TRUE, TRUE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 20.37: ReportExportJob - queued-then-background-run heavy report export,
-- reusing the existing outbox/worker-polling pattern (engines/outbox.go)
-- rather than a new job-queue dependency; the generated CSV is stored
-- directly in the job's own JSONB document, same trick Stage 15.2's
-- ImportJob.error_csv already uses (no new file-storage mechanism).
-- Registered read-only, like Stage 20.7's POSSession and Stage 20.27's
-- PaymentProposal - every write goes through the dedicated
-- POST /api/v1/reports/export endpoint and the background worker, never
-- the generic doc API, so requested_role/status/csv can never be
-- client-forged.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('ReportExportJob', 'Reports', 'Transaction', 'reports')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ReportExportJob', 'report_id', 'Report', 'Data', TRUE, NULL, 1),
('ReportExportJob', 'requested_role', 'Requested By Role', 'Data', FALSE, NULL, 2),
('ReportExportJob', 'params', 'Parameters (JSON)', 'Data', FALSE, NULL, 3),
('ReportExportJob', 'status', 'Status', 'Select', TRUE, 'Pending,Completed,Failed', 4),
('ReportExportJob', 'csv', 'Generated CSV', 'Data', FALSE, NULL, 5),
('ReportExportJob', 'error', 'Error (if Failed)', 'Data', FALSE, NULL, 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ReportExportJob', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'ReportExportJob', TRUE, FALSE, FALSE, FALSE),
('Cashier', 'ReportExportJob', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
