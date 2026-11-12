-- ---------------------------------------------------------------------------
-- Stage 36.3: PIM import depth - scheduled, drop-directory, templates,
-- an API hook, variant-aware, preview.
--
-- Two doctypes, in dependency order:
--   PIMImportTemplate (Master) - a reusable column mapping (a supplier's own
--                                 header names -> this doctype's own field
--                                 names, each column optionally run through a
--                                 Stage 36.5 transform rule).
--   PIMImportSchedule (Master) - a template driven either by a periodic scan
--                                 of a configured directory, or by an inbound
--                                 webhook token minted for it.
--
-- variant-aware (36.3.5) and preview (36.3.6) are engine behaviour
-- (engines/pim_import_template.go), not schema - no new fields for them.
--
-- Everything here is additive. Nothing alters an existing doctype, column or
-- row, so a tenant that never opens the Import Templates/Schedules screens is
-- byte-identical to what it is today.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PIMImportTemplate', 'PIM', 'pim', 'Master'),
('PIMImportSchedule', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.3.3 - PIMImportTemplate.
--
-- column_mappings is a JSONTable for the same reason PIMWorkflowDefinition's
-- stages and PIMTransformRule's steps are: only ever read as part of their
-- parent template, never queried across templates. target_field is checked
-- against the target doctype's real fields, and transform_rule against a
-- real Active PIMTransformRule, by ValidatePIMImportTemplateDocument
-- (engines/pim_import_template.go) at save time.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMImportTemplate', 'code', 'Template Code', 'Data', TRUE, NULL, 1),
('PIMImportTemplate', 'name', 'Template Name', 'Data', TRUE, NULL, 2),
('PIMImportTemplate', 'target_doctype', 'Target Doctype', 'Data', TRUE, NULL, 3),
('PIMImportTemplate', 'description', 'Description', 'Data', FALSE, NULL, 4),
('PIMImportTemplate', 'column_mappings', 'Column Mappings', 'JSONTable', TRUE,
 '[{"key":"source_column","label":"Source Column","type":"text","required":true},
   {"key":"target_field","label":"Target Field","type":"text","required":true},
   {"key":"transform_rule","label":"Transform Rule","type":"link","link":"PIMTransformRule"},
   {"key":"default_value","label":"Default Value","type":"text"}]', 5),
('PIMImportTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.3.1 / 36.3.2 / 36.3.4 - PIMImportSchedule.
--
-- source_type picks between the two delivery mechanisms this stage actually
-- builds: a directory the worker scans (which also transparently serves an
-- SFTP source ops mounts as a filesystem path - see the micro_checklist.md
-- entry for why a native SFTP client is deliberately out of scope) or an
-- inbound webhook token. frequency/next_run_date reuse ScheduledReport's own
-- shape (Stage 26.10.4, engines/scheduled_reports.go's advanceScheduleDate)
-- rather than a second date-advancement scheme, and only apply to a Drop
-- Directory schedule - an API Hook schedule is purely reactive.
--
-- hook_token_hash/last_run_at/last_run_status are engine-managed, the same
-- posture ScheduledReport already gives next_run_date/last_run_at/
-- last_run_status: not specially hardened against a trusted role's own raw
-- API writes (RBAC on this doctype already gates who can touch a schedule
-- at all), just conventionally left off the authoring form. The token
-- itself is never stored - only its SHA-256 digest, minted and shown once
-- by POST /api/v1/pim/import-schedules/{id}/rotate-hook-token
-- (engines/pim_import_schedule.go), the same crypto/rand + digest-only
-- shape Stage 38.2a's API keys use.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMImportSchedule', 'code', 'Schedule Code', 'Data', TRUE, NULL, 1),
('PIMImportSchedule', 'name', 'Schedule Name', 'Data', TRUE, NULL, 2),
('PIMImportSchedule', 'template', 'Import Template', 'Link', TRUE, 'PIMImportTemplate', 3),
('PIMImportSchedule', 'source_type', 'Source Type', 'Select', TRUE, 'Drop Directory,API Hook', 4),
('PIMImportSchedule', 'source_path', 'Drop Directory Path', 'Data', FALSE, NULL, 5),
('PIMImportSchedule', 'frequency', 'Frequency', 'Select', FALSE, 'Daily,Weekly,Monthly', 6),
('PIMImportSchedule', 'next_run_date', 'Next Run Date', 'Data', FALSE, NULL, 7),
('PIMImportSchedule', 'hook_token_hash', 'Hook Token Hash (system-managed)', 'Data', FALSE, NULL, 8),
('PIMImportSchedule', 'last_run_at', 'Last Run At', 'Data', FALSE, NULL, 9),
('PIMImportSchedule', 'last_run_status', 'Last Run Status', 'Data', FALSE, NULL, 10),
('PIMImportSchedule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Permissions - same split every PIM authoring master gets (36.2's
-- PIMWorkflowDefinition precedent): Super Admin full control, Store Manager
-- can author and use but not delete one out from under other work that
-- references it. 'Super Admin', not 'HR/Admin' - stage40_3 renamed that role
-- away on an already-migrated database.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'PIMImportTemplate', TRUE, TRUE, TRUE, TRUE),
('Super Admin',   'PIMImportSchedule', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PIMImportTemplate', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'PIMImportSchedule', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO UPDATE SET
    allow_read   = EXCLUDED.allow_read,
    allow_create = EXCLUDED.allow_create,
    allow_update = EXCLUDED.allow_update,
    allow_delete = EXCLUDED.allow_delete;

-- ---------------------------------------------------------------------------
-- A template/schedule code is quoted by name (a schedule names its
-- template; an operator names a schedule when troubleshooting), so each
-- must resolve to exactly one row. The hook-token lookup is the one hot
-- read the API hook handler makes on every call, keyed on the digest, not
-- the schedule's own id/code.
-- ---------------------------------------------------------------------------
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_import_template_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'PIMImportTemplate' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_import_schedule_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'PIMImportSchedule' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_pim_import_schedule_hook_token
    ON tenant_default.documents ((data->>'hook_token_hash'))
    WHERE doctype = 'PIMImportSchedule' AND deleted_at IS NULL AND status = 'Active';

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- PIM-stage migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
  doctype_list TEXT[] := ARRAY['PIMImportTemplate', 'PIMImportSchedule'];
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = ANY($1)
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module,
        module_key = EXCLUDED.module_key,
        document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name) USING doctype_list;

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = ANY($1)
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label,
        fieldtype = EXCLUDED.fieldtype,
        mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options,
        display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name) USING doctype_list;

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = ANY($1)
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read,
        allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update,
        allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name) USING doctype_list;

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_import_template_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'PIMImportTemplate' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_import_schedule_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'PIMImportSchedule' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE INDEX IF NOT EXISTS idx_documents_pim_import_schedule_hook_token
        ON %I.documents ((data->>'hook_token_hash'))
       WHERE doctype = 'PIMImportSchedule' AND deleted_at IS NULL AND status = 'Active'
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
