-- ---------------------------------------------------------------------------
-- Item ranging: which items a store or a channel carries. One row names a store
-- or a channel, an item or a whole category, and a date window. A plain Master
-- doctype (generic list, form, CSV import, RBAC, audit). Additive; nothing
-- changes until the setting inventory.assortment_enforce is turned on AND a
-- store has at least one row.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('Assortment', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Assortment', 'store', 'Store', 'Link', FALSE, 'Location', 1),
('Assortment', 'channel', 'Channel', 'Link', FALSE, 'Channel', 2),
('Assortment', 'item', 'Item', 'Link', FALSE, 'Item', 3),
('Assortment', 'category', 'Category', 'Data', FALSE, NULL, 4),
('Assortment', 'valid_from', 'From', 'Date', FALSE, NULL, 5),
('Assortment', 'valid_to', 'To', 'Date', FALSE, NULL, 6),
('Assortment', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'Assortment', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Assortment', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

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
        FROM tenant_default.doctype_meta WHERE name = 'Assortment'
      ON CONFLICT (name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'Assortment'
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'Assortment'
      ON CONFLICT (role, doctype_name) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
