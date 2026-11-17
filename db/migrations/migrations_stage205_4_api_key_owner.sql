-- 205.4: who an API key acts as and what it may reach. Additive; every column is
-- optional, so a key made before this keeps working with the scopes it has.
-- owner_user_id: the person whose rights are the key's ceiling. modules, doctypes,
-- stores, ip_allow: what was ticked. old_key_ends_at: when a rotated key's old
-- secret stops. claim_*: a developer link waiting for the developer to open it.
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS owner_user_id VARCHAR(100);
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS modules JSONB;
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS doctypes JSONB;
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS can_approve BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS ip_allow JSONB;
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS stores JSONB;
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS old_key_ends_at TIMESTAMPTZ;
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS claim_state VARCHAR(20);
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS claim_token_hash VARCHAR(64);
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ;
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE tenant_default.api_credentials ADD COLUMN IF NOT EXISTS quick_start VARCHAR(60);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS owner_user_id VARCHAR(100)', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS modules JSONB', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS doctypes JSONB', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS can_approve BOOLEAN NOT NULL DEFAULT FALSE', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS ip_allow JSONB', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS stores JSONB', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS old_key_ends_at TIMESTAMPTZ', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS claim_state VARCHAR(20)', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS claim_token_hash VARCHAR(64)', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.api_credentials ADD COLUMN IF NOT EXISTS quick_start VARCHAR(60)', schema_rec.schema_name);
  END LOOP;
END $$;
