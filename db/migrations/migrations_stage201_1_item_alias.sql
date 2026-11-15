-- ---------------------------------------------------------------------------
-- Item 201.1 (user, 2026-11-15): "Need a Item Alias name that will be
-- printed. Make Item Alias Auto generated from Item Name. Can be overwritten.
-- ... It will be char limit 14."
--
-- Declares one optional field, item_alias, on the Item Name master and on
-- each Item. engines/item_alias.go fills it (made from the name when left
-- blank, at most 14 characters) and stickers print it. Additive and
-- idempotent, replayed into every tenant schema; no stored record is changed
-- (a record without an alias prints one made from its name).
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
      SELECT v.doctype_name, v.fieldname, v.label, v.fieldtype, FALSE, NULL, v.display_order
        FROM (VALUES
          ('ItemName', 'item_alias', 'Item Alias', 'Data', 4),
          ('Item', 'item_alias', 'Item Alias', 'Data', 3)
        ) AS v(doctype_name, fieldname, label, fieldtype, display_order)
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = v.doctype_name)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
