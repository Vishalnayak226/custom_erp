-- Stage 47.1.7 - reversible, reviewed role-template migration.
--
-- The item's constraint is "produce per-tenant before/after grant diff,
-- owner approval and reversible migration. Do not silently reset legitimate
-- custom roles." Nothing in this file changes a single existing grant: it
-- only creates the ledger that makes an application of a role template
-- (engines/role_template_migration.go) reviewable and undoable.
--
-- Deliberately not a schema change to role_permissions itself. The grants
-- stay exactly where every existing reader already looks for them
-- (checkPermission, engines.StoredRoleGrants); this table records what they
-- WERE, so a revert restores the prior state byte for byte rather than
-- re-deriving it from a template that may have moved on.
CREATE TABLE IF NOT EXISTS tenant_default.role_template_migrations (
    id VARCHAR(100) PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- The human who approved it. Owner approval is not a checkbox here: no
    -- code path writes a row without one, so an unattributed grant change
    -- through this mechanism is impossible rather than merely discouraged.
    approved_by VARCHAR(100) NOT NULL,
    roles TEXT NOT NULL,
    -- Every role_permissions row for those roles as it stood immediately
    -- before the change, as a JSON array. A role that had no rows at all is
    -- represented by an empty array, which is what makes "revert" able to
    -- remove grants this migration created rather than leaving them behind.
    previous_state JSONB NOT NULL,
    applied_state JSONB NOT NULL,
    reverted_at TIMESTAMP,
    reverted_by VARCHAR(100)
);

CREATE INDEX IF NOT EXISTS idx_role_template_migrations_applied
  ON tenant_default.role_template_migrations (applied_at DESC);

-- Stage 47.1.4's self scope resolves a session's Employee once per read of a
-- self-scoped doctype (engines.EmployeeCodeForUser -> GetMyEmployeeRecord),
-- which is a data->>'user_id' lookup against the shared documents table. That
-- was already the shape GET /api/v1/hr/my-employee used; making it a scope
-- check on every Payslip/Leave/Grievance read is what turns it into a hot
-- path, so it gets the partial index it never had.
CREATE INDEX IF NOT EXISTS idx_documents_employee_user_id
  ON tenant_default.documents ((data->>'user_id'))
  WHERE doctype = 'Employee';

-- Existing tenants were provisioned by copying tenant_default, so a change
-- made only there never reaches them - same DO-block catch-up shape as
-- Stage 31.1/32.5.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.role_template_migrations (id VARCHAR(100) PRIMARY KEY, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, approved_by VARCHAR(100) NOT NULL, roles TEXT NOT NULL, previous_state JSONB NOT NULL, applied_state JSONB NOT NULL, reverted_at TIMESTAMP, reverted_by VARCHAR(100))', schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_role_template_migrations_applied ON %I.role_template_migrations (applied_at DESC)', schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_documents_employee_user_id ON %I.documents ((data->>''user_id'')) WHERE doctype = ''Employee''', schema_rec.schema_name);
  END LOOP;
END $$;
