-- Stage 148.40: correct a piece's attributes. A serial keeps its own attribute
-- values (same JSON shape as Batch.attributes) and both a batch and a serial
-- keep the history of corrections (engines/piece_attribute_correction.go).
-- Field rows only, no client data is written. Safe to re-run.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SerialNumber', 'attributes', 'Attributes JSON', 'Data', FALSE, NULL, 20),
('SerialNumber', 'attribute_history', 'Attribute Corrections', 'Data', FALSE, NULL, 21),
('Batch', 'attribute_history', 'Attribute Corrections', 'Data', FALSE, NULL, 21)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
       WHERE (doctype_name, fieldname) IN (('SerialNumber','attributes'), ('SerialNumber','attribute_history'), ('Batch','attribute_history'))
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
