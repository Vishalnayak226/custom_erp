-- ---------------------------------------------------------------------------
-- Stage 168.12 / 168.8: per-category attribute rules and category hierarchy.
--
-- CategoryAttributeRule says, for one category and one attribute, whether the
-- attribute is Required, Optional or Hidden, with an optional default value.
-- Category gets a parent, so a rule on a parent reaches its child categories
-- unless the child has its own rule for that attribute (nearest wins).
--
-- Additive and idempotent, replayed into every tenant schema (tenant_default
-- included). No existing row is written except the metadata rows below, and a
-- category with no parent and no rule behaves exactly as before.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('CategoryAttributeRule', 'PIM', 'pim', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('CategoryAttributeRule', 'category', 'Category', 'Link', TRUE, 'Category', 1),
        ('CategoryAttributeRule', 'attribute', 'Attribute', 'Link', TRUE, 'ProductAttributeDef', 2),
        ('CategoryAttributeRule', 'rule', 'Rule', 'Select', TRUE, 'Required,Optional,Hidden', 3),
        ('CategoryAttributeRule', 'default_value', 'Default value', 'Data', FALSE, NULL, 4),
        ('Category', 'parent', 'Parent category', 'Link', FALSE, 'Category', 11)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('Super Admin',   'CategoryAttributeRule', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin',      'CategoryAttributeRule', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'CategoryAttributeRule', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- One rule per category and attribute.
    EXECUTE format($sql$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_category_attribute_rule_unique
        ON %I.documents ((data->>'category'), (data->>'attribute'))
       WHERE doctype = 'CategoryAttributeRule' AND deleted_at IS NULL
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('CategoryAttributeRule', 'CAR', '-', 4, 'ANNUAL', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
