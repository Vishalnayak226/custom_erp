-- ---------------------------------------------------------------------------
-- Stage 251.2: the rolls a printer prints perfectly on.
--
-- After the first real print on a printer and roll, the ERP asks how it came
-- out (Perfect / Bit off / Bad). Perfect is remembered here so it asks once;
-- Bit off or Bad opens Fix from photo instead.
--
--   align_confirmed - the confirmed rolls, comma separated, each as
--                     <width>x<height>x<across>[v] (public/print-check.js
--                     rollKey). Blank = ask on the next print; clearing it
--                     makes the ERP ask again.
--
-- Field only: the value lives in the Printer record's data, beside the Stage
-- 137.1 preset. Existing printers are untouched (blank).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Printer', 'align_confirmed', 'Alignment confirmed for', 'Data', FALSE, NULL, 17)
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
      SELECT 'Printer', 'align_confirmed', 'Alignment confirmed for', 'Data', FALSE, NULL::text, 17
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'Printer')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$f$,
      schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
