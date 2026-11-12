-- ---------------------------------------------------------------------------
-- Stage 148.16: memo / consignment stock (module memo_stock, off by default).
--
--   memo_stock_lots   one row per memo receipt line: the units a vendor still
--                     owns, at the rate agreed. open = received - sold - returned.
--   memo_stock_sales  one row per draw of a lot by a sale: the units, the rate,
--                     and the vendor bill raised for them.
--   GRN.memo_stock    "Memo stock" Yes on the receipt (shown only while the
--                     module is on).
--
-- The module is registered switched OFF in the catalog; a tenant with no
-- entitlement row reads as off, so no client sees a change until an admin turns
-- it on. No per-client row is written. Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('memo_stock', 'Memo stock', 'Vendor-owned stock held at zero value; the sale raises the vendor bill.', FALSE, FALSE)
ON CONFLICT (module_key) DO NOTHING;

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
      CREATE TABLE IF NOT EXISTS %I.memo_stock_lots (
        id            BIGSERIAL PRIMARY KEY,
        grn_id        VARCHAR(200) NOT NULL,
        part          VARCHAR(100) NOT NULL DEFAULT '',
        sku           VARCHAR(200) NOT NULL,
        vendor_id     VARCHAR(200) NOT NULL DEFAULT '',
        location      VARCHAR(200) NOT NULL DEFAULT '',
        qty_received  NUMERIC NOT NULL DEFAULT 0,
        qty_sold      NUMERIC NOT NULL DEFAULT 0,
        qty_returned  NUMERIC NOT NULL DEFAULT 0,
        qty_lost      NUMERIC NOT NULL DEFAULT 0,
        rate_paise    BIGINT NOT NULL DEFAULT 0,
        received_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        UNIQUE (grn_id, part, sku)
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS memo_stock_lots_sku_idx ON %I.memo_stock_lots (sku, received_at)
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.memo_stock_sales (
        id             BIGSERIAL PRIMARY KEY,
        lot_id         BIGINT NOT NULL,
        source_doctype VARCHAR(100) NOT NULL,
        source_id      VARCHAR(200) NOT NULL,
        sku            VARCHAR(200) NOT NULL,
        vendor_id      VARCHAR(200) NOT NULL DEFAULT '',
        qty            NUMERIC NOT NULL DEFAULT 0,
        rate_paise     BIGINT NOT NULL DEFAULT 0,
        bill_id        VARCHAR(200) NOT NULL DEFAULT '',
        created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS memo_stock_sales_source_idx ON %I.memo_stock_sales (source_doctype, source_id)
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT 'GRN', 'memo_stock', 'Memo stock', 'Select', FALSE, ',Yes', 124
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = 'GRN')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
