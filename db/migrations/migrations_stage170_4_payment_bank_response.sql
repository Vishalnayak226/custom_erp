-- ---------------------------------------------------------------------------
-- Stage 170.4: the bank's acknowledgement of a payment file. One row per
-- payment reference (the invoice id in the file) answered by the bank:
-- paid (with the UTR) or rejected (with the reason). Additive; the UTR itself
-- still goes to payment_utr_log.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');
    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.payment_bank_response (
        id SERIAL PRIMARY KEY,
        proposal_id VARCHAR(100) NOT NULL,
        invoice_id VARCHAR(100) NOT NULL,
        status VARCHAR(20) NOT NULL,
        utr VARCHAR(50),
        reason TEXT,
        exception_line_id VARCHAR(100),
        recorded_by VARCHAR(100),
        recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE (proposal_id, invoice_id)
      )$sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
