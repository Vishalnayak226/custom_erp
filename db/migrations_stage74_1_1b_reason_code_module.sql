-- ---------------------------------------------------------------------------
-- Stage 74.1.1: reason codes serve purchasing, the till and stock as well as
-- OMS, so the Reason Code master moves to Master Data - a module every client
-- has (engines/modules.go) - and its Setup screen and record API are reachable
-- for a client without OMS. Only the master's module label changes; its
-- fields, records and every role's permissions stay as they are.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      UPDATE %1$I.doctype_meta SET module = 'Master Data', module_key = 'master_data'
       WHERE name = 'ReasonCode' AND module_key = 'oms'$sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
