-- ---------------------------------------------------------------------------
-- Stage 127.6 (user, 2026-10-21: "I need reprint setion for all soort of
-- preint and if will limit client to client if i give less moduel access to
-- them"): the print job log also records each document print - which
-- transaction, which document and number, Print / PDF / Send / Reprint, and a
-- reprint's reason - so Setup > Printing > Reprint can list what was printed,
-- only for the modules the client has, and print it again.
--
-- This is the print log 74.1.4 planned (doctype, reprint reason); its frozen
-- payload, copy label and checksum stay with 74.1.4. Additive: new nullable
-- columns and one index; every existing row keeps its meaning (doctype NULL =
-- a QZ job that is not a document print).
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
       WHERE table_schema = schema_rec.schema_name AND table_name = 'print_job_log');
    EXECUTE format($sql$
      ALTER TABLE %I.print_job_log
        ADD COLUMN IF NOT EXISTS txn VARCHAR(100),
        ADD COLUMN IF NOT EXISTS doctype VARCHAR(100),
        ADD COLUMN IF NOT EXISTS doc_id VARCHAR(200),
        ADD COLUMN IF NOT EXISTS doc_number VARCHAR(200),
        ADD COLUMN IF NOT EXISTS doc_title VARCHAR(200),
        ADD COLUMN IF NOT EXISTS store VARCHAR(100),
        ADD COLUMN IF NOT EXISTS print_action VARCHAR(20),
        ADD COLUMN IF NOT EXISTS reprint_reason TEXT
    $sql$, schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_print_job_log_document ON %I.print_job_log (doctype, doc_id, printed_at DESC)', schema_rec.schema_name);
  END LOOP;
END
$mig$;
