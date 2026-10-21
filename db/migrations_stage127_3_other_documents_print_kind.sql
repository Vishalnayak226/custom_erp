-- ---------------------------------------------------------------------------
-- Stage 127.3 (user, 2026-10-21: "all txn need print"): every transaction
-- prints; one with no format of its own prints through the generic format
-- (engines/print_generic.go) as the kind of print "Other Documents", which a
-- computer chooses a printer for like any other kind.
--
-- Printer."Default For" gains it, so a store-wide default can name it too.
-- Appended only where it is missing. Additive and idempotent, replayed into
-- every tenant schema.
-- ---------------------------------------------------------------------------
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
      UPDATE %I.doctype_fields
         SET options = options || ',' || $1
       WHERE doctype_name = 'Printer' AND fieldname = 'print_role'
         AND COALESCE(options, '') <> ''
         AND (',' || options || ',') NOT LIKE ('%%,' || $1 || ',%%')
    $sql$, schema_rec.schema_name) USING 'Other Documents';
  END LOOP;
END
$mig$;
