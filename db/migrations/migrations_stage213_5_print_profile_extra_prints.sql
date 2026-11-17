-- Stage 213.5 (ask 224): a Print Profile can ask for extra prints beyond the
-- main one, each going to the printer set up for its paper. One line:
-- "<template name or id, Roll 80, Roll 58, Standard> x<copies>, ...".
-- Additive and idempotent, replayed into every tenant schema.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('PrintProfile', 'extra_prints', 'Extra prints (template x copies, comma-separated)', 'Data', FALSE, NULL, 104)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
