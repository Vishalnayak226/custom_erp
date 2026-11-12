-- 49.8.5: who saved a connector credential, when it was last used, and an
-- optional expiry - shown on the connector screens as
-- "Owner · Last used · Expires", and the basis of Disconnect
-- (engines/channel_credentials.go, engines/connector_disconnect.go).
--
-- Additive: three nullable columns on each credential table. A row saved
-- before this migration shows no owner and no last use until it is saved or
-- read again. db/tenant_schema_sync.go carries the columns to every client
-- schema after -migrate; the loop below does it at once for the ones that
-- exist now.

ALTER TABLE tenant_default.channel_credentials
  ADD COLUMN IF NOT EXISTS saved_by VARCHAR(255),
  ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMP,
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP;
ALTER TABLE tenant_default.pinelabs_credentials
  ADD COLUMN IF NOT EXISTS saved_by VARCHAR(255),
  ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMP,
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP;
ALTER TABLE tenant_default.clevertap_credentials
  ADD COLUMN IF NOT EXISTS saved_by VARCHAR(255),
  ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMP,
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP;
ALTER TABLE tenant_default.unicommerce_credentials
  ADD COLUMN IF NOT EXISTS saved_by VARCHAR(255),
  ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMP,
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP;

DO $$
DECLARE
  schema_rec RECORD;
  tbl TEXT;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    FOREACH tbl IN ARRAY ARRAY['channel_credentials', 'pinelabs_credentials', 'clevertap_credentials', 'unicommerce_credentials']
    LOOP
      IF to_regclass(format('%I.%I', schema_rec.schema_name, tbl)) IS NULL THEN
        CONTINUE;
      END IF;
      EXECUTE format('ALTER TABLE %I.%I ADD COLUMN IF NOT EXISTS saved_by VARCHAR(255), ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMP, ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP', schema_rec.schema_name, tbl);
    END LOOP;
  END LOOP;
END $$;
