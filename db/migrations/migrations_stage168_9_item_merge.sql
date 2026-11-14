-- ---------------------------------------------------------------------------
-- Stage 168.9: product merge. Two system-set Item boxes: where a merged item went
-- (merged_into) and the codes it took over (alias_codes). Metadata only,
-- replayed into every tenant schema; nothing is backfilled.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'merged_into', 'Merged into', 'Data', FALSE, NULL, 160),
('Item', 'alias_codes', 'Old codes', 'Data', FALSE, NULL, 161)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL;
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
       WHERE doctype_name = 'Item' AND fieldname IN ('merged_into', 'alias_codes')
         AND EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'Item')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$f$,
      schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
