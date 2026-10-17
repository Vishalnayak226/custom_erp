-- ---------------------------------------------------------------------------
-- Stage 67.2 - "I want to stay 12 hr not get logged out ... 12hr default
-- unless user changes" (the user, 2026-10-17). The auto-logout timer every
-- user was seeded with was 30 minutes; the default is now 12 hours
-- (engines/settings_definitions.go security.default_idle_timeout_minutes).
--
-- Only a user still at the shipped 30-minute seed is moved - anyone who chose
-- Never / 15 min / 1 h / 2 h keeps it - and a tenant whose admin explicitly
-- set its own default (a system_settings row exists) is left alone, since
-- its users' 30 is that admin's decision, not the old seed. The ledger runs
-- this once, so a user who picks 30 again afterwards keeps it. The column
-- default follows, for any insert that does not name the column.
-- ---------------------------------------------------------------------------

DO $mig$
DECLARE
  schema_rec RECORD;
  admin_chose_default BOOLEAN;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.columns
      WHERE table_schema = schema_rec.schema_name AND table_name = 'users' AND column_name = 'idle_timeout_minutes');

    EXECUTE format('ALTER TABLE %I.users ALTER COLUMN idle_timeout_minutes SET DEFAULT 720', schema_rec.schema_name);

    admin_chose_default := FALSE;
    IF EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'system_settings') THEN
      EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I.system_settings WHERE key = %L)',
                     schema_rec.schema_name, 'security.default_idle_timeout_minutes')
         INTO admin_chose_default;
    END IF;
    CONTINUE WHEN admin_chose_default;

    EXECUTE format('UPDATE %I.users SET idle_timeout_minutes = 720 WHERE idle_timeout_minutes = 30', schema_rec.schema_name);
  END LOOP;
END
$mig$;
