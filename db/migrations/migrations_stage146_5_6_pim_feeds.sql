-- ---------------------------------------------------------------------------
-- Stage 146.5.6: feed runs. A PIMFeed is a template + product group + "ready
-- only" + format + destination; every run is stored as a PIMExportRun with its
-- skipped items and the reasons. A PIMExportSchedule may name a feed instead
-- of a template (export_template is no longer mandatory at the field level;
-- the validator still asks for a template when no feed is named). Additive;
-- nothing changes until a feed is made.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PIMFeed', 'PIM', 'pim', 'Master'),
('PIMExportRun', 'PIM', 'pim', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMFeed', 'code', 'Code', 'Data', TRUE, NULL, 1),
('PIMFeed', 'name', 'Name', 'Data', TRUE, NULL, 2),
('PIMFeed', 'export_template', 'Template', 'Link', TRUE, 'PIMExportTemplate', 3),
('PIMFeed', 'product_group', 'Product Group', 'Link', FALSE, 'PIMProductGroup', 4),
('PIMFeed', 'ready_only', 'Ready Only', 'Select', FALSE, 'No,Yes', 5),
('PIMFeed', 'format', 'Format', 'Select', FALSE, 'csv,xlsx', 6),
('PIMFeed', 'destination', 'Destination', 'Select', FALSE, 'Download,Email', 7),
('PIMFeed', 'recipient_email', 'Recipient Email', 'Data', FALSE, NULL, 8),
('PIMFeed', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 9),
('PIMExportRun', 'feed', 'Feed', 'Link', FALSE, 'PIMFeed', 1),
('PIMExportRun', 'export_template', 'Template', 'Data', FALSE, NULL, 2),
('PIMExportRun', 'started_at', 'Started', 'Data', FALSE, NULL, 3),
('PIMExportRun', 'status', 'Status', 'Data', FALSE, NULL, 4),
('PIMExportRun', 'rows', 'Rows', 'Data', FALSE, NULL, 5),
('PIMExportRun', 'skipped_count', 'Skipped', 'Data', FALSE, NULL, 6),
('PIMExportRun', 'skipped', 'Skipped Items', 'JSONTable', FALSE,
 '[{"key":"item","label":"Item","type":"text"},
   {"key":"column","label":"Column","type":"text"},
   {"key":"reason","label":"Reason","type":"text"}]', 7),
('PIMExportRun', 'format', 'Format', 'Data', FALSE, NULL, 8),
('PIMExportRun', 'destination', 'Destination', 'Data', FALSE, NULL, 9),
('PIMExportRun', 'delivery_status', 'Delivery', 'Data', FALSE, NULL, 10),
('PIMExportRun', 'delivery_message', 'Delivery Note', 'Data', FALSE, NULL, 11),
('PIMExportRun', 'run_by', 'Run By', 'Data', FALSE, NULL, 12),
('PIMExportSchedule', 'feed', 'Feed', 'Link', FALSE, 'PIMFeed', 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

UPDATE tenant_default.doctype_fields SET mandatory = FALSE
 WHERE doctype_name = 'PIMExportSchedule' AND fieldname = 'export_template';

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'PIMFeed', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PIMFeed', TRUE, TRUE, TRUE, FALSE),
('Super Admin',   'PIMExportRun', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'PIMExportRun', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_feed_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'PIMFeed' AND deleted_at IS NULL;

DO $$
DECLARE
  schema_rec RECORD;
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
        FROM tenant_default.doctype_meta WHERE name IN ('PIMFeed', 'PIMExportRun')
      ON CONFLICT (name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
       WHERE doctype_name IN ('PIMFeed', 'PIMExportRun')
          OR (doctype_name = 'PIMExportSchedule' AND fieldname = 'feed')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      UPDATE %I.doctype_fields SET mandatory = FALSE
       WHERE doctype_name = 'PIMExportSchedule' AND fieldname = 'export_template'
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name IN ('PIMFeed', 'PIMExportRun')
      ON CONFLICT (role, doctype_name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_feed_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'PIMFeed' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
