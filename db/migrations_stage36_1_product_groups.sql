-- Stage 36.1.1-36.1.2: reusable PIM product groups.
--
-- A group is a normal PIM Master so the existing generic form, permissions,
-- audit trail and CSV tooling work without a parallel admin surface. Static
-- groups store hand-picked Item links. Dynamic groups expose four typed fields
-- which map to the same flat key/value parameter vocabulary as registered
-- reports. Typed fields make Family/Attribute real Link pickers and avoid a
-- user-authored query language or hand-written JSON.

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PIMProductGroup', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMProductGroup', 'code', 'Group Code', 'Data', TRUE, NULL, 1),
('PIMProductGroup', 'name', 'Group Name', 'Data', TRUE, NULL, 2),
('PIMProductGroup', 'group_type', 'Group Type', 'Select', TRUE, 'Static,Dynamic', 3),
('PIMProductGroup', 'members', 'Static Products', 'JSONTable', FALSE,
 '[{"key":"item_code","label":"Item","type":"link","link":"Item","required":true}]', 4),
('PIMProductGroup', 'filter_family', 'Dynamic: Product Family', 'Link', FALSE, 'ProductFamily', 5),
('PIMProductGroup', 'filter_completeness_below', 'Dynamic: Completeness Below (%)', 'Number', FALSE, NULL, 6),
('PIMProductGroup', 'filter_missing_attribute', 'Dynamic: Missing Attribute', 'Link', FALSE, 'ProductAttributeDef', 7),
('PIMProductGroup', 'filter_status', 'Dynamic: Item Status', 'Select', FALSE, 'Active,Inactive', 8),
('PIMProductGroup', 'description', 'Description', 'Data', FALSE, NULL, 9),
('PIMProductGroup', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PIMProductGroup', TRUE, TRUE, TRUE, TRUE),
('Super Admin', 'PIMProductGroup', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PIMProductGroup', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO UPDATE SET
    allow_read = EXCLUDED.allow_read,
    allow_create = EXCLUDED.allow_create,
    allow_update = EXCLUDED.allow_update,
    allow_delete = EXCLUDED.allow_delete;

-- Group Code is the operator-facing reference accepted by reports and the
-- member resolver. The generic document primary key does not cover JSON data,
-- so enforce its uniqueness explicitly instead of resolving an arbitrary row.
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_product_group_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'PIMProductGroup' AND deleted_at IS NULL;

-- Existing tenant schemas are independent copies of tenant_default metadata.
-- Backfill them from the canonical rows so a tenant provisioned before Stage
-- 36 receives the same master and permissions as a new tenant.
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
        FROM tenant_default.doctype_meta WHERE name = 'PIMProductGroup'
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module,
        module_key = EXCLUDED.module_key,
        document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'PIMProductGroup'
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label,
        fieldtype = EXCLUDED.fieldtype,
        mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options,
        display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'PIMProductGroup'
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read,
        allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update,
        allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_product_group_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'PIMProductGroup' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
