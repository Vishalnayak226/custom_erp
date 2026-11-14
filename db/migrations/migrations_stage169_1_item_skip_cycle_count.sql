-- ---------------------------------------------------------------------------
-- Stage 146.3.16 (ask 169): leave an item out of cycle counts. One optional
-- Item box, "Leave out of cycle counts" (No / Yes), for low-value items such
-- as packaging or display props. The cycle-count plans skip a Yes item; a full
-- physical inventory and a count someone scans by hand still count it.
-- Metadata only, replayed into every tenant schema; no row changes.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL;
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT v.* FROM (VALUES
        ('Item', 'skip_cycle_count', 'Leave out of cycle counts', 'Select', FALSE, 'No,Yes', 170)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = v.doctype_name)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$f$,
      schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
