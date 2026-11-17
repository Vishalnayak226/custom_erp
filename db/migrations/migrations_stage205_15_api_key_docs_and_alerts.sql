-- 205.15-205.21: what an API key needs for a shared docs link, expiry reminders and
-- the failure alert. Additive; every column is optional.
-- docs_token_hash / docs_expires_at: the share-docs link (the token itself is never kept).
-- expiry_reminded_days: the smallest reminder step (30, 7, 1) already sent.
-- failure_alerted_at: when the owner was last told about repeated refusals.
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS docs_token_hash VARCHAR(64);
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS docs_expires_at TIMESTAMPTZ;
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS expiry_reminded_days INTEGER;
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS failure_alerted_at TIMESTAMPTZ;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS docs_token_hash VARCHAR(64)', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS docs_expires_at TIMESTAMPTZ', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS expiry_reminded_days INTEGER', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS failure_alerted_at TIMESTAMPTZ', schema_rec.schema_name);
  END LOOP;
END $$;
