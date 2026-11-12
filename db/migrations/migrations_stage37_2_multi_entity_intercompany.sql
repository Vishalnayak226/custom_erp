-- ---------------------------------------------------------------------------
-- Stage 37.2: Multi-entity & intercompany - entity-scoped posting, mirrored
-- intercompany entries, reconciliation, and consolidation with eliminations.
--
-- `LegalEntity` already exists as a Master (Stage 17.9, linked from
-- `Location.legal_entity`) but nothing transacts across entities. This tenant
-- schema model is confirmed one-schema-per-tenant (engines/saas.go
-- ProvisionTenantSchema clones the full table set per tenant), so "multiple
-- entities" here means multiple LegalEntity books living inside ONE tenant's
-- schema - not cross-tenant consolidation, which this codebase has no
-- infrastructure or terminology for.
--
-- Two additive pieces, following the exact precedent Stage 26.6.8's
-- cost_center/department and Stage 37.1.2's currency/exchange_rate columns
-- both already set on gl_postings:
--   1. gl_postings.entity - a nullable whole-posting dimension column, so
--      every existing row and every tenant that never opens this feature is
--      byte-identical to what it is today.
--   2. IntercompanyTransaction - a new Transaction doctype (Draft -> Pending
--      Approval -> Approved -> Posted/Partially Posted, same maker-checker
--      shape as JournalVoucher) that posts TWO balanced PostDoubleEntry calls
--      - one per entity's book, tagged with that entity - sharing one pair of
--      new control accounts (1700 Due from Intercompany / 2500 Due to
--      Intercompany) so per-entity trial balances (engine-side, 37.2.1) and
--      pairwise reconciliation (37.2.3) both fall out of that one dimension
--      plus the IntercompanyTransaction documents themselves, with no second
--      ledger and no per-line dimension needed.
-- ---------------------------------------------------------------------------

ALTER TABLE tenant_default.gl_postings
    ADD COLUMN IF NOT EXISTS entity VARCHAR(50);

COMMENT ON COLUMN tenant_default.gl_postings.entity IS
    'LegalEntity document id this posting belongs to. NULL means unassigned - every pre-Stage-37.2 row and every posting made through a caller that does not pass PostingOptions.Entity.';

CREATE INDEX IF NOT EXISTS idx_gl_postings_entity
    ON tenant_default.gl_postings (entity, created_at)
    WHERE entity IS NOT NULL;

-- Two control accounts an IntercompanyTransaction's mirrored legs always post
-- to, regardless of which operator-chosen account sits on the other side of
-- each leg. Asset/Liability so they net at zero group-wide when every
-- IntercompanyTransaction is fully posted - the exact balance 37.2.3's
-- reconciliation report checks and 37.2.4's elimination removes at
-- consolidation.
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('1700', 'Due from Intercompany', 'Asset'),
('2500', 'Due to Intercompany', 'Liability')
ON CONFLICT (account_code) DO NOTHING;

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('IntercompanyTransaction', 'Finance', 'finance', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- JournalVoucherOptions.Entity (engines/journal_voucher.go) - the same
-- optional whole-voucher dimension cost_center/department already are,
-- registered so the generic form/detail view can show it.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('JournalVoucher', 'entity', 'Entity', 'Link', FALSE, 'LegalEntity', 82)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Mirrors JournalVoucher's own doctype_fields shape (voucher_number/status/
-- total_amount) - registered for generic read/list/detail rendering and so
-- the generic /api/v1/approval/submit|decide endpoints (which resolve a
-- doctype's fields via doctype_meta) work unchanged. Creation goes through
-- the dedicated CreateIntercompanyTransaction engine function/endpoint, the
-- same split JournalVoucher already uses - see engines/intercompany.go.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('IntercompanyTransaction', 'code', 'Transaction Code', 'Data', TRUE, NULL, 1),
('IntercompanyTransaction', 'transaction_date', 'Transaction Date', 'Date', TRUE, NULL, 2),
('IntercompanyTransaction', 'narration', 'Narration', 'Data', TRUE, NULL, 3),
('IntercompanyTransaction', 'from_entity', 'From Entity', 'Link', TRUE, 'LegalEntity', 4),
('IntercompanyTransaction', 'from_account_code', 'From Entity Account (credited)', 'Data', TRUE, NULL, 5),
('IntercompanyTransaction', 'to_entity', 'To Entity', 'Link', TRUE, 'LegalEntity', 6),
('IntercompanyTransaction', 'to_account_code', 'To Entity Account (debited)', 'Data', TRUE, NULL, 7),
('IntercompanyTransaction', 'amount', 'Amount', 'Number', TRUE, NULL, 8),
('IntercompanyTransaction', 'status', 'Status', 'Select', TRUE,
 'Draft,Pending Approval,Approved,Rejected,Posted,Partially Posted', 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'IntercompanyTransaction', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'IntercompanyTransaction', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Every amount routes to HR/Admin - an intercompany posting affects two
-- entities' books at once, the same "inherently the kind of action this
-- engine exists to gate" reasoning JournalVoucher's own rule uses.
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('IntercompanyTransaction', 0, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- Stage 35-37 migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'ALTER TABLE IF EXISTS %I.gl_postings ADD COLUMN IF NOT EXISTS entity VARCHAR(50)',
      schema_rec.schema_name
    );
    EXECUTE format(
      'CREATE INDEX IF NOT EXISTS idx_gl_postings_entity ON %I.gl_postings (entity, created_at) WHERE entity IS NOT NULL',
      schema_rec.schema_name
    );
    EXECUTE format(
      'INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES '
      '(''1700'', ''Due from Intercompany'', ''Asset''), '
      '(''2500'', ''Due to Intercompany'', ''Liability'') '
      'ON CONFLICT (account_code) DO NOTHING',
      schema_rec.schema_name
    );

    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = 'IntercompanyTransaction'
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module, module_key = EXCLUDED.module_key, document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'IntercompanyTransaction'
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label, fieldtype = EXCLUDED.fieldtype, mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options, display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'IntercompanyTransaction'
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read, allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update, allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
      SELECT doctype, min_amount, max_amount, required_role
        FROM tenant_default.approval_rules WHERE doctype = 'IntercompanyTransaction'
      ON CONFLICT (doctype, min_amount) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'JournalVoucher' AND fieldname = 'entity'
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
