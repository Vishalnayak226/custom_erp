-- Stage 26.10.7 (Reports/BI Sprint P2 follow-up): report query-load
-- instrumentation. This is the prerequisite 26.10.6 (dedicated BI data
-- mart/read replica) itself was deferred pending - "only justified once
-- real report-query load is measured against the live Postgres instance" -
-- so this is the measurement mechanism, not the mart itself. ReportRunLog
-- is a plain system-written log doctype (same "engine writes directly into
-- documents, no bespoke table" precedent StockLedgerEntry set - see that
-- doctype's own migrations_phase3.sql registration) - no role gets
-- create/update/delete via the generic API since engines/report_registry.go
-- writes it directly with a raw INSERT, matching WriteStockLedgerEntry's
-- own direct-SQL-insert shape.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('ReportRunLog', 'Reports', 'Transaction', 'reports')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ReportRunLog', 'report_id', 'Report', 'Data', TRUE, NULL, 1),
('ReportRunLog', 'duration_ms', 'Duration (ms)', 'Number', TRUE, NULL, 2),
('ReportRunLog', 'row_count', 'Row Count', 'Number', TRUE, NULL, 3),
('ReportRunLog', 'user_id', 'Run By', 'Data', FALSE, NULL, 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ReportRunLog', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
