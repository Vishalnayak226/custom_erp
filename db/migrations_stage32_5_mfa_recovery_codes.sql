-- Stage 32.5: MFA recovery codes + authenticator re-enrollment.
--
-- Context: before this, a lost or replaced phone locked a tenant out of every
-- HR/Admin screen permanently. The only MFA endpoints were enroll/activate/
-- verify (internal/server/routes.go), there were no backup codes anywhere in
-- the tree, and `handleMFAEnroll` is gated on a `mfa_enroll` purpose token
-- that /api/v1/login only issues to an account that is NOT yet enrolled - so
-- an already-enrolled user could not re-enroll from inside the app either.
-- The only way back in was cmd/reset_mfa, i.e. SSH + DB access on a box with
-- no Go toolchain.
--
-- Two additive pieces close that:
--
--   mfa_recovery_codes       - single-use codes issued at enrollment, any one
--                              of which can stand in for a TOTP code at
--                              /auth/mfa/verify. Only the SHA-256 hash is
--                              stored, never the code itself - same posture
--                              as engines/password_reset.go's reset tokens
--                              (a DB leak alone cannot be replayed into a
--                              login). SHA-256 rather than bcrypt because
--                              these are 50-bit random codes, not
--                              user-chosen passwords: there is no dictionary
--                              to slow an attacker down against, and a
--                              single indexed hash lookup beats bcrypt-
--                              comparing every outstanding code in a loop.
--
--   users.mfa_pending_secret - lets an enrolled user move their authenticator
--                              to a new device. The new secret parks here
--                              until a code proves the new device works, and
--                              only then replaces mfa_secret. Deliberately a
--                              separate column: reusing mfa_secret would
--                              disable the working authenticator the moment
--                              re-enrollment started, so an abandoned attempt
--                              would itself cause the lockout this whole
--                              stage exists to prevent.
CREATE TABLE IF NOT EXISTS tenant_default.mfa_recovery_codes (
    id SERIAL PRIMARY KEY,
    user_id VARCHAR(100) NOT NULL,
    code_hash CHAR(64) NOT NULL,           -- SHA-256 hex of the normalized code
    used_at TIMESTAMP,                      -- NULL until redeemed; single-use
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- The verify path looks up (user_id, code_hash) and the profile screen counts
-- unused codes per user; both are served by this one index.
CREATE INDEX IF NOT EXISTS idx_mfa_recovery_codes_user
  ON tenant_default.mfa_recovery_codes (user_id, code_hash);

ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS mfa_pending_secret VARCHAR(64);

-- Existing tenants were provisioned by copying tenant_default, so a change
-- made only there never reaches them - the failure mode Stage 30.2.2 was
-- written to clean up (five tables missing on live). Same DO-block catch-up
-- shape as that migration and as Stage 31.1's.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.mfa_recovery_codes (id SERIAL PRIMARY KEY, user_id VARCHAR(100) NOT NULL, code_hash CHAR(64) NOT NULL, used_at TIMESTAMP, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)', schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_mfa_recovery_codes_user ON %I.mfa_recovery_codes (user_id, code_hash)', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE %I.users ADD COLUMN IF NOT EXISTS mfa_pending_secret VARCHAR(64)', schema_rec.schema_name);
  END LOOP;
END $$;
