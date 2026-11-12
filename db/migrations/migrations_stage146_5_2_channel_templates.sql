-- ---------------------------------------------------------------------------
-- Stage 146.5.2 (D14): one marketplace template per channel + channel category,
-- loaded from the marketplace's own category file.
--
-- ChannelCategoryTemplate holds the file's columns (mandatory flag, accepted
-- values, max length, conditional mandatory). An export template may point at
-- one through the new optional PIMExportTemplate.channel_template; a template
-- with it blank exports exactly as before. Additive only.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ChannelCategoryTemplate', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ChannelCategoryTemplate', 'code', 'Code', 'Data', TRUE, NULL, 1),
('ChannelCategoryTemplate', 'channel', 'Channel', 'Link', TRUE, 'Channel', 2),
('ChannelCategoryTemplate', 'channel_category', 'Channel Category', 'Data', TRUE, NULL, 3),
('ChannelCategoryTemplate', 'columns', 'Columns', 'JSONTable', TRUE,
 '[{"key":"column","label":"Column","type":"text","required":true},
   {"key":"mandatory","label":"Mandatory","type":"text"},
   {"key":"active","label":"Active","type":"text"},
   {"key":"allowed_values","label":"Accepted Values","type":"text"},
   {"key":"max_length","label":"Max Length","type":"text"},
   {"key":"required_if","label":"Required If","type":"text"}]', 4),
('ChannelCategoryTemplate', 'parent_rows', 'Parent Rows', 'Select', FALSE, 'No,Yes', 5),
('ChannelCategoryTemplate', 'parentage_column', 'Parentage Column', 'Data', FALSE, NULL, 6),
('ChannelCategoryTemplate', 'parent_sku_column', 'Parent SKU Column', 'Data', FALSE, NULL, 7),
('ChannelCategoryTemplate', 'variation_theme_column', 'Theme Column', 'Data', FALSE, NULL, 8),
('ChannelCategoryTemplate', 'sku_column', 'SKU Column', 'Data', FALSE, NULL, 9),
('ChannelCategoryTemplate', 'source_file', 'Source File', 'Data', FALSE, NULL, 10),
('ChannelCategoryTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 11),
('PIMExportTemplate', 'channel_template', 'Channel Template', 'Link', FALSE, 'ChannelCategoryTemplate', 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'ChannelCategoryTemplate', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ChannelCategoryTemplate', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- One template per channel + channel category.
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_channel_category_template_unique
    ON tenant_default.documents (UPPER(data->>'channel'), UPPER(TRIM(data->>'channel_category')))
    WHERE doctype = 'ChannelCategoryTemplate' AND deleted_at IS NULL;

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
        FROM tenant_default.doctype_meta WHERE name = 'ChannelCategoryTemplate'
      ON CONFLICT (name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
       WHERE doctype_name = 'ChannelCategoryTemplate'
          OR (doctype_name = 'PIMExportTemplate' AND fieldname = 'channel_template')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'ChannelCategoryTemplate'
      ON CONFLICT (role, doctype_name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_channel_category_template_unique
        ON %I.documents (UPPER(data->>'channel'), UPPER(TRIM(data->>'channel_category')))
       WHERE doctype = 'ChannelCategoryTemplate' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
