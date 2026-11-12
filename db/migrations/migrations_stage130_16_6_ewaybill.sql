-- Stage 130.16 (74.4.1-74.4.7, the e-way bill side): one row per document
-- that carries an e-way bill decision - a delivery challan, a sales invoice or
-- a stock transfer - with the decision and its rule version, the bill's own
-- status (NotRequired, Ready, Submitted, Generated, Failed, Expired,
-- Cancelled, Reconciled - apart from the document's), its number, validity,
-- transport (Part B) and where it came from (a provider, or the government
-- portal recorded by hand). Additive: a new table.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');
    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.ewb_documents (
        id SERIAL PRIMARY KEY,
        doctype VARCHAR(100) NOT NULL,
        doc_id VARCHAR(200) NOT NULL,
        doc_number VARCHAR(100),
        gstin VARCHAR(20),
        status VARCHAR(20) NOT NULL DEFAULT 'Ready',
        decision JSONB,
        value NUMERIC(18,2),
        from_state VARCHAR(10),
        to_state VARCHAR(10),
        distance_km INT,
        transport_mode VARCHAR(20),
        vehicle_no VARCHAR(20),
        transporter_id VARCHAR(20),
        ewb_no VARCHAR(20),
        ewb_date TIMESTAMP,
        valid_upto TIMESTAMP,
        extensions INT NOT NULL DEFAULT 0,
        attempts INT NOT NULL DEFAULT 0,
        source VARCHAR(20),
        payload_hash VARCHAR(64),
        last_error TEXT,
        created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        UNIQUE (doctype, doc_id)
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_ewb_documents_status ON %I.ewb_documents (status, valid_upto)', schema_rec.schema_name);
  END LOOP;
END
$mig$;
