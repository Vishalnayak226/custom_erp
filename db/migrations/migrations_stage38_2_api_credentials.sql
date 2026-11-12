-- Stage 38.2 foundation: durable, tenant-scoped public API credentials.
--
-- This is deliberately a raw security table rather than a generic document:
-- generic document reads expose every JSON field, while an API secret hash
-- must never be returned by list/detail APIs. The plaintext key is generated
-- by Go, returned once, and only its SHA-256 digest is stored. SHA-256 is the
-- correct primitive here because the input is 256 bits of crypto/rand entropy,
-- not a human password that needs a slow password KDF.

CREATE TABLE IF NOT EXISTS tenant_default.api_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(120) NOT NULL,
    key_prefix VARCHAR(32) NOT NULL UNIQUE,
    secret_hash CHAR(64) NOT NULL,
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    status VARCHAR(20) NOT NULL DEFAULT 'Active'
        CHECK (status IN ('Active', 'Revoked')),
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    rotated_from UUID,
    created_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (jsonb_typeof(scopes) = 'array')
);

CREATE INDEX IF NOT EXISTS idx_api_credentials_status_expiry
    ON tenant_default.api_credentials (status, expires_at);
CREATE INDEX IF NOT EXISTS idx_api_credentials_rotated_from
    ON tenant_default.api_credentials (rotated_from)
    WHERE rotated_from IS NOT NULL;

-- Existing tenant schemas are independent physical copies. Keep the table
-- shape identical everywhere; engines.ProvisionTenantSchema separately adds
-- api_credentials to the template-clone list for tenants created later.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'CREATE TABLE IF NOT EXISTS %I.api_credentials (LIKE tenant_default.api_credentials INCLUDING ALL)',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
