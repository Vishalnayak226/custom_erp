-- ---------------------------------------------------------------------------
-- Stage 125.2 (user, 2026-10-20: "I can't add hundred of diff autheticator
-- for diff client").
--
-- The platform operator opens any client from the Control Panel instead of
-- holding a password and an authenticator in each one. The client gets one
-- hidden support user (is_platform_support) whose password nobody knows; a
-- session for it is issued only by redeeming a single-use hand-off code
-- (handoff_token_hash, a SHA-256, valid until handoff_expires_at) that the
-- operator's own signed-in session asked for.
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

    EXECUTE format('ALTER TABLE %I.users ADD COLUMN IF NOT EXISTS is_platform_support BOOLEAN NOT NULL DEFAULT FALSE', s.schema_name);
    EXECUTE format('ALTER TABLE %I.users ADD COLUMN IF NOT EXISTS handoff_token_hash VARCHAR(64)', s.schema_name);
    EXECUTE format('ALTER TABLE %I.users ADD COLUMN IF NOT EXISTS handoff_expires_at TIMESTAMP', s.schema_name);
  END LOOP;
END
$mig$;
