-- ---------------------------------------------------------------------------
-- Stage 72.8 - Gift cards / vouchers as stored value (the user's blueprint
-- section 11: "Gift cards/vouchers represent value and an obligation to the
-- holder. They must not be modeled as ordinary merchandise").
--
--   * StoredValueProfile (Setup -> Sales -> Stored Value Profile): number
--     format, fixed or variable amounts, validity, reload, partial use and
--     the liability account (2610, seeded by 72.1).
--   * stored_value_instruments: one row per card - number, profile, status,
--     balance (never negative). Changed only by engines/stored_value.go.
--   * stored_value_ledger: append-only events (ACTIVATE, RELOAD, REDEEM,
--     REVERSAL, BLOCK, UNBLOCK, CANCEL) with the balance after each, a
--     per-card sequence and a unique idempotency key. A trigger refuses any
--     UPDATE or DELETE.
--   * One seeded profile, "Standard Gift Card": 16-digit numeric, any amount from 100
--     to 50,000, valid 12 months, partial use allowed, no reload.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION public.stored_value_ledger_append_only() RETURNS trigger
LANGUAGE plpgsql AS $fn$
BEGIN
  RAISE EXCEPTION 'stored_value_ledger is append-only: a gift card balance changes only by a new event';
END
$fn$;

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
      CREATE TABLE IF NOT EXISTS %I.stored_value_instruments (
        instrument_id   TEXT PRIMARY KEY,
        card_number     TEXT NOT NULL UNIQUE,
        profile         TEXT NOT NULL,
        status          TEXT NOT NULL DEFAULT 'Inactive',
        balance_paise   BIGINT NOT NULL DEFAULT 0 CHECK (balance_paise >= 0),
        funded_paise    BIGINT NOT NULL DEFAULT 0,
        currency        TEXT NOT NULL DEFAULT 'INR',
        expires_on      DATE,
        issued_location TEXT,
        issue_payment_mode TEXT,
        customer_id     TEXT,
        activated_at    TIMESTAMPTZ,
        last_event_seq  INT NOT NULL DEFAULT 0,
        created_by      TEXT,
        created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.stored_value_ledger (
        entry_id            BIGSERIAL PRIMARY KEY,
        instrument_id       TEXT NOT NULL REFERENCES %I.stored_value_instruments(instrument_id),
        event_seq           INT NOT NULL,
        event_type          TEXT NOT NULL,
        amount_paise        BIGINT NOT NULL,
        balance_after_paise BIGINT NOT NULL CHECK (balance_after_paise >= 0),
        currency            TEXT NOT NULL DEFAULT 'INR',
        source_doctype      TEXT,
        source_id           TEXT,
        location            TEXT,
        payment_mode        TEXT,
        user_name           TEXT,
        reason              TEXT,
        idempotency_key     TEXT UNIQUE,
        reverses_entry      BIGINT,
        created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
        UNIQUE (instrument_id, event_seq)
      )
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS stored_value_ledger_source_idx ON %I.stored_value_ledger (source_doctype, source_id)
    $sql$, schema_rec.schema_name);

    IF NOT EXISTS (SELECT 1 FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE n.nspname = schema_rec.schema_name AND c.relname = 'stored_value_ledger' AND t.tgname = 'stored_value_ledger_append_only') THEN
      EXECUTE format($sql$
        CREATE TRIGGER stored_value_ledger_append_only BEFORE UPDATE OR DELETE ON %I.stored_value_ledger
        FOR EACH ROW EXECUTE FUNCTION public.stored_value_ledger_append_only()
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('StoredValueProfile', 'Sales', 'sales', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('StoredValueProfile', 'code', 'Profile', 'Data', TRUE, NULL, 1),
        ('StoredValueProfile', 'instrument_type', 'Type', 'Select', TRUE, 'Gift card,Gift voucher,Store credit', 2),
        ('StoredValueProfile', 'number_length', 'Card number length', 'Number', FALSE, NULL, 3),
        ('StoredValueProfile', 'number_format', 'Card number format', 'Select', FALSE, 'Numeric,Alphanumeric', 4),
        ('StoredValueProfile', 'denomination_mode', 'Amounts', 'Select', TRUE, 'Variable,Fixed', 5),
        ('StoredValueProfile', 'denominations', 'Fixed amounts (comma separated)', 'Data', FALSE, NULL, 6),
        ('StoredValueProfile', 'min_amount', 'Minimum amount', 'Number', FALSE, NULL, 7),
        ('StoredValueProfile', 'max_amount', 'Maximum amount', 'Number', FALSE, NULL, 8),
        ('StoredValueProfile', 'validity_months', 'Valid for (months, blank = no expiry)', 'Number', FALSE, NULL, 9),
        ('StoredValueProfile', 'reload_allowed', 'Add value allowed', 'Select', FALSE, 'No,Yes', 10),
        ('StoredValueProfile', 'partial_redemption', 'Part use allowed', 'Select', FALSE, 'Yes,No', 11),
        ('StoredValueProfile', 'liability_account', 'Liability account', 'Data', FALSE, NULL, 12),
        ('StoredValueProfile', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 13)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'StoredValueProfile', TRUE, TRUE, TRUE, FALSE),
             ('Store Manager', 'StoredValueProfile', TRUE, FALSE, FALSE, FALSE),
             ('Cashier', 'StoredValueProfile', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT 'Standard Gift Card', 'StoredValueProfile', jsonb_build_object(
               'code', 'Standard Gift Card', 'instrument_type', 'Gift card', 'number_length', 16, 'number_format', 'Numeric',
               'denomination_mode', 'Variable', 'min_amount', 100, 'max_amount', 50000, 'validity_months', 12,
               'reload_allowed', 'No', 'partial_redemption', 'Yes', 'liability_account', '2610', 'status', 'Active'),
             'Active', 'system'
      WHERE NOT EXISTS (SELECT 1 FROM %I.documents WHERE id = 'Standard Gift Card')
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
