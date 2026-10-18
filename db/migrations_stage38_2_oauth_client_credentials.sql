-- ---------------------------------------------------------------------------
-- Stage 38.2d - OAuth2 client credentials beside the opaque API-key branch.
--
--   * api_credentials.auth_mode: 'api_key' (the 38.2a key, sent on every
--     call) or 'oauth_client' (client_id = key prefix, client_secret = the
--     key; accepted ONLY at POST /api/public/v1/oauth/token, never as a
--     bearer on a data call). Existing rows stay 'api_key'.
--   * api_oauth_tokens: short-lived access tokens. Only a SHA-256 digest is
--     stored; each token is bound to one credential and tenant, carries a
--     subset of the client's scopes, and dies with the client (revocation or
--     rotation) or at its own expiry.
--
-- Additive and idempotent, replayed into every tenant schema.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'api_credentials');

    EXECUTE format($sql$
      ALTER TABLE %I.api_credentials
        ADD COLUMN IF NOT EXISTS auth_mode VARCHAR(20) NOT NULL DEFAULT 'api_key'
          CHECK (auth_mode IN ('api_key', 'oauth_client'))
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.api_oauth_tokens (
        token_hash    CHAR(64) PRIMARY KEY,
        credential_id UUID NOT NULL,
        scopes        JSONB NOT NULL DEFAULT '[]'::jsonb,
        issued_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
        expires_at    TIMESTAMPTZ NOT NULL,
        revoked_at    TIMESTAMPTZ,
        CHECK (jsonb_typeof(scopes) = 'array')
      )
    $sql$, schema_rec.schema_name);

    EXECUTE format(
      'CREATE INDEX IF NOT EXISTS idx_api_oauth_tokens_credential ON %I.api_oauth_tokens (credential_id, expires_at)',
      schema_rec.schema_name);
  END LOOP;
END
$mig$;
