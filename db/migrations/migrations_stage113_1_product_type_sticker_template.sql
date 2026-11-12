-- ---------------------------------------------------------------------------
-- Stage 113.1 (user, 2026-10-19: "Template in product type level" - which
-- sticker template prints, set per Product type).
--
-- ProductType.sticker_template - a Link to the StickerTemplate its items'
-- labels use. engines.ResolveStickerTemplateForItem checks it after the
-- Item's and the Item Type's, before the category. Blank means "next rule",
-- so no existing item changes what it prints.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      EXECUTE format($sql$
        INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT v.* FROM (VALUES
          ('ProductType', 'sticker_template', 'Sticker template', 'Link', FALSE, 'StickerTemplate', 11)
        ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = v.doctype_name)
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
