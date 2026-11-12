-- ---------------------------------------------------------------------------
-- Stage 146.4.5: a channel's own product listings, pulled through its
-- connector (one row per channel + listing), linked to an ERP item by SKU or
-- barcode, with a per-listing switch for the stock push. Empty until someone
-- pulls; no client data is written. stock_sync defaults to true, so a listing
-- syncs as it does today. Additive; loops every tenant schema; safe to re-run.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                    WHERE table_schema = schema_rec.schema_name AND table_name = 'documents') THEN
      CONTINUE;
    END IF;
    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.channel_listings (
        channel      VARCHAR(100) NOT NULL,
        listing_ref  VARCHAR(250) NOT NULL,
        channel_sku  VARCHAR(250) NOT NULL DEFAULT '',
        barcode      VARCHAR(100) NOT NULL DEFAULT '',
        title        VARCHAR(500) NOT NULL DEFAULT '',
        product_ref  VARCHAR(250) NOT NULL DEFAULT '',
        inventory_ref VARCHAR(250) NOT NULL DEFAULT '',
        price        NUMERIC(14,2) NOT NULL DEFAULT 0,
        compare_at   NUMERIC(14,2) NOT NULL DEFAULT 0,
        sku          VARCHAR(200) NOT NULL DEFAULT '',
        stock_sync   BOOLEAN NOT NULL DEFAULT TRUE,
        state        VARCHAR(20) NOT NULL DEFAULT 'Listed',
        pulled_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        linked_at    TIMESTAMP,
        updated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (channel, listing_ref)
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS channel_listings_sku_idx ON %I.channel_listings (channel, sku)
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
