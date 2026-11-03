-- Stage 49.11.7 - privileged sessions.
--
-- One small row per sign-in of a privileged user (a full-access role): who,
-- when, from which address and device (as 49.11.2 records them), last seen,
-- and whether an administrator ended it. The token jti is the key, so one
-- session can be ended without signing the person out everywhere.
-- Bounded: pruned after 30 days (SweepPrivilegedSessions, hourly).
-- Additive: a new table; same catch-up block for schemas provisioned earlier.
CREATE TABLE IF NOT EXISTS tenant_default.privileged_sessions (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(100) NOT NULL,
    username VARCHAR(100) NOT NULL DEFAULT '',
    role VARCHAR(100) NOT NULL DEFAULT '',
    started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    address VARCHAR(64) NOT NULL DEFAULT '',
    device VARCHAR(200) NOT NULL DEFAULT '',
    credential_version INTEGER NOT NULL DEFAULT 0,
    ended_at TIMESTAMP,
    ended_by VARCHAR(100) NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_privileged_sessions_started
  ON tenant_default.privileged_sessions (started_at DESC);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.privileged_sessions (LIKE tenant_default.privileged_sessions INCLUDING ALL)', schema_rec.schema_name);
  END LOOP;
END $$;
