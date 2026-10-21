-- ---------------------------------------------------------------------------
-- Stage 125.8 (user, 2026-10-21: "Yes. Bring that" - remember this device).
--
-- A browser remembered after a correct authenticator code. Only the SHA-256
-- of its device token is kept; the browser holds the token. A row stands in
-- for the 6-digit code (never the password) while it is younger than the
-- client's security.mfa_remember_days, before expires_at, and while the
-- account's credential_version still matches - so a password change ends it.
-- A 2FA reset or a new authenticator deletes the user's rows.
-- See engines/mfa_trusted_devices.go.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  s RECORD;
BEGIN
  FOR s IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.users', s.schema_name)) IS NULL;

    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.mfa_trusted_devices (
        id                 BIGSERIAL PRIMARY KEY,
        user_id            VARCHAR(100) NOT NULL,
        token_hash         VARCHAR(64)  NOT NULL UNIQUE,
        credential_version INTEGER      NOT NULL,
        label              VARCHAR(160),
        created_at         TIMESTAMP    NOT NULL DEFAULT NOW(),
        last_used_at       TIMESTAMP,
        expires_at         TIMESTAMP    NOT NULL
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS mfa_trusted_devices_user_idx ON %I.mfa_trusted_devices (user_id)', s.schema_name);
  END LOOP;
END
$mig$;
