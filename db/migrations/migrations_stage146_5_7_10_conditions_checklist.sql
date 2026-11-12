-- ---------------------------------------------------------------------------
-- Stage 146.5.7 / 146.5.10: a dynamic product group may carry typed conditions
-- on attribute values (filter_conditions), and a PIM task may carry an
-- attribute checklist (checklist_attributes). Both are optional fields on
-- existing doctypes: nothing changes for a group or task that does not use
-- them. Field rows only; no client document is touched.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMProductGroup', 'filter_conditions', 'Dynamic: Attribute Conditions', 'JSONTable', FALSE,
 '[{"key":"attribute","label":"Attribute or field","type":"text","required":true},
   {"key":"operator","label":"Operator","type":"select","options":"equals,not equals,in,contains,starts with,greater than,less than,between,before,after,is empty,is not empty","required":true},
   {"key":"value","label":"Value"}]', 11),
('PIMTask', 'checklist_attributes', 'Attributes To Fill (comma-separated)', 'Data', FALSE, NULL, 18)
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
       WHERE (doctype_name = 'PIMProductGroup' AND fieldname = 'filter_conditions')
          OR (doctype_name = 'PIMTask' AND fieldname = 'checklist_attributes')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
