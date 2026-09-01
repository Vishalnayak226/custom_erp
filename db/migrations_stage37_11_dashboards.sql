-- Stage 37.11: role dashboards - savable layouts, scheduled digests, and
-- drill-through (drill-through is pure frontend reuse of the existing
-- /api/v1/reports/drilldown/:id endpoint, no schema change needed for it).
--
-- DashboardLayout mirrors the OMSSavedView precedent
-- (migrations_stage35_2_oms_console.sql): the engine writes these documents
-- directly (engines/dashboard.go's SaveDashboardLayout/ListDashboardLayouts/
-- DeleteDashboardLayout), so doctype_meta/doctype_fields exist for RBAC and
-- audit only, not for the generic doc-create API.
--
-- DashboardDigest mirrors the ScheduledReport precedent
-- (migrations_stage26_10_4_scheduled_reports.sql) instead: a plain
-- registered doctype the generic doc API already handles create/list/delete
-- for, with engines/dashboard.go's StartDashboardDigestWorker as the only
-- writer back to next_run_date/last_run_at/last_run_status. Both doctypes
-- use module_key 'reports', already registered and default_enabled in
-- public.modules (confirmed before writing this migration - see Stage
-- 37.9's module-registration gap for why that check now happens first).
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('DashboardLayout', 'Reports', 'reports', 'Master'),
('DashboardDigest', 'Reports', 'reports', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('DashboardLayout', 'code', 'Layout ID', 'Data', TRUE, NULL, 1),
('DashboardLayout', 'name', 'Layout Name', 'Data', TRUE, NULL, 2),
('DashboardLayout', 'owner', 'Owner', 'Data', TRUE, NULL, 3),
('DashboardLayout', 'role', 'Shared With Role', 'Select', FALSE, 'Super Admin,Store Manager,Cashier', 4),
('DashboardLayout', 'tiles_json', 'Tiles (JSON)', 'Data', TRUE, NULL, 5),
('DashboardDigest', 'dashboard_layout_id', 'Dashboard Layout', 'Data', TRUE, NULL, 1),
('DashboardDigest', 'frequency', 'Frequency', 'Select', TRUE, 'Daily,Weekly,Monthly', 2),
('DashboardDigest', 'requested_role', 'Run As Role', 'Select', TRUE, 'HR/Admin,Store Manager,Cashier', 3),
('DashboardDigest', 'recipient_email', 'Recipient Email', 'Data', FALSE, NULL, 4),
('DashboardDigest', 'webhook_url', 'Webhook URL', 'Data', FALSE, NULL, 5),
('DashboardDigest', 'next_run_date', 'Next Run Date', 'Data', TRUE, NULL, 6),
('DashboardDigest', 'last_run_at', 'Last Run At', 'Data', FALSE, NULL, 7),
('DashboardDigest', 'last_run_status', 'Last Run Status', 'Data', FALSE, NULL, 8),
('DashboardDigest', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin', 'DashboardLayout', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'DashboardLayout', TRUE, TRUE, TRUE, TRUE),
('Cashier', 'DashboardLayout', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'DashboardDigest', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'DashboardDigest', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Backfill every already-provisioned tenant schema (new tenants inherit
-- these via ProvisionTenantSchema cloning tenant_default).
DO $$
DECLARE
    schema_rec RECORD;
BEGIN
    FOR schema_rec IN
        SELECT schema_name FROM information_schema.schemata
        WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
    LOOP
        EXECUTE format('INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
            (''DashboardLayout'', ''Reports'', ''reports'', ''Master''),
            (''DashboardDigest'', ''Reports'', ''reports'', ''Master'')
            ON CONFLICT (name) DO NOTHING', schema_rec.schema_name);

        EXECUTE format('INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
            (''DashboardLayout'', ''code'', ''Layout ID'', ''Data'', TRUE, NULL, 1),
            (''DashboardLayout'', ''name'', ''Layout Name'', ''Data'', TRUE, NULL, 2),
            (''DashboardLayout'', ''owner'', ''Owner'', ''Data'', TRUE, NULL, 3),
            (''DashboardLayout'', ''role'', ''Shared With Role'', ''Select'', FALSE, ''Super Admin,Store Manager,Cashier'', 4),
            (''DashboardLayout'', ''tiles_json'', ''Tiles (JSON)'', ''Data'', TRUE, NULL, 5),
            (''DashboardDigest'', ''dashboard_layout_id'', ''Dashboard Layout'', ''Data'', TRUE, NULL, 1),
            (''DashboardDigest'', ''frequency'', ''Frequency'', ''Select'', TRUE, ''Daily,Weekly,Monthly'', 2),
            (''DashboardDigest'', ''requested_role'', ''Run As Role'', ''Select'', TRUE, ''HR/Admin,Store Manager,Cashier'', 3),
            (''DashboardDigest'', ''recipient_email'', ''Recipient Email'', ''Data'', FALSE, NULL, 4),
            (''DashboardDigest'', ''webhook_url'', ''Webhook URL'', ''Data'', FALSE, NULL, 5),
            (''DashboardDigest'', ''next_run_date'', ''Next Run Date'', ''Data'', TRUE, NULL, 6),
            (''DashboardDigest'', ''last_run_at'', ''Last Run At'', ''Data'', FALSE, NULL, 7),
            (''DashboardDigest'', ''last_run_status'', ''Last Run Status'', ''Data'', FALSE, NULL, 8),
            (''DashboardDigest'', ''status'', ''Status'', ''Select'', TRUE, ''Active,Inactive'', 9)
            ON CONFLICT (doctype_name, fieldname) DO NOTHING', schema_rec.schema_name);

        EXECUTE format('INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
            (''Super Admin'', ''DashboardLayout'', TRUE, TRUE, TRUE, TRUE),
            (''Store Manager'', ''DashboardLayout'', TRUE, TRUE, TRUE, TRUE),
            (''Cashier'', ''DashboardLayout'', TRUE, TRUE, TRUE, TRUE),
            (''HR/Admin'', ''DashboardDigest'', TRUE, TRUE, TRUE, TRUE),
            (''Store Manager'', ''DashboardDigest'', TRUE, TRUE, TRUE, FALSE)
            ON CONFLICT (role, doctype_name) DO NOTHING', schema_rec.schema_name);
    END LOOP;
END $$;
