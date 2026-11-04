-- ---------------------------------------------------------------------------
-- Stage 146.5.12: typed product relationships.
--
-- One row says "for this item, that item is a Similar / Complete the look /
-- Alternative / Upsell / Cross-sell / Add-on". A plain Master doctype: the
-- generic list, form, CSV import, RBAC and audit apply. Additive only; no
-- existing row is touched and nothing changes until someone adds a relation.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ProductRelation', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductRelation', 'item', 'Item', 'Link', TRUE, 'Item', 1),
('ProductRelation', 'relation_type', 'Relation', 'Select', TRUE, 'Similar,Complete the look,Alternative,Upsell,Cross-sell,Add-on', 2),
('ProductRelation', 'related_item', 'Related Item', 'Link', TRUE, 'Item', 3),
('ProductRelation', 'position', 'Order', 'Number', FALSE, NULL, 4),
('ProductRelation', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'ProductRelation', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ProductRelation', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- One relation per item, type and related item.
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_product_relation_unique
    ON tenant_default.documents ((data->>'item'), (data->>'relation_type'), (data->>'related_item'))
    WHERE doctype = 'ProductRelation' AND deleted_at IS NULL;

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
        FROM tenant_default.doctype_meta WHERE name = 'ProductRelation'
      ON CONFLICT (name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'ProductRelation'
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'ProductRelation'
      ON CONFLICT (role, doctype_name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_product_relation_unique
        ON %I.documents ((data->>'item'), (data->>'relation_type'), (data->>'related_item'))
       WHERE doctype = 'ProductRelation' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
