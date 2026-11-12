-- ---------------------------------------------------------------------------
-- Stage 137.1: a printer's sticker preset.
--
-- The roll loaded in a label printer decides how a sticker must be laid out,
-- but the template followed the item's rule, so a horizontal one-tag template
-- could be sent to a 3-across vertical roll and cross the tags (2026-10-24).
-- And the alignment correction lived in each template, so a printer that
-- prints 1 mm off had to be corrected in every template.
--
--   sticker_template - the template for the roll loaded in this printer. A
--                      line whose template does not fit that roll prints
--                      with this one (engines/stickers.go applyPrinterRoll).
--   shift_right_mm / shift_down_mm - this printer's own alignment, added to
--                      the template's offsets; may be negative.
--   darkness / print_speed - blank = the template's own setting.
--
-- Fields only: the values live in the Printer record's data, as the Stage
-- 31.1 fields do. Existing printers are untouched (all blank).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Printer', 'sticker_template', 'Roll loaded (sticker template)', 'Link', FALSE, 'StickerTemplate', 11),
('Printer', 'shift_right_mm', 'Shift right (mm)', 'Number', FALSE, NULL, 12),
('Printer', 'shift_down_mm', 'Shift down (mm)', 'Number', FALSE, NULL, 13),
('Printer', 'darkness', 'Darkness (0-15)', 'Number', FALSE, NULL, 14),
('Printer', 'print_speed', 'Speed (inches/s)', 'Number', FALSE, NULL, 15)
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
        ('Printer', 'sticker_template', 'Roll loaded (sticker template)', 'Link', FALSE, 'StickerTemplate'::text, 11),
        ('Printer', 'shift_right_mm', 'Shift right (mm)', 'Number', FALSE, NULL::text, 12),
        ('Printer', 'shift_down_mm', 'Shift down (mm)', 'Number', FALSE, NULL::text, 13),
        ('Printer', 'darkness', 'Darkness (0-15)', 'Number', FALSE, NULL::text, 14),
        ('Printer', 'print_speed', 'Speed (inches/s)', 'Number', FALSE, NULL::text, 15)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'Printer')
        AND EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'StickerTemplate')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$f$,
      schema_rec.schema_name, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
