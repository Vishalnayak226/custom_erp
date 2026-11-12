-- ---------------------------------------------------------------------------
-- Stage 134.3 (49.8.3): the inbound webhook replay store. One row per
-- delivery handled (the platform's delivery id, or the body's hash), so a
-- repeat is answered OK and not processed twice. Rows older than 30 days are
-- pruned by the server. Additive; nothing existing changes.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %1$I.inbound_webhook_receipts (
        source VARCHAR(120) NOT NULL,
        event_id VARCHAR(200) NOT NULL,
        received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (source, event_id)
      )$sql$, schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_inbound_webhook_receipts_at ON %1$I.inbound_webhook_receipts (received_at)', schema_rec.schema_name);
  END LOOP;
END
$mig$;
