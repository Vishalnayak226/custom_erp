-- ---------------------------------------------------------------------------
-- Stage 89 (user, 2026-10-18, on Edit Item): one pick fills the item, and the
-- attributes an item offers can follow its type.
--
--   Item.item_name     Link -> ItemName (Category + Product Type, with HSN and
--                      GST). Picking it fills the Item's category, product
--                      type, HSN, GST and name (engines/item_setup.go).
--   Item.product_type  the type's name, as Item.category holds the category's.
--   ProductType.attribute_map / Category.attribute_map
--                      which attributes (and, optionally, which values) a type
--                      or category offers when "Attributes follow the item's
--                      type" is on: "COLOR:Red|Gold;POLISH" - blank = all.
--
-- Additive and idempotent, replayed into every tenant schema; nothing in any
-- document changes.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT v.doctype_name, v.fieldname, v.label, v.fieldtype, FALSE, v.options, v.display_order
        FROM (VALUES
          ('Item', 'item_name', 'Item Name', 'Link', 'ItemName', 2),
          ('Item', 'product_type', 'Product Type', 'Data', NULL, 6),
          ('ProductType', 'attribute_map', 'Attributes for this type', 'Data', NULL, 10),
          ('Category', 'attribute_map', 'Attributes for this category', 'Data', NULL, 10)
        ) AS v(doctype_name, fieldname, label, fieldtype, options, display_order)
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = v.doctype_name)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
