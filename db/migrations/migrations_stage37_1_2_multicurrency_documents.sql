-- Stage 37.1.2: transaction currency and functional currency on financial
-- documents and their postings.
--
-- The design constraint that shapes this whole migration: `debit` and `credit`
-- must keep meaning exactly what they mean today - an amount in the tenant's
-- functional currency. Every existing report, trial balance, P&L, GST return
-- and reconciliation sums those two columns, and none of them will be taught
-- about currency. So the transaction-currency amounts arrive in NEW columns
-- beside them, and a single-currency tenant's rows are indistinguishable from
-- what it has today.
--
-- Documents themselves carry their currency in their JSON body (documents.data
-- is schemaless), so no column is needed there - engines/currency_documents.go
-- stamps `currency`, `exchange_rate`, `functional_currency` and `base_amount`
-- at the shared ValidateDocument choke point.

ALTER TABLE tenant_default.gl_postings
    ADD COLUMN IF NOT EXISTS currency VARCHAR(3),
    ADD COLUMN IF NOT EXISTS exchange_rate NUMERIC(18,8),
    ADD COLUMN IF NOT EXISTS transaction_debit NUMERIC(18,4),
    ADD COLUMN IF NOT EXISTS transaction_credit NUMERIC(18,4);

COMMENT ON COLUMN tenant_default.gl_postings.currency IS
    'ISO code of the currency the source document was transacted in. NULL means the functional currency, which is what every pre-Stage-37.1.2 row is.';
COMMENT ON COLUMN tenant_default.gl_postings.exchange_rate IS
    'Rate applied to reach debit/credit from transaction_debit/transaction_credit. NULL or 1 for a functional-currency posting.';
COMMENT ON COLUMN tenant_default.gl_postings.transaction_debit IS
    'Debit in the transaction currency. debit remains the functional-currency amount every existing report sums.';

-- A rate must be positive if it is present at all: a zero or negative rate
-- would silently zero or invert a posting's functional amount.
ALTER TABLE tenant_default.gl_postings
    DROP CONSTRAINT IF EXISTS gl_postings_exchange_rate_positive;
ALTER TABLE tenant_default.gl_postings
    ADD CONSTRAINT gl_postings_exchange_rate_positive
    CHECK (exchange_rate IS NULL OR exchange_rate > 0);

-- Foreign-currency postings are the ones revaluation (37.1.4) and FX reporting
-- (37.1.5) need to find. A partial index keeps the cost off the overwhelming
-- majority of rows, which are functional-currency.
CREATE INDEX IF NOT EXISTS idx_gl_postings_foreign_currency
    ON tenant_default.gl_postings (currency, created_at)
    WHERE currency IS NOT NULL;

-- Make the two fields enterable on the documents that can carry them. Only the
-- doctypes engines/currency_documents.go lists as currency-bearing get them:
-- offering a currency box on a document whose posting path cannot honour it
-- would be a promise the system does not keep.
--
-- exchange_rate is optional everywhere. Left blank, the server resolves it from
-- the ExchangeRate master on the document's own date; filled in, the operator's
-- agreed rate is honoured. Both are recorded on the saved document either way.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('JournalVoucher', 'currency', 'Currency', 'Link', FALSE, 'Currency', 80),
('JournalVoucher', 'exchange_rate', 'Exchange Rate', 'Number', FALSE, NULL, 81),
('SalesInvoice', 'currency', 'Currency', 'Link', FALSE, 'Currency', 80),
('SalesInvoice', 'exchange_rate', 'Exchange Rate', 'Number', FALSE, NULL, 81),
('PurchaseOrder', 'currency', 'Currency', 'Link', FALSE, 'Currency', 80),
('PurchaseOrder', 'exchange_rate', 'Exchange Rate', 'Number', FALSE, NULL, 81),
('VendorInvoice', 'currency', 'Currency', 'Link', FALSE, 'Currency', 80),
('VendorInvoice', 'exchange_rate', 'Exchange Rate', 'Number', FALSE, NULL, 81),
('ExpenseClaim', 'currency', 'Currency', 'Link', FALSE, 'Currency', 80),
('ExpenseClaim', 'exchange_rate', 'Exchange Rate', 'Number', FALSE, NULL, 81)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
       SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
         FROM tenant_default.doctype_fields
        WHERE fieldname IN (''currency'', ''exchange_rate'')
          AND doctype_name IN (''JournalVoucher'',''SalesInvoice'',''PurchaseOrder'',''VendorInvoice'',''ExpenseClaim'')
       ON CONFLICT (doctype_name, fieldname) DO NOTHING',
      schema_rec.schema_name
    );
    EXECUTE format(
      'ALTER TABLE IF EXISTS %I.gl_postings
         ADD COLUMN IF NOT EXISTS currency VARCHAR(3),
         ADD COLUMN IF NOT EXISTS exchange_rate NUMERIC(18,8),
         ADD COLUMN IF NOT EXISTS transaction_debit NUMERIC(18,4),
         ADD COLUMN IF NOT EXISTS transaction_credit NUMERIC(18,4)',
      schema_rec.schema_name
    );
    EXECUTE format(
      'ALTER TABLE IF EXISTS %I.gl_postings DROP CONSTRAINT IF EXISTS gl_postings_exchange_rate_positive',
      schema_rec.schema_name
    );
    EXECUTE format(
      'ALTER TABLE IF EXISTS %I.gl_postings ADD CONSTRAINT gl_postings_exchange_rate_positive
         CHECK (exchange_rate IS NULL OR exchange_rate > 0)',
      schema_rec.schema_name
    );
    EXECUTE format(
      'CREATE INDEX IF NOT EXISTS idx_gl_postings_foreign_currency
         ON %I.gl_postings (currency, created_at) WHERE currency IS NOT NULL',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
