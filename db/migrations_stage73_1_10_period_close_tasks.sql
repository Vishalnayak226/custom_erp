-- ---------------------------------------------------------------------------
-- Stage 73.1.10 - period close with owned tasks and a controlled reopen (D09).
--
-- Before: a period closed with one click against an advisory checklist, and
-- could never be reopened. Decision D09: posted documents are corrected only
-- by reversal entries, never edited; a closed period reopens only through
-- maker-checker (Accountant - the "Finance Head" role template - or Super
-- Admin) with a reason and audit, and re-closes through the same task list.
--
--   period_close_tasks   one row per close task per period (bank, stock
--                        valuation, AP tie-out, AR tie-out, settlement):
--                        owner, status (Open / Done / Not applicable), the
--                        owner's evidence, and the system check as it read
--                        at sign-off. A period closes only when every task
--                        is signed. A reopen resets them for the next round.
--   period_close_log     append-only history: assignments, sign-offs, undo,
--                        close, reopen requested / approved.
--   PeriodReopenRequest  a Transaction doctype routed through the existing
--                        approval engine; approving it reopens the period.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant_default.period_close_tasks (
    id BIGSERIAL PRIMARY KEY,
    period_id UUID NOT NULL REFERENCES tenant_default.accounting_periods(id) ON DELETE CASCADE,
    task_key VARCHAR(40) NOT NULL,
    title VARCHAR(120) NOT NULL,
    owner VARCHAR(150),
    status VARCHAR(20) NOT NULL DEFAULT 'Open' CHECK (status IN ('Open', 'Done', 'Not applicable')),
    evidence TEXT,
    evidence_ref VARCHAR(300),
    check_snapshot TEXT,
    signed_by VARCHAR(150),
    signed_at TIMESTAMP,
    close_round INT NOT NULL DEFAULT 1,
    UNIQUE (period_id, task_key)
);

CREATE TABLE IF NOT EXISTS tenant_default.period_close_log (
    id BIGSERIAL PRIMARY KEY,
    period_id UUID NOT NULL,
    close_round INT NOT NULL DEFAULT 1,
    event VARCHAR(40) NOT NULL,
    task_key VARCHAR(40),
    actor VARCHAR(150) NOT NULL,
    detail TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_period_close_log_period ON tenant_default.period_close_log (period_id, created_at);

ALTER TABLE tenant_default.accounting_periods ADD COLUMN IF NOT EXISTS close_round INT NOT NULL DEFAULT 1;
ALTER TABLE tenant_default.accounting_periods ADD COLUMN IF NOT EXISTS reopened_by VARCHAR(150);
ALTER TABLE tenant_default.accounting_periods ADD COLUMN IF NOT EXISTS reopened_at TIMESTAMP;

INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('PeriodReopenRequest', 'Finance', 'Transaction', 'finance')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PeriodReopenRequest', 'period_id', 'Accounting Period', 'Data', TRUE, NULL, 1),
('PeriodReopenRequest', 'period_name', 'Period Name', 'Data', TRUE, NULL, 2),
('PeriodReopenRequest', 'reason', 'Reason', 'Text', TRUE, NULL, 3),
('PeriodReopenRequest', 'requested_by', 'Requested By', 'Data', FALSE, NULL, 4),
('PeriodReopenRequest', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'PeriodReopenRequest', TRUE, TRUE, FALSE, FALSE),
('Accountant', 'PeriodReopenRequest', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Every reopen routes to the Accountant role (Super Admin may always decide);
-- the maker-checker engine refuses an approval by the requester themself.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('PeriodReopenRequest', 0, NULL, 'Accountant')
ON CONFLICT (doctype, min_amount) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'accounting_periods') THEN
      EXECUTE format($sql$
        CREATE TABLE IF NOT EXISTS %1$I.period_close_tasks (
            id BIGSERIAL PRIMARY KEY,
            period_id UUID NOT NULL REFERENCES %1$I.accounting_periods(id) ON DELETE CASCADE,
            task_key VARCHAR(40) NOT NULL,
            title VARCHAR(120) NOT NULL,
            owner VARCHAR(150),
            status VARCHAR(20) NOT NULL DEFAULT 'Open' CHECK (status IN ('Open', 'Done', 'Not applicable')),
            evidence TEXT,
            evidence_ref VARCHAR(300),
            check_snapshot TEXT,
            signed_by VARCHAR(150),
            signed_at TIMESTAMP,
            close_round INT NOT NULL DEFAULT 1,
            UNIQUE (period_id, task_key))
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        CREATE TABLE IF NOT EXISTS %1$I.period_close_log (
            id BIGSERIAL PRIMARY KEY,
            period_id UUID NOT NULL,
            close_round INT NOT NULL DEFAULT 1,
            event VARCHAR(40) NOT NULL,
            task_key VARCHAR(40),
            actor VARCHAR(150) NOT NULL,
            detail TEXT,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)
      $sql$, schema_rec.schema_name);
      EXECUTE format('CREATE INDEX IF NOT EXISTS idx_period_close_log_period ON %I.period_close_log (period_id, created_at)', schema_rec.schema_name);
      EXECUTE format('ALTER TABLE %I.accounting_periods ADD COLUMN IF NOT EXISTS close_round INT NOT NULL DEFAULT 1', schema_rec.schema_name);
      EXECUTE format('ALTER TABLE %I.accounting_periods ADD COLUMN IF NOT EXISTS reopened_by VARCHAR(150)', schema_rec.schema_name);
      EXECUTE format('ALTER TABLE %I.accounting_periods ADD COLUMN IF NOT EXISTS reopened_at TIMESTAMP', schema_rec.schema_name);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_meta (name, module, document_type, module_key)
        SELECT name, module, document_type, module_key FROM tenant_default.doctype_meta WHERE name = 'PeriodReopenRequest'
        ON CONFLICT (name) DO NOTHING
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'PeriodReopenRequest'
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        INSERT INTO %1$I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'PeriodReopenRequest'
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        INSERT INTO %1$I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT doctype, min_amount, max_amount, required_role FROM tenant_default.approval_rules WHERE doctype = 'PeriodReopenRequest'
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
