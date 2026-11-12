-- Stage 49.6.8 - data-subject request lifecycle (access/export/correction/
-- erasure/anonymization/consent-withdrawal), evidenced end to end.
--
-- Legal hold itself is deliberately NOT a new column here: it is stored as
-- an ad-hoc JSONB field (data->>'legal_hold', data->>'legal_hold_reason') on
-- the subject document, the same convention this codebase already uses for
-- fields with no dedicated column (Item.cost_price, per
-- engines/sensitive_fields.go's own header note), set through
-- engines.SetSubjectLegalHold. That means it applies to any doctype with no
-- per-doctype migration - only the request-tracking table below needs one.
CREATE TABLE IF NOT EXISTS tenant_default.data_subject_requests (
    id VARCHAR(100) PRIMARY KEY,
    subject_doctype VARCHAR(100) NOT NULL,
    subject_id VARCHAR(100) NOT NULL,
    -- access, export, correction, erasure, anonymize, consent_withdraw
    request_type VARCHAR(50) NOT NULL,
    -- Pending, Approved, Denied, Completed
    status VARCHAR(50) NOT NULL DEFAULT 'Pending',
    reason TEXT,
    requested_by VARCHAR(100) NOT NULL,
    requested_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Maker-checker: engines.DecideDataSubjectRequest refuses decided_by ==
    -- requested_by, so a completed row proves two distinct accounts touched it.
    decided_by VARCHAR(100),
    decided_at TIMESTAMP,
    decision_note TEXT,
    completed_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_data_subject_requests_subject
  ON tenant_default.data_subject_requests (subject_doctype, subject_id);
CREATE INDEX IF NOT EXISTS idx_data_subject_requests_status
  ON tenant_default.data_subject_requests (status);

-- Existing tenants were provisioned by copying tenant_default, so a change
-- made only there never reaches them - same DO-block catch-up shape as
-- Stage 31.1/32.5/47.1.7.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.data_subject_requests (id VARCHAR(100) PRIMARY KEY, subject_doctype VARCHAR(100) NOT NULL, subject_id VARCHAR(100) NOT NULL, request_type VARCHAR(50) NOT NULL, status VARCHAR(50) NOT NULL DEFAULT ''Pending'', reason TEXT, requested_by VARCHAR(100) NOT NULL, requested_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, decided_by VARCHAR(100), decided_at TIMESTAMP, decision_note TEXT, completed_at TIMESTAMP)', schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_data_subject_requests_subject ON %I.data_subject_requests (subject_doctype, subject_id)', schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS idx_data_subject_requests_status ON %I.data_subject_requests (status)', schema_rec.schema_name);
  END LOOP;
END $$;
