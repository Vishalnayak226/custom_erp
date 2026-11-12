-- ---------------------------------------------------------------------------
-- Stage 106.6 (user, 2026-10-19): "Once I switch Store. It will only show txn
-- of that store." Decided the same day: a user who sees every store's
-- documents (an admin) gets "All stores" at the top of the store picker -
-- everything, as before - and picking one store narrows every transaction
-- list to it. users.store_view remembers that choice:
--
--   ''       All stores (the default, so nothing changes at deploy)
--   'store'  lists follow the working store in the top bar
--
-- A user whose role sees only their working store's documents is narrowed
-- either way. Additive; loops every tenant schema; re-running changes nothing.
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
      ALTER TABLE %I.users ADD COLUMN IF NOT EXISTS store_view VARCHAR(10) NOT NULL DEFAULT ''
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
