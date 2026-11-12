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
-- Idempotent: every statement is WHERE-guarded, so re-running is a no-op.
--
-- MERGE, not blind rename (2026-08-11): a straight
-- `UPDATE ... SET role='Super Admin' WHERE role='HR/Admin'` is NOT safe,
-- because the row it renames into may already exist. Any database that applied
-- the Currency / ExchangeRate / PIMProductGroup migrations before this one
-- already carries 'Super Admin' grants for those doctypes, seeded under the new
-- name, while the other ~111 grants are still 'HR/Admin'. The rename then
-- violates role_permissions' UNIQUE (role, doctype_name) - which is exactly how
-- this failed against the live droplet, mid-deploy, on 2026-08-11.
--
-- The two permission tables therefore merge first (OR the flags together, so
-- the surviving row is never LESS permissive than either input), drop the
-- now-duplicate 'HR/Admin' row, and only then rename what is left. Ordering
-- between this migration and the Currency/PIM ones stops mattering in either
-- direction.
--
-- One loop covers tenant_default and every provisioned tenant schema, so all
-- of them get identical treatment rather than tenant_default having its own
-- hand-written copy that can drift. Each table is guarded by to_regclass
-- because tenant schemas are provisioned at different points in this project's
-- history and do not all carry every table.
-- ---------------------------------------------------------------------------

DO $mig$
DECLARE
  s   RECORD;
  tbl RECORD;
BEGIN
  FOR s IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    -- 1. role_permissions: UNIQUE (role, doctype_name) ---------------------
    IF to_regclass(format('%I.role_permissions', s.schema_name)) IS NOT NULL THEN
      -- Fold the old row's grants into the new row where both exist.
      EXECUTE format($f$
        UPDATE %I.role_permissions sa
           SET allow_read   = sa.allow_read   OR ha.allow_read,
               allow_create = sa.allow_create OR ha.allow_create,
               allow_update = sa.allow_update OR ha.allow_update,
               allow_delete = sa.allow_delete OR ha.allow_delete
          FROM %I.role_permissions ha
         WHERE sa.role = 'Super Admin'
           AND ha.role = 'HR/Admin'
           AND sa.doctype_name = ha.doctype_name
      $f$, s.schema_name, s.schema_name);

      -- Now that its grants are preserved above, the duplicate can go.
      EXECUTE format($f$
        DELETE FROM %I.role_permissions ha
         WHERE ha.role = 'HR/Admin'
           AND EXISTS (
             SELECT 1 FROM %I.role_permissions sa
              WHERE sa.role = 'Super Admin'
                AND sa.doctype_name = ha.doctype_name)
      $f$, s.schema_name, s.schema_name);

      -- Whatever is left has no counterpart and renames cleanly.
      EXECUTE format($f$
        UPDATE %I.role_permissions SET role = 'Super Admin' WHERE role = 'HR/Admin'
      $f$, s.schema_name);
    END IF;

    -- 2. field_permissions: PRIMARY KEY (role, doctype_name, fieldname) ----
    -- Same collision is possible here, so same treatment. This table arrived
    -- in Stage 16 and is absent from schemas that predate it.
    IF to_regclass(format('%I.field_permissions', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($f$
        UPDATE %I.field_permissions sa
           SET allow_read  = sa.allow_read  OR ha.allow_read,
               allow_write = sa.allow_write OR ha.allow_write
          FROM %I.field_permissions ha
         WHERE sa.role = 'Super Admin'
           AND ha.role = 'HR/Admin'
           AND sa.doctype_name = ha.doctype_name
           AND sa.fieldname = ha.fieldname
      $f$, s.schema_name, s.schema_name);

      EXECUTE format($f$
        DELETE FROM %I.field_permissions ha
         WHERE ha.role = 'HR/Admin'
           AND EXISTS (
             SELECT 1 FROM %I.field_permissions sa
              WHERE sa.role = 'Super Admin'
                AND sa.doctype_name = ha.doctype_name
                AND sa.fieldname = ha.fieldname)
      $f$, s.schema_name, s.schema_name);

      EXECUTE format($f$
        UPDATE %I.field_permissions SET role = 'Super Admin' WHERE role = 'HR/Admin'
      $f$, s.schema_name);
    END IF;

    -- 3. Tables where the role column carries no uniqueness constraint -----
    -- users.role, approval_rules.required_role and approval_log.actor_role
    -- are all free of any unique index involving the role, so a plain
    -- WHERE-guarded rename is enough. approval_log.actor_role is who acted,
    -- not a historical message string, and the approvals screen groups by it -
    -- leaving it split across two spellings would show the same person as two
    -- different approvers.
    FOR tbl IN
      SELECT * FROM (VALUES
        ('users',          'role'),
        ('approval_rules', 'required_role'),
        ('approval_log',   'actor_role')
      ) AS t(table_name, column_name)
    LOOP
      IF to_regclass(format('%I.%I', s.schema_name, tbl.table_name)) IS NOT NULL THEN
        EXECUTE format(
          'UPDATE %I.%I SET %I = ''Super Admin'' WHERE %I = ''HR/Admin''',
          s.schema_name, tbl.table_name, tbl.column_name, tbl.column_name);
      END IF;
    END LOOP;
  END LOOP;
END $mig$;
