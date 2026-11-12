-- ---------------------------------------------------------------------------
-- Stage 75.2 (75.2.4): e-invoice (IRN) readiness - the data model.
-- docs/product/einvoice-iris-irp-plan-2026-10-17.md; engines/einvoice*.go.
--
-- irp_profiles   one row per (environment, GSTIN): whether e-invoicing is on,
--                the eligibility rules (effective-dated, versioned), the
--                provider and a reference to its encrypted credentials.
-- irp_documents  one row per tax document's IRN life: the canonical payload
--                frozen at posting with its SHA-256, the state machine, the
--                IRN / Ack and the signed invoice and QR byte-exact. Unique on
--                the legal identity (environment, GSTIN, fiscal year, type,
--                number) and on (environment, GSTIN, IRN): a number is never
--                reused, a sandbox row can never be retried in production.
-- irp_attempts   every call to the provider, redacted (no token, no payload).
-- irp_events     append-only history (a trigger refuses UPDATE and DELETE).
--
-- IRNCancellation / IRNManualImport are the maker-checker records for a
-- cancel and for an IRN generated on the portal during an outage: written by
-- the engine, approved by an Accountant who is not the requester.
--
-- Additive and idempotent; every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %1$I.irp_profiles (
        id              BIGSERIAL PRIMARY KEY,
        environment     VARCHAR(12) NOT NULL DEFAULT 'sandbox',
        gstin           VARCHAR(15) NOT NULL,
        legal_entity    TEXT NOT NULL DEFAULT '',
        provider        VARCHAR(20) NOT NULL DEFAULT 'fake',
        enabled         BOOLEAN NOT NULL DEFAULT FALSE,
        auto_submit     BOOLEAN NOT NULL DEFAULT FALSE,
        turnover_band   VARCHAR(12) NOT NULL DEFAULT '5-10CR',
        document_types  TEXT NOT NULL DEFAULT 'INV,CRN,DBN',
        supply_types    TEXT NOT NULL DEFAULT 'B2B,SEZWP,SEZWOP,EXPWP,EXPWOP,DEXP',
        inter_gstin     VARCHAR(16) NOT NULL DEFAULT 'REQUIRED',
        whitelisted_ip  TEXT NOT NULL DEFAULT '',
        test_status     VARCHAR(40) NOT NULL DEFAULT 'Not tested',
        tested_at       TIMESTAMPTZ,
        effective_from  DATE NOT NULL DEFAULT CURRENT_DATE,
        rule_version    INT NOT NULL DEFAULT 1,
        activated_at    TIMESTAMPTZ,
        updated_by      TEXT NOT NULL DEFAULT '',
        updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        UNIQUE (environment, gstin)
      )$sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %1$I.irp_documents (
        id              BIGSERIAL PRIMARY KEY,
        environment     VARCHAR(12) NOT NULL,
        gstin           VARCHAR(15) NOT NULL,
        fiscal_year     VARCHAR(7) NOT NULL,
        doc_type        VARCHAR(3) NOT NULL,
        doc_number      VARCHAR(40) NOT NULL,
        doc_date        DATE,
        source_doctype  VARCHAR(40) NOT NULL,
        source_id       TEXT NOT NULL,
        state           VARCHAR(20) NOT NULL,
        reason          TEXT NOT NULL DEFAULT '',
        rule_version    INT NOT NULL DEFAULT 0,
        payload         JSONB,
        payload_hash    CHAR(64),
        irn             VARCHAR(64),
        ack_no          VARCHAR(30),
        ack_date        TIMESTAMPTZ,
        signed_invoice  TEXT,
        signed_qr       TEXT,
        source          VARCHAR(12) NOT NULL DEFAULT 'api',
        attempts        INT NOT NULL DEFAULT 0,
        next_attempt_at TIMESTAMPTZ,
        lease_until     TIMESTAMPTZ,
        last_error      TEXT NOT NULL DEFAULT '',
        cancel_reason   TEXT NOT NULL DEFAULT '',
        cancelled_at    TIMESTAMPTZ,
        created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        UNIQUE (environment, gstin, fiscal_year, doc_type, doc_number),
        UNIQUE (environment, source_doctype, source_id)
      )$sql$, schema_rec.schema_name);
    EXECUTE format('CREATE UNIQUE INDEX IF NOT EXISTS irp_documents_irn_uq ON %1$I.irp_documents (environment, gstin, irn) WHERE irn IS NOT NULL', schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS irp_documents_due_idx ON %1$I.irp_documents (state, next_attempt_at)', schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %1$I.irp_attempts (
        id            BIGSERIAL PRIMARY KEY,
        document_id   BIGINT REFERENCES %1$I.irp_documents(id),
        gstin         VARCHAR(15) NOT NULL DEFAULT '',
        action        VARCHAR(16) NOT NULL,
        outcome       VARCHAR(20) NOT NULL,
        error_code    VARCHAR(40) NOT NULL DEFAULT '',
        error_message TEXT NOT NULL DEFAULT '',
        duration_ms   INT NOT NULL DEFAULT 0,
        at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
      )$sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %1$I.irp_events (
        id          BIGSERIAL PRIMARY KEY,
        document_id BIGINT,
        gstin       VARCHAR(15) NOT NULL DEFAULT '',
        event       VARCHAR(40) NOT NULL,
        from_state  VARCHAR(20) NOT NULL DEFAULT '',
        to_state    VARCHAR(20) NOT NULL DEFAULT '',
        detail      TEXT NOT NULL DEFAULT '',
        actor       TEXT NOT NULL DEFAULT '',
        at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
      )$sql$, schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS irp_events_doc_idx ON %1$I.irp_events (document_id)', schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE OR REPLACE FUNCTION %1$I.irp_events_append_only() RETURNS trigger AS $f$
      BEGIN
        RAISE EXCEPTION 'irp_events is append-only';
      END;
      $f$ LANGUAGE plpgsql$sql$, schema_rec.schema_name);
    EXECUTE format('DROP TRIGGER IF EXISTS irp_events_append_only ON %1$I.irp_events', schema_rec.schema_name);
    EXECUTE format('CREATE TRIGGER irp_events_append_only BEFORE UPDATE OR DELETE ON %1$I.irp_events FOR EACH ROW EXECUTE FUNCTION %1$I.irp_events_append_only()', schema_rec.schema_name);

    -- The maker-checker records.
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_meta (name, module, document_type, module_key) VALUES
        ('IRNCancellation', 'Finance', 'Transaction', 'finance'),
        ('IRNManualImport', 'Finance', 'Transaction', 'finance')
      ON CONFLICT (name) DO NOTHING$sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('IRNCancellation', 'irp_document', 'E-invoice record', 'Data', TRUE, NULL, 1),
        ('IRNCancellation', 'document_number', 'Document number', 'Data', FALSE, NULL, 2),
        ('IRNCancellation', 'irn', 'IRN', 'Data', FALSE, NULL, 3),
        ('IRNCancellation', 'cancel_code', 'Reason code', 'Select', TRUE, '1 - Duplicate,2 - Data entry mistake,3 - Order cancelled,4 - Others', 4),
        ('IRNCancellation', 'reason', 'Reason', 'Text', TRUE, NULL, 5),
        ('IRNCancellation', 'requested_by', 'Asked by', 'Data', FALSE, NULL, 6),
        ('IRNCancellation', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Applied', 7),
        ('IRNManualImport', 'irp_document', 'E-invoice record', 'Data', TRUE, NULL, 1),
        ('IRNManualImport', 'document_number', 'Document number', 'Data', FALSE, NULL, 2),
        ('IRNManualImport', 'irn', 'IRN', 'Data', TRUE, NULL, 3),
        ('IRNManualImport', 'ack_no', 'Ack No', 'Data', TRUE, NULL, 4),
        ('IRNManualImport', 'ack_date', 'Ack Date', 'Data', TRUE, NULL, 5),
        ('IRNManualImport', 'signed_qr', 'Signed QR (text)', 'Text', TRUE, NULL, 6),
        ('IRNManualImport', 'reason', 'Why it was made on the portal', 'Text', TRUE, NULL, 7),
        ('IRNManualImport', 'requested_by', 'Entered by', 'Data', FALSE, NULL, 8),
        ('IRNManualImport', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Applied', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('Accountant', 'IRNCancellation', TRUE, FALSE, FALSE, FALSE),
          ('Accountant', 'IRNManualImport', TRUE, FALSE, FALSE, FALSE),
          ('HR/Admin', 'IRNCancellation', TRUE, FALSE, FALSE, FALSE),
          ('HR/Admin', 'IRNManualImport', TRUE, FALSE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING$sql$, schema_rec.schema_name);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
          ('IRNCancellation', 0, NULL, 'Accountant'),
          ('IRNManualImport', 0, NULL, 'Accountant')
        ON CONFLICT (doctype, min_amount) DO NOTHING$sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
