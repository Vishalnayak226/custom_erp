-- ---------------------------------------------------------------------------
-- Stage 154.5: a printer's skew.
--
-- A roll that runs a little crooked through a label printer drifts sideways
-- as it feeds, so every label comes out tilted on its tag (MINN's Y468BT on
-- the Seznik roll: about 1 degree, the same on every tag). Shift right moves a
-- print; it cannot straighten one.
--
--   skew_deg - degrees the sticker engine slants each print back, about its
--              middle; may be negative. Blank = none.
--
-- Field only: the value lives in the Printer record's data, beside the Stage
-- 137.1 preset. Existing printers are untouched (blank).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Printer', 'skew_deg', 'Skew (degrees)', 'Number', FALSE, NULL, 16)
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
      SELECT 'Printer', 'skew_deg', 'Skew (degrees)', 'Number', FALSE, NULL::text, 16
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'Printer')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$f$,
      schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
