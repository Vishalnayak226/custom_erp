-- ---------------------------------------------------------------------------
-- Stage 98.1 (user, 2026-10-19: "Same for PO, GRN, TO and Sales Bill").
--
-- The Sales Invoice joins the print transactions (print, PDF, CSV / Excel,
-- send), and a template can be designed for it. PrintTemplate.transaction is
-- a Select: its options gain SalesInvoice, and POSTaxInvoice, which Stage
-- 85.D2 made printable without adding it here. Each key is appended only
-- where it is missing, so an option list an admin has edited keeps its
-- entries.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
  txn TEXT;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      FOREACH txn IN ARRAY ARRAY['POSTaxInvoice', 'SalesInvoice'] LOOP
        EXECUTE format($sql$
          UPDATE %I.doctype_fields
             SET options = options || ',' || $1
           WHERE doctype_name = 'PrintTemplate' AND fieldname = 'transaction'
             AND COALESCE(options, '') <> ''
             AND (',' || options || ',') NOT LIKE ('%%,' || $1 || ',%%')
        $sql$, schema_rec.schema_name) USING txn;
      END LOOP;
    END IF;
  END LOOP;
END
$mig$;
