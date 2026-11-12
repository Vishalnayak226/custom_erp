-- Stage 49.11.6 (history backfill, live-window gap check) on the optional
-- security-event export. Additive: three columns on security_export_config.
--
-- backfill_until_seq / backfill_from: an administrator chose to send events from a
--   start date. The cursor is moved back to just before that date and the worker
--   sends strictly in order through the same batch limits; backfill_until_seq is
--   the cursor's old position, where the backfill is complete.
-- live_gap_checked_seq: sealed live checkpoint windows up to this seq were already
--   checked against the table, so a missing row is reported once.
ALTER TABLE tenant_default.security_export_config ADD COLUMN IF NOT EXISTS backfill_until_seq BIGINT NOT NULL DEFAULT 0;
ALTER TABLE tenant_default.security_export_config ADD COLUMN IF NOT EXISTS backfill_from TIMESTAMP;
ALTER TABLE tenant_default.security_export_config ADD COLUMN IF NOT EXISTS live_gap_checked_seq BIGINT NOT NULL DEFAULT 0;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.security_export_config ADD COLUMN IF NOT EXISTS backfill_until_seq BIGINT NOT NULL DEFAULT 0', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.security_export_config ADD COLUMN IF NOT EXISTS backfill_from TIMESTAMP', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.security_export_config ADD COLUMN IF NOT EXISTS live_gap_checked_seq BIGINT NOT NULL DEFAULT 0', schema_rec.schema_name);
  END LOOP;
END $$;
