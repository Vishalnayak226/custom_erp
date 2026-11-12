-- Stage 49.11.6 (optional security-event export) and 49.12.5 (notification
-- decision workflow). Additive: three new tenant tables, no existing row touched.
--
-- security_export_config: ONE row per client (id = 1), off by default. The
-- destination's secret is stored encrypted (CHANNEL_CREDENTIAL_KEY keyring) and
-- never returned. cursor_seq is the audit_logs.seq already delivered; nothing
-- is queued locally - an unsent event simply stays in audit_logs.
CREATE TABLE IF NOT EXISTS tenant_default.security_export_config (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    url TEXT NOT NULL DEFAULT '',
    secret_enc BYTEA,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    cursor_seq BIGINT NOT NULL DEFAULT 0,
    last_success_at TIMESTAMP,
    last_attempt_at TIMESTAMP,
    last_error TEXT NOT NULL DEFAULT '',
    failures INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMP,
    gap_note TEXT NOT NULL DEFAULT '',
    exported_total BIGINT NOT NULL DEFAULT 0,
    updated_by VARCHAR(100) NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- notification_matrix_versions: every save is a NEW version; none is edited.
CREATE TABLE IF NOT EXISTS tenant_default.notification_matrix_versions (
    version INTEGER PRIMARY KEY,
    entries JSONB NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_by VARCHAR(100) NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- notification_decisions: one row per (incident, matrix row). The row keeps
-- the version it used and a copy of that matrix row, so a later version never
-- changes an old decision.
CREATE TABLE IF NOT EXISTS tenant_default.notification_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_ref UUID NOT NULL,
    incident_title VARCHAR(200) NOT NULL,
    discovered_at TIMESTAMP NOT NULL,
    opened_by VARCHAR(100) NOT NULL DEFAULT '',
    opened_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    matrix_version INTEGER NOT NULL,
    matrix_key VARCHAR(60) NOT NULL,
    row_snapshot JSONB NOT NULL,
    assessment_due_at TIMESTAMP,
    target_at TIMESTAMP,
    status VARCHAR(20) NOT NULL DEFAULT 'Open',
    decision VARCHAR(20) NOT NULL DEFAULT 'pending',
    reason TEXT NOT NULL DEFAULT '',
    evidence_ref TEXT NOT NULL DEFAULT '',
    decided_by VARCHAR(100) NOT NULL DEFAULT '',
    decided_at TIMESTAMP,
    approver VARCHAR(100) NOT NULL DEFAULT '',
    approved_by VARCHAR(100) NOT NULL DEFAULT '',
    approved_at TIMESTAMP,
    approval_note TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_notification_decisions_opened
  ON tenant_default.notification_decisions (opened_at DESC);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.security_export_config (LIKE tenant_default.security_export_config INCLUDING ALL)', schema_rec.schema_name);
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.notification_matrix_versions (LIKE tenant_default.notification_matrix_versions INCLUDING ALL)', schema_rec.schema_name);
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.notification_decisions (LIKE tenant_default.notification_decisions INCLUDING ALL)', schema_rec.schema_name);
  END LOOP;
END $$;
