-- Stage 76.10: an offline till numbers its own bills from its own series.
--
-- The store's bill number (Stage 85.D1, POSBILL) is drawn on the server when
-- the sale posts, so a sale rung up offline got its number only when a
-- manager validated it (49.5.6a) - the customer's receipt had none. A till
-- now registers a series while it is online: one row here per store and till
-- (01-99), whose base is prefix + store tag + till, e.g. TJAY01. Offline, the
-- till numbers each sale base + year + running number (TJAY012627000001, at
-- most 16 characters, CGST Rule 46(b)); the server takes that number as the
-- bill number when the sale posts, after checking it is this store's series
-- and not already on another bill.
--
-- Additive: a new table; the DO block reaches client schemas made before it
-- (tenant_schema_sync copies it to any made later).

CREATE TABLE IF NOT EXISTS tenant_default.pos_till_series (
  store      TEXT NOT NULL,
  till       TEXT NOT NULL,
  base       TEXT NOT NULL,
  label      TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (store, till)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pos_till_series_base ON tenant_default.pos_till_series (base);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.pos_till_series (LIKE tenant_default.pos_till_series INCLUDING ALL)', schema_rec.schema_name);
  END LOOP;
END $$;
