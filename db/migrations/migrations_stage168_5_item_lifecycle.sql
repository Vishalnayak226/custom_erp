-- ---------------------------------------------------------------------------
-- Stage 168.5: product lifecycle. Three optional Item boxes: the stage
-- (Launch, Active, Phase-out, End of life), a launch date and a substitute
-- item. Metadata only, replayed into every tenant schema. An item with no
-- stage is Active, so no existing row changes and nothing is backfilled.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'lifecycle_stage', 'Lifecycle', 'Select', FALSE, ',Launch,Active,Phase-out,End of life', 150),
('Item', 'launch_date', 'Launch date', 'Date', FALSE, NULL, 151),
('Item', 'substitute_item', 'Substitute item', 'Link', FALSE, 'Item', 152)
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
       WHERE doctype_name = 'Item' AND fieldname IN ('lifecycle_stage', 'launch_date', 'substitute_item')
         AND EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'Item')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$f$,
      schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
