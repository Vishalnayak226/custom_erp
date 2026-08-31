-- ---------------------------------------------------------------------------
-- Stage 36.4: PIM export & syndication depth - custom export templates,
-- scheduled delivery, parent/variant-collapsed shapes, shareable Catalogs,
-- bulk channel download.
--
-- Three doctypes, in dependency order:
--   PIMExportTemplate (Master) - chosen/ordered/labelled columns, a
--                                 headerless mode, and a variant-collapse
--                                 mode. Blank `channel` exports raw ERP
--                                 fields (the same shape fetchSearchFeedRows
--                                 already reads); a set `channel` exports
--                                 through BuildChannelPayload so the columns
--                                 on offer are that channel's own
--                                 ChannelFieldMap target fields.
--   PIMExportSchedule (Master) - a template delivered on a Daily/Weekly/
--                                 Monthly cadence via the existing outbox
--                                 (email/webhook are simulated dispatch, the
--                                 same posture ScheduledReport already takes
--                                 - see engines/scheduled_reports.go).
--   PIMCatalog (Master) - a product group exposed as a shareable, tokenised,
--                          read-only link. Re-resolves the group's live
--                          membership on every view (ResolvePIMProductGroup),
--                          so the catalogue is real-time, not a point-in-time
--                          export. share_token_hash is digest-only, minted
--                          by POST /api/v1/pim/catalogs/{id}/rotate-share-token
--                          - the same crypto/rand + SHA-256 shape Stage
--                          36.3.4's import hook token and Stage 38.2a's API
--                          keys both already use.
--
-- Bulk channel download (36.4.5) is engine behaviour over the existing
-- ChannelConnector framework (an optional read-back capability a connector
-- may implement) - no new schema for it, the same "no new table" posture
-- 36.5.3/36.5.4 took for wiring transform rules into export/import.
--
-- Everything here is additive. Nothing alters an existing doctype, column or
-- row, so a tenant that never opens the Export Templates/Schedules/Catalogs
-- screens is byte-identical to what it is today.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PIMExportTemplate', 'PIM', 'pim', 'Master'),
('PIMExportSchedule', 'PIM', 'pim', 'Master'),
('PIMCatalog', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.4.1 / 36.4.3 - PIMExportTemplate. column_mappings is a JSONTable for the
-- same reason PIMImportTemplate.column_mappings is: only ever read as part
-- of its parent template. Column order IS the array's row order - no
-- separate sort_order field to keep in sync with it. field_key is checked
-- against a closed set (raw ERP fields) or, when `channel` is set, that
-- channel's own ChannelFieldMap target fields, by
-- ValidatePIMExportTemplateDocument (engines/pim_export_template.go) at save
-- time - the same "the form cannot offer what the engine cannot run"
-- guarantee 36.2.3/36.3.3/36.5 established.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMExportTemplate', 'code', 'Template Code', 'Data', TRUE, NULL, 1),
('PIMExportTemplate', 'name', 'Template Name', 'Data', TRUE, NULL, 2),
('PIMExportTemplate', 'channel', 'Channel (blank = raw ERP export)', 'Link', FALSE, 'Channel', 3),
('PIMExportTemplate', 'column_mappings', 'Columns', 'JSONTable', TRUE,
 '[{"key":"field_key","label":"Field","type":"text","required":true},
   {"key":"column_header","label":"Column Header","type":"text","required":true}]', 4),
('PIMExportTemplate', 'headerless', 'Headerless Output', 'Select', TRUE, 'No,Yes', 5),
('PIMExportTemplate', 'variant_mode', 'Variant Mode', 'Select', TRUE,
 'All Rows,Parent Only - Variants Collapsed', 6),
('PIMExportTemplate', 'description', 'Description', 'Data', FALSE, NULL, 7),
('PIMExportTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.4.2 - PIMExportSchedule. frequency/next_run_date reuse ScheduledReport's
-- own shape (Stage 26.10.4, engines/scheduled_reports.go's
-- advanceScheduleDate) rather than a second date-advancement scheme.
-- Delivery is via the existing outbox (engines/outbox.go), the same
-- simulated-dispatch posture ScheduledReport already takes - no real SMTP/
-- webhook HTTP client, avoiding an SSRF surface against an arbitrary
-- user-supplied webhook URL. last_run_at/last_run_status are engine-managed.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMExportSchedule', 'code', 'Schedule Code', 'Data', TRUE, NULL, 1),
('PIMExportSchedule', 'name', 'Schedule Name', 'Data', TRUE, NULL, 2),
('PIMExportSchedule', 'export_template', 'Export Template', 'Link', TRUE, 'PIMExportTemplate', 3),
('PIMExportSchedule', 'frequency', 'Frequency', 'Select', TRUE, 'Daily,Weekly,Monthly', 4),
('PIMExportSchedule', 'next_run_date', 'Next Run Date', 'Data', TRUE, NULL, 5),
('PIMExportSchedule', 'recipient_email', 'Recipient Email', 'Data', FALSE, NULL, 6),
('PIMExportSchedule', 'webhook_url', 'Webhook URL', 'Data', FALSE, NULL, 7),
('PIMExportSchedule', 'last_run_at', 'Last Run At', 'Data', FALSE, NULL, 8),
('PIMExportSchedule', 'last_run_status', 'Last Run Status', 'Data', FALSE, NULL, 9),
('PIMExportSchedule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.4.4 - PIMCatalog. product_group reuses the exact 36.1.3 resolver seam
-- (ResolvePIMProductGroup) - "the Winter Launch group" means one thing
-- everywhere. share_token_hash/last_shared_at are engine-managed, the same
-- posture PIMImportSchedule.hook_token_hash already takes: not specially
-- hardened against a trusted role's own raw API writes (RBAC on this
-- doctype already gates who can touch a catalog at all), just
-- conventionally left off the authoring form.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMCatalog', 'code', 'Catalog Code', 'Data', TRUE, NULL, 1),
('PIMCatalog', 'name', 'Catalog Name', 'Data', TRUE, NULL, 2),
('PIMCatalog', 'product_group', 'Product Group', 'Link', TRUE, 'PIMProductGroup', 3),
('PIMCatalog', 'expiry_date', 'Expiry Date (blank = no expiry)', 'Data', FALSE, NULL, 4),
('PIMCatalog', 'share_token_hash', 'Share Token Hash (system-managed)', 'Data', FALSE, NULL, 5),
('PIMCatalog', 'last_shared_at', 'Last Viewed At (system-managed)', 'Data', FALSE, NULL, 6),
('PIMCatalog', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Permissions - same split every PIM authoring master gets (36.2/36.3
-- precedent): Super Admin full control, Store Manager can author and use
-- but not delete one out from under other work that references it.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'PIMExportTemplate', TRUE, TRUE, TRUE, TRUE),
('Super Admin',   'PIMExportSchedule', TRUE, TRUE, TRUE, TRUE),
('Super Admin',   'PIMCatalog',        TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PIMExportTemplate', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'PIMExportSchedule', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'PIMCatalog',        TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO UPDATE SET
    allow_read   = EXCLUDED.allow_read,
    allow_create = EXCLUDED.allow_create,
    allow_update = EXCLUDED.allow_update,
    allow_delete = EXCLUDED.allow_delete;

-- ---------------------------------------------------------------------------
-- A template/schedule/catalog code is quoted by name, so each must resolve
-- to exactly one row. The share-token lookup is the one hot read the public
-- catalog-share handler makes on every view, keyed on the digest, not the
-- catalog's own id/code.
-- ---------------------------------------------------------------------------
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_export_template_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'PIMExportTemplate' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_export_schedule_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'PIMExportSchedule' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_catalog_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'PIMCatalog' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_pim_catalog_share_token
    ON tenant_default.documents ((data->>'share_token_hash'))
    WHERE doctype = 'PIMCatalog' AND deleted_at IS NULL AND status = 'Active';

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- PIM-stage migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
  doctype_list TEXT[] := ARRAY['PIMExportTemplate', 'PIMExportSchedule', 'PIMCatalog'];
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
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_export_template_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'PIMExportTemplate' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_export_schedule_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'PIMExportSchedule' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_catalog_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'PIMCatalog' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE INDEX IF NOT EXISTS idx_documents_pim_catalog_share_token
        ON %I.documents ((data->>'share_token_hash'))
       WHERE doctype = 'PIMCatalog' AND deleted_at IS NULL AND status = 'Active'
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
