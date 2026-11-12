-- ---------------------------------------------------------------------------
-- Stage 125.4 (user, 2026-10-21: "Change them to Admin too").
--
-- Super Admin is the platform operator's alone (decision D4). Every client's
-- Super Admin account becomes Admin - the same reach over the business,
-- without setup, users and roles, integrations and keys, or audit and
-- privacy. tenant_default is the operator's own client (app.wholeops.in) and
-- is left alone. Passwords, authenticators and sessions are untouched: the
-- session re-reads the role on its next request.
--
-- A client data change: Invoke-Deploy asks for YES before applying it.
--
--   users             role -> Admin (never the hidden support user)
--   approval_rules    required_role -> Admin, so approvals reach the client
--   field_permissions Super Admin's field rules copied to Admin (kept too,
--                     for the support user)
-- approval_log is history and keeps the role each decision was made under.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  s RECORD;
BEGIN
  FOR s IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
       AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.users', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($f$
        UPDATE %I.users SET role = 'Admin'
         WHERE role IN ('Super Admin', 'HR/Admin')
           AND NOT is_platform_support
      $f$, s.schema_name);
    END IF;

    IF to_regclass(format('%I.approval_rules', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($f$
        UPDATE %I.approval_rules SET required_role = 'Admin'
         WHERE required_role IN ('Super Admin', 'HR/Admin')
      $f$, s.schema_name);
    END IF;

    IF to_regclass(format('%I.field_permissions', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($f$
        INSERT INTO %I.field_permissions (role, doctype_name, fieldname, allow_read, allow_write)
        SELECT 'Admin', doctype_name, fieldname, allow_read, allow_write
          FROM %I.field_permissions
         WHERE role IN ('Super Admin', 'HR/Admin')
        ON CONFLICT DO NOTHING
      $f$, s.schema_name, s.schema_name);
    END IF;
  END LOOP;
END
$mig$;
