-- Stage 49.3.6 - privilege-change evidence.
--
-- One row per saved change to a role's record-type grants (Roles screen,
-- POST /api/v1/admin/role-permissions) and per rollback of one. It holds the
-- role's exact grants for every record type the change touched, before and
-- after, so a rollback can put back what was there - and, by comparing with
-- what is there now, can leave alone anything changed again since.
--
-- Evidence, not configuration: the grants themselves stay in role_permissions
-- where every reader already looks. The row's SHA-256 is written into the
-- signed, hash-chained audit log at the same time, so editing a row here is
-- detectable (engines.VerifyRoleGrantChangeEvidence).
--
-- Additive: a new table only, no change to any existing one. Same DO-block
-- catch-up shape as Stage 47.1.7 for schemas provisioned earlier.
CREATE TABLE IF NOT EXISTS tenant_default.role_grant_changes (
    id VARCHAR(100) PRIMARY KEY,
    changed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    changed_by VARCHAR(100) NOT NULL,
    role VARCHAR(100) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    before_state JSONB NOT NULL,
    after_state JSONB NOT NULL,
    -- Every payroll / posting / account-takeover / connector-secret grant
    -- the change gave or took away (engines/privilege_risk.go).
    dangerous_gained JSONB NOT NULL DEFAULT '[]',
    dangerous_removed JSONB NOT NULL DEFAULT '[]',
    sessions_ended INTEGER NOT NULL DEFAULT 0,
    evidence_sha256 VARCHAR(64) NOT NULL,
    -- A rollback is a change of its own; rollback_of names what it undid,
    -- rolled_back_by on the original names the rollback.
    rollback_of VARCHAR(100),
    rolled_back_by VARCHAR(100)
);

CREATE INDEX IF NOT EXISTS idx_role_grant_changes_role
  ON tenant_default.role_grant_changes (role, changed_at DESC);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.role_grant_changes (LIKE tenant_default.role_grant_changes INCLUDING ALL)', schema_rec.schema_name);
  END LOOP;
END $$;
