-- 162.15 - server-side sessions (session-topology ADR, Option B step 1).
--
-- One small row per signed-in session, keyed by the token's jti: who, from
-- where, when it was last used, and whether it was ended (signed out, idle,
-- or by an administrator). apiMiddleware adds the row the first time it sees
-- a session, refuses an ended one, and - only when the client set
-- security.session_idle_minutes - ends one that went unused for longer.
-- TIMESTAMPTZ so idle time never depends on the database session's zone.
-- Bounded: a row is swept a day after its token expired (SweepUserSessions,
-- hourly), never before - a missing row would make a signed-out token live.
-- Additive: a new table; same catch-up block for schemas provisioned earlier.
CREATE TABLE IF NOT EXISTS tenant_default.user_sessions (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(100) NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    address VARCHAR(64) NOT NULL DEFAULT '',
    device VARCHAR(200) NOT NULL DEFAULT '',
    ended_at TIMESTAMPTZ,
    ended_by VARCHAR(100) NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_user_sessions_expires
  ON tenant_default.user_sessions (expires_at);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.user_sessions (LIKE tenant_default.user_sessions INCLUDING ALL)', schema_rec.schema_name);
  END LOOP;
END $$;
