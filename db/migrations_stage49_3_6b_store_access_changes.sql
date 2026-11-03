-- Stage 49.3.6 (c) - store-access change evidence.
--
-- One row per saved change to where a role's holders, or one user, may work
-- (Roles / Users -> Stores). Same promise as role_grant_changes: the before
-- and after settings, who, why, and a SHA-256 that is also written into the
-- signed audit chain, so an edited row is detectable. Evidence only; the
-- settings themselves stay in role_store_access and users.
-- Additive: a new table. Same catch-up DO block for schemas provisioned earlier.
CREATE TABLE IF NOT EXISTS tenant_default.store_access_changes (
    id VARCHAR(100) PRIMARY KEY,
    changed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    changed_by VARCHAR(100) NOT NULL,
    kind VARCHAR(10) NOT NULL,
    subject VARCHAR(100) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    before_state JSONB NOT NULL,
    after_state JSONB NOT NULL,
    widened BOOLEAN NOT NULL DEFAULT FALSE,
    narrowed BOOLEAN NOT NULL DEFAULT FALSE,
    evidence_sha256 VARCHAR(64) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_store_access_changes_subject
  ON tenant_default.store_access_changes (subject, changed_at DESC);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.store_access_changes (LIKE tenant_default.store_access_changes INCLUDING ALL)', schema_rec.schema_name);
  END LOOP;
END $$;
