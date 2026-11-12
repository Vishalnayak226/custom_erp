-- 165.1 (for 49.12.5): the Privacy Approver role keeps its own favourites (the
-- star on a screen, UserShortcut), like every role Stage 139 granted. The role
-- is new (2026-11-12), so migrations_stage139_user_shortcuts.sql never saw it
-- and its sign-in got a 403 reading its favourites. Numbered under 165.1 so it
-- sorts after stage 139: on a fresh database the UserShortcut record type must
-- exist first (role_permissions has a foreign key to doctype_meta), and
-- tenant_default must hold the row so every new client cloned from it does.
-- Additive: one role_permissions row per client, ON CONFLICT DO NOTHING; a
-- client without the record type is skipped; no client document is touched.
DO $mig$
DECLARE
  schema_rec RECORD;
  has_type BOOLEAN;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = ''UserShortcut'')',
                   schema_rec.schema_name) INTO has_type;
    CONTINUE WHEN NOT has_type;
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('Privacy Approver', 'UserShortcut', TRUE, TRUE, TRUE, TRUE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
