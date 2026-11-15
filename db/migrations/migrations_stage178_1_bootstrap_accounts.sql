-- ---------------------------------------------------------------------------
-- 178.1 (SB-021): close the two bootstrap accounts that still carry the
-- password hash db/migrations/migration.sql shipped with.
--
-- 1. `system` is the internal account audit rows and triggers name as their
--    actor (COALESCE(created_by, 'system')). Nothing signs in as it, and every
--    lookup that names it reads the string, never the users row. It is set
--    Inactive, which stops it signing in; its row stays, so every reference
--    to it keeps resolving.
-- 2. `cashier1` is a real POS login. Its password is never reset: the account
--    is only flagged must_change_password (57.17), and only while it still
--    carries the shipped hash, so the cashier sets their own at the next web
--    sign-in. A password anyone has already set is left alone.
--
-- No password hash is written. Every statement is guarded, so re-running
-- changes nothing and a schema without the users table or the 57.17 column
-- is skipped.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = schema_rec.schema_name AND table_name = 'users') THEN
      CONTINUE;
    END IF;

    EXECUTE format($sql$
      UPDATE %I.users SET status = 'Inactive'
       WHERE id = 'system' AND status = 'Active'
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = schema_rec.schema_name AND table_name = 'users'
                 AND column_name = 'must_change_password') THEN
      EXECUTE format($sql$
        UPDATE %I.users SET must_change_password = TRUE
         WHERE id = 'cashier1' AND must_change_password = FALSE
           AND password_hash = '$2a$10$u2OOnj/nClI2tPmLfyTPpuePXesLvp1oOwzfK4EAmKFNxbYeJzS5u'
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
