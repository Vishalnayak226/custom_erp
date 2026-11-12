-- Stage 130.1: a goods receipt keeps all its lines in one field
-- (GRN.received_items, JSON). With no limit of its own it was held to the
-- org's blanket platform.field_max_length (10,000 characters), about 27
-- lines. Give the field its own limit - about 4,000 lines - the way
-- Stage 63.1 did for a print template's layout. Additive: only a field with
-- no limit of its own is changed, so a limit an admin set stays.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.columns
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields' AND column_name = 'max_length');

    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET max_length = 1500000
      WHERE doctype_name = 'GRN' AND fieldname = 'received_items' AND max_length IS NULL
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
