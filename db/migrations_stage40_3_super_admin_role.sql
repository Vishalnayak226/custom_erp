-- ---------------------------------------------------------------------------
-- Stage 40.3: rename the "HR/Admin" role to "Super Admin".
--
-- The name came from db/migration.sql's very first seed, when one person did
-- HR and administration. It has not meant that for a long time - it is the
-- role that can do everything in every module - and the profile chip read
-- like a job title rather than a privilege level.
--
-- This rewrites the stored value everywhere it is a role NAME. It deliberately
-- does NOT touch:
--
--   * the `hr` module key, the HR doctypes (Employee, Attendance, Leave,
--     Payroll...) or the HR menu - those are the real HR module and keep
--     their name;
--   * audit/system log TEXT, which is a historical record of what was
--     written at the time and must not be retro-edited.
--
-- Safety: engines/roles.go's IsSuperAdmin accepts BOTH names permanently, so
-- a session token minted before this ran, an external script still sending
-- "HR/Admin", or a tenant schema restored from an older snapshot all keep
-- working. That is what makes this migration safe to run against live data
-- while sessions are open - nothing is gated on the rename having happened.
--
-- Idempotent: every statement is a WHERE-guarded UPDATE, so re-running is a
-- no-op.
-- ---------------------------------------------------------------------------

-- 1. tenant_default -------------------------------------------------------
UPDATE tenant_default.users
   SET role = 'Super Admin'
 WHERE role = 'HR/Admin';

UPDATE tenant_default.role_permissions
   SET role = 'Super Admin'
 WHERE role = 'HR/Admin';

UPDATE tenant_default.approval_rules
   SET required_role = 'Super Admin'
 WHERE required_role = 'HR/Admin';

-- approval_log.actor_role is who acted, not a historical message string, and
-- the approvals screen groups by it - leaving it split across two spellings
-- would show the same person as two different approvers.
UPDATE tenant_default.approval_log
   SET actor_role = 'Super Admin'
 WHERE actor_role = 'HR/Admin';

-- field_permissions arrived later (Stage 16) and may not exist on a schema
-- that predates it.
DO $$
BEGIN
  IF to_regclass('tenant_default.field_permissions') IS NOT NULL THEN
    UPDATE tenant_default.field_permissions SET role = 'Super Admin' WHERE role = 'HR/Admin';
  END IF;
END $$;

-- 2. Every other provisioned tenant schema --------------------------------
-- Same statements, guarded per table because tenant schemas are provisioned
-- at different points in this project's history and do not all carry every
-- table.
DO $$
DECLARE
  schema_rec RECORD;
  tbl        RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    FOR tbl IN
      SELECT * FROM (VALUES
        ('users',            'role'),
        ('role_permissions', 'role'),
        ('field_permissions','role'),
        ('approval_rules',   'required_role'),
        ('approval_log',     'actor_role')
      ) AS t(table_name, column_name)
    LOOP
      IF to_regclass(format('%I.%I', schema_rec.schema_name, tbl.table_name)) IS NOT NULL THEN
        EXECUTE format(
          'UPDATE %I.%I SET %I = ''Super Admin'' WHERE %I = ''HR/Admin''',
          schema_rec.schema_name, tbl.table_name, tbl.column_name, tbl.column_name);
      END IF;
    END LOOP;
  END LOOP;
END $$;
