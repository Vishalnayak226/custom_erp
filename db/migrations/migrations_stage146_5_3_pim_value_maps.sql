-- ---------------------------------------------------------------------------
-- Stage 146.5.3 (D15): PIM value maps, kept per marketplace / channel.
--
-- One row says "on this channel, our value X for this attribute is called Y".
-- A transform rule's value_map step reads it, so every template of that
-- marketplace uses the same names and Myntra and Amazon can differ. A plain
-- Master doctype: the generic list, form, CSV import, RBAC and audit apply.
-- Additive only; nothing changes until a transform rule uses the step.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PIMValueMap', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMValueMap', 'channel', 'Channel', 'Link', TRUE, 'Channel', 1),
('PIMValueMap', 'attribute', 'Attribute', 'Data', TRUE, NULL, 2),
('PIMValueMap', 'our_value', 'Our Value', 'Data', TRUE, NULL, 3),
('PIMValueMap', 'their_value', 'Their Value', 'Data', TRUE, NULL, 4),
('PIMValueMap', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'PIMValueMap', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PIMValueMap', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- One name per channel + attribute + our value.
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_value_map_unique
    ON tenant_default.documents (UPPER(data->>'channel'), UPPER(data->>'attribute'), UPPER(TRIM(data->>'our_value')))
    WHERE doctype = 'PIMValueMap' AND deleted_at IS NULL;

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
        FROM tenant_default.doctype_meta WHERE name = 'PIMValueMap'
      ON CONFLICT (name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'PIMValueMap'
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'PIMValueMap'
      ON CONFLICT (role, doctype_name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_value_map_unique
        ON %I.documents (UPPER(data->>'channel'), UPPER(data->>'attribute'), UPPER(TRIM(data->>'our_value')))
       WHERE doctype = 'PIMValueMap' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
