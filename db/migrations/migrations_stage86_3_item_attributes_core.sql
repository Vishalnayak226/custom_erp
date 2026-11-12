-- ---------------------------------------------------------------------------
-- Stage 86.3 (user, 2026-10-18): "PIM attribute is diff and erp is diff ...
-- I can sell different app to client one or the another. If ERP Master data
-- is diff and PIM is diff. PIM will map it and have more attribute but ERP
-- will have very basic attribute." Two separate lists: yes.
--
-- ItemAttribute - the ERP's own attribute list (Inventory, a core module every
-- client has): the few attributes an item varies by, read by the Item's
-- variant options, the PO design panel and the prints. Same fields as PIM's
-- list minus its group and "used for". ProductAttributeDef stays PIM's richer
-- list and gains erp_attribute, the ERP attribute it maps onto.
--
-- Existing attributes are COPIED, never moved: every live ProductAttributeDef
-- the ERP already used (Used for Variant, or blank - blank was offered
-- everywhere) gets an ItemAttribute with the same code, so every item and
-- design keeps finding its values under the key it stored. Its id is
-- 'IA-' || code, because ids are unique across record types and PIM's record
-- keeps the bare code. PIM's record is kept as it is, plus erp_attribute.
--
-- Additive and idempotent; tenant_default and every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL
               OR to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL
               OR to_regclass(format('%I.role_permissions', schema_rec.schema_name)) IS NULL
               OR to_regclass(format('%I.documents', schema_rec.schema_name)) IS NULL;

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('ItemAttribute', 'Inventory', 'inventory', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('ItemAttribute', 'code', 'Attribute Code', 'Data', TRUE, NULL, 1),
        ('ItemAttribute', 'label', 'Label', 'Data', TRUE, NULL, 2),
        ('ItemAttribute', 'value_type', 'Value Type', 'Select', TRUE, 'Select,Data,Number', 3),
        ('ItemAttribute', 'value_options', 'Values (if Value Type = Select)', 'Data', FALSE, NULL, 4),
        ('ItemAttribute', 'uom', 'Unit', 'Data', FALSE, NULL, 5),
        ('ItemAttribute', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6),
        ('ItemAttribute', 'library_id', 'From the attribute library', 'Data', FALSE, NULL, 7),
        ('ProductAttributeDef', 'erp_attribute', 'ERP item attribute', 'Link', FALSE, 'ItemAttribute', 10)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- Whoever could see or change attributes before keeps exactly that.
    EXECUTE format($sql$
      INSERT INTO %1$I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT p.role, 'ItemAttribute', p.allow_read, p.allow_create, p.allow_update, p.allow_delete
        FROM %1$I.role_permissions p
       WHERE p.doctype_name = 'ProductAttributeDef'
         AND NOT EXISTS (SELECT 1 FROM %1$I.role_permissions q
                          WHERE q.role = p.role AND q.doctype_name = 'ItemAttribute')
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %1$I.documents (id, doctype, data, status, created_by)
      SELECT 'IA-' || src.code, 'ItemAttribute',
             jsonb_strip_nulls(jsonb_build_object(
               'code', src.code,
               'label', COALESCE(NULLIF(p.data->>'label', ''), src.code),
               'value_type', COALESCE(NULLIF(p.data->>'value_type', ''), 'Data'),
               'value_options', NULLIF(p.data->>'value_options', ''),
               'uom', NULLIF(p.data->>'uom', ''),
               'status', COALESCE(NULLIF(p.data->>'status', ''), 'Active'),
               'library_id', NULLIF(p.data->>'library_id', ''))),
             COALESCE(NULLIF(p.data->>'status', ''), 'Active'), p.created_by
        FROM %1$I.documents p
        CROSS JOIN LATERAL (SELECT COALESCE(NULLIF(p.data->>'code', ''), p.id) AS code) src
       WHERE p.doctype = 'ProductAttributeDef' AND p.deleted_at IS NULL
         AND COALESCE(p.data->>'used_for', '') <> 'Product information'
      ON CONFLICT (id) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %1$I.documents p
         SET data = p.data || jsonb_build_object('erp_attribute', e.id)
        FROM %1$I.documents e
       WHERE p.doctype = 'ProductAttributeDef' AND p.deleted_at IS NULL
         AND COALESCE(p.data->>'erp_attribute', '') = ''
         AND e.doctype = 'ItemAttribute'
         AND e.id = 'IA-' || COALESCE(NULLIF(p.data->>'code', ''), p.id)
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
