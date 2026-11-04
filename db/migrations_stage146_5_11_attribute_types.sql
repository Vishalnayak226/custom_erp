-- ---------------------------------------------------------------------------
-- Stage 146.5.11: four more attribute types.
--
-- ProductAttributeDef.value_type already offers Data, Number, Select and Date.
-- This adds Yes / No, Multi-select, Long text and URL to that list. Only the
-- field's option list changes (and only where it is still the shipped list, so
-- a client's own edit is left alone). No attribute or value row is touched.
-- ---------------------------------------------------------------------------

UPDATE tenant_default.doctype_fields
   SET options = 'Data,Number,Select,Date,Yes / No,Multi-select,Long text,URL'
 WHERE doctype_name = 'ProductAttributeDef' AND fieldname = 'value_type'
   AND options = 'Data,Number,Select,Date';

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
      UPDATE %I.doctype_fields
         SET options = 'Data,Number,Select,Date,Yes / No,Multi-select,Long text,URL'
       WHERE doctype_name = 'ProductAttributeDef' AND fieldname = 'value_type'
         AND options = 'Data,Number,Select,Date'
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
