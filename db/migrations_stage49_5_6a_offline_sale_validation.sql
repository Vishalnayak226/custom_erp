-- Stage 49.5.6a: offline sales are validated before they post.
--
-- A checkout the till sends as rung up offline (offline_synced) no longer
-- posts at checkout. It is held here, Pending, with the till's whole request,
-- until a manager opens it on POS > Offline Sales: Validate (every line's
-- scanned barcode is one of that item's ERP barcodes) posts it through the
-- normal checkout as the cashier's sale; Reject (with a reason) never posts.
--
-- idempotency_key is "<cashier user id>:<idempotency key or cart number>",
-- the checkout's own key, so a queue that replays the same sale twice makes
-- one row. received_at is TIMESTAMPTZ (not the session-local documents
-- clock). Additive: a new table; the DO block reaches client schemas made
-- before it (tenant_schema_sync copies it to any made later).

CREATE TABLE IF NOT EXISTS tenant_default.pos_offline_sales (
  id              TEXT PRIMARY KEY,
  cart_number     TEXT NOT NULL,
  idempotency_key TEXT NOT NULL UNIQUE,
  payload         JSONB NOT NULL,
  payload_digest  TEXT NOT NULL DEFAULT '',
  cashier_user_id TEXT NOT NULL DEFAULT '',
  cashier         TEXT NOT NULL DEFAULT '',
  cashier_role    TEXT NOT NULL DEFAULT '',
  location        TEXT NOT NULL DEFAULT '',
  pos_session     TEXT NOT NULL DEFAULT '',
  store_device    TEXT NOT NULL DEFAULT '',
  sold_at         TIMESTAMPTZ,
  received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  total           NUMERIC(14,2) NOT NULL DEFAULT 0,
  lines_total     INTEGER NOT NULL DEFAULT 0,
  lines_matched   INTEGER NOT NULL DEFAULT 0,
  status          TEXT NOT NULL DEFAULT 'Pending' CHECK (status IN ('Pending', 'Validated', 'Rejected')),
  decided_by      TEXT NOT NULL DEFAULT '',
  decided_at      TIMESTAMPTZ,
  reject_reason   TEXT NOT NULL DEFAULT '',
  bill_number     TEXT NOT NULL DEFAULT '',
  result          JSONB
);
CREATE INDEX IF NOT EXISTS idx_pos_offline_sales_status ON tenant_default.pos_offline_sales (status, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_pos_offline_sales_cart ON tenant_default.pos_offline_sales (cart_number);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.pos_offline_sales (LIKE tenant_default.pos_offline_sales INCLUDING ALL)', schema_rec.schema_name);
  END LOOP;
END $$;
