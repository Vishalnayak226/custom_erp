-- Item 74.0.1 (DEC-10, CGST Act s.143): an optional "Received by job worker on"
-- date on the job work order. When goods go direct to the job worker, the
-- one-year (inputs) or three-year (capital goods) limit counts from that date;
-- without it the send date stays the strict default. Additive and safe to run twice.
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
        ('SubcontractOrder', 'job_worker_received_on', 'Received by job worker on', 'Date', FALSE, NULL, 24)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
