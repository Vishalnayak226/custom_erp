-- ---------------------------------------------------------------------------
-- Stage 148.35: buy in packs, stock in pieces (setting inventory.pack_units).
--
-- Four Item boxes: the stock unit, an optional purchase unit with how many
-- stock units are in one, and whether part-pieces are allowed. Metadata only,
-- replayed into every tenant schema; they stay hidden until the setting is on
-- (engines/pack_units_stage148_35.go). No existing item, document or posting
-- row is changed. PO and GRN lines carry their pack keys inside their own JSON,
-- so those need no column.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'uom', 'Stock unit', 'Data', FALSE, NULL, 140),
('Item', 'purchase_unit', 'Purchase unit', 'Data', FALSE, NULL, 141),
('Item', 'purchase_unit_factor', 'Stock units per purchase unit', 'Number', FALSE, NULL, 142),
('Item', 'allow_fraction', 'Part-pieces allowed', 'Select', FALSE, ',Yes', 143)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT * FROM (VALUES
        ('Item', 'uom', 'Stock unit', 'Data', FALSE, NULL::text, 140),
        ('Item', 'purchase_unit', 'Purchase unit', 'Data', FALSE, NULL::text, 141),
        ('Item', 'purchase_unit_factor', 'Stock units per purchase unit', 'Number', FALSE, NULL::text, 142),
        ('Item', 'allow_fraction', 'Part-pieces allowed', 'Select', FALSE, ',Yes'::text, 143)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = v.doctype_name)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$f$,
      schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
