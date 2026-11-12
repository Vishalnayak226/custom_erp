-- ---------------------------------------------------------------------------
-- Stage 85.D3 (74.2.1; user, 2026-10-18: "Need a advance setion also" - an
-- Advance tab on the till): Customer Advance.
--
-- An advance is stored value tied to one customer, so it is built on the
-- Stage 72.8 stored-value engine rather than beside it: its own profile
-- ("Customer Advance", liability 2640, no expiry, partial use), one instrument
-- per receipt voucher, every balance change an append-only ledger event, and
-- spent at the till through the same tender path a gift card uses - inside
-- the sale's transaction, under a row lock, so two tills cannot spend the
-- same rupee.
--
-- customer_advances: the receipt voucher's own facts (purpose, expected date,
-- goods or services, the GST taken at receipt for services) and the till
-- session it was taken in. customer_advance_refunds: each refund voucher
-- (AVR), its reason, the tender it went back to and its session - so the
-- drawer's expected cash counts both.
--
-- DEC-03/04/05/13 at the BRD's defaults (user, 2026-10-18; the CA confirms
-- the tax setting before go-live): an advance for goods carries no GST at
-- receipt (Notification 66/2017-CT), one for services does; only the same
-- customer may use it; a refund goes back to the original tender with a
-- reason; no expiry.
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
       WHERE table_schema = schema_rec.schema_name AND table_name = 'stored_value_instruments');

    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type)
        VALUES ('2640', 'Customer Advance Liability', 'Liability')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.customer_advances (
        instrument_id TEXT PRIMARY KEY,
        customer_id   TEXT NOT NULL,
        location      TEXT NOT NULL,
        amount_paise  BIGINT NOT NULL CHECK (amount_paise > 0),
        payment_mode  TEXT NOT NULL,
        purpose       TEXT NOT NULL DEFAULT '',
        expected_date DATE,
        supply_type   TEXT NOT NULL DEFAULT 'Goods' CHECK (supply_type IN ('Goods', 'Services')),
        gst_rate      NUMERIC(5,2) NOT NULL DEFAULT 0,
        tax_paise     BIGINT NOT NULL DEFAULT 0,
        interstate    BOOLEAN NOT NULL DEFAULT FALSE,
        pos_session   TEXT,
        created_by    TEXT,
        created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS customer_advances_customer_idx ON %I.customer_advances (customer_id)
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.customer_advance_refunds (
        voucher_number TEXT PRIMARY KEY,
        instrument_id  TEXT NOT NULL,
        amount_paise   BIGINT NOT NULL CHECK (amount_paise > 0),
        payment_mode   TEXT NOT NULL,
        reason         TEXT NOT NULL,
        location       TEXT,
        pos_session    TEXT,
        created_by     TEXT,
        created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    EXECUTE format($sql$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT 'Customer Advance', 'StoredValueProfile', jsonb_build_object(
               'code', 'Customer Advance', 'instrument_type', 'Customer advance', 'number_length', 16, 'number_format', 'Alphanumeric',
               'denomination_mode', 'Variable', 'min_amount', 1, 'max_amount', 0, 'validity_months', 0,
               'reload_allowed', 'No', 'partial_redemption', 'Yes', 'liability_account', '2640', 'status', 'Active'),
             'Active', 'system'
      WHERE NOT EXISTS (SELECT 1 FROM %I.documents WHERE id = 'Customer Advance')
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('POSBILL', 'B', '', 6, 'ANNUAL', TRUE),
               ('CA', 'CA', '', 5, 'ANNUAL', TRUE),
               ('AVR', 'AVR', '', 5, 'ANNUAL', TRUE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
