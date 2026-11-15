-- 49.2.3: WebAuthn passkeys as a phishing-resistant second factor.
-- Additive and safe to run twice. tenant_default is the template; db/tenant_schema_sync.go
-- copies a missing table into every client schema after each -migrate.
CREATE TABLE IF NOT EXISTS tenant_default.user_passkeys (
    id SERIAL PRIMARY KEY,
    user_id VARCHAR(100) NOT NULL,
    credential_id BYTEA NOT NULL,
    public_key BYTEA NOT NULL,            -- COSE key exactly as the authenticator sent it
    sign_count BIGINT NOT NULL DEFAULT 0,
    aaguid BYTEA,
    label VARCHAR(80) NOT NULL DEFAULT 'Passkey',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_passkeys_credential ON tenant_default.user_passkeys (credential_id);
CREATE INDEX IF NOT EXISTS idx_user_passkeys_user ON tenant_default.user_passkeys (user_id);

-- One-time challenges: a row is consumed (deleted) by the first answer, so a
-- captured answer cannot be replayed. Swept when a new one is issued.
CREATE TABLE IF NOT EXISTS tenant_default.passkey_challenges (
    challenge VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(100) NOT NULL,
    purpose VARCHAR(20) NOT NULL,         -- register | assert
    expires_at TIMESTAMP NOT NULL
);
