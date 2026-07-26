-- Stage 26.10.4 (Reports and BI Sprint): scheduled report delivery.
-- ScheduledReport is a plain registered doctype - same "engine writes the
-- system-managed fields directly, the generic doc API handles create/list/
-- delete" pattern ReportFilterPreset (Stage 20.36) and ReportExportJob
-- (Stage 20.37) already use, so no bespoke create/list/delete handler is
-- needed here (see internal/server/handlers_report_engine.go's own comment
-- on ReportFilterPreset for the precedent). Only next_run_date/last_run_at/
-- last_run_status are advanced by the worker (engines/scheduled_reports.go)
-- after each run - report_id/frequency/requested_role/recipient_email/
-- webhook_url/status are set by whoever creates the schedule.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('ScheduledReport', 'Reports', 'Master', 'reports')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ScheduledReport', 'report_id', 'Report', 'Data', TRUE, NULL, 1),
('ScheduledReport', 'params_json', 'Parameters (JSON, optional)', 'Data', FALSE, NULL, 2),
('ScheduledReport', 'frequency', 'Frequency', 'Select', TRUE, 'Daily,Weekly,Monthly', 3),
('ScheduledReport', 'requested_role', 'Run As Role', 'Select', TRUE, 'HR/Admin,Store Manager,Cashier', 4),
('ScheduledReport', 'recipient_email', 'Recipient Email', 'Data', FALSE, NULL, 5),
('ScheduledReport', 'webhook_url', 'Webhook URL', 'Data', FALSE, NULL, 6),
('ScheduledReport', 'next_run_date', 'Next Run Date', 'Data', TRUE, NULL, 7),
('ScheduledReport', 'last_run_at', 'Last Run At', 'Data', FALSE, NULL, 8),
('ScheduledReport', 'last_run_status', 'Last Run Status', 'Data', FALSE, NULL, 9),
('ScheduledReport', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ScheduledReport', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ScheduledReport', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
