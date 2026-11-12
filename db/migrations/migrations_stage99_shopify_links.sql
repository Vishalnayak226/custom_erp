-- ---------------------------------------------------------------------------
-- Stage 99.3 (Shopify, D1: the ERP owns products, prices and stock): which
-- Shopify product, variant and inventory item each ERP item is, per channel,
-- and what was last sent - so publishing again updates rather than
-- duplicates, and prices and stock are sent only when they change.
--
--   shopify_links  one row per (channel, sku): the design it belongs to, the
--                  product / variant / inventory item gids, the last price and
--                  compare-at (MRP) sent, and last_stock = {shopify location
--                  gid: quantity} as last set.
--
-- Additive; loops every tenant schema; re-running changes nothing.
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
      CREATE TABLE IF NOT EXISTS %I.shopify_links (
        channel           VARCHAR(100) NOT NULL,
        sku               VARCHAR(200) NOT NULL,
        design            VARCHAR(200) NOT NULL DEFAULT '',
        product_gid       VARCHAR(200) NOT NULL DEFAULT '',
        variant_gid       VARCHAR(200) NOT NULL DEFAULT '',
        inventory_item_id VARCHAR(200) NOT NULL DEFAULT '',
        last_price        NUMERIC(14,2),
        last_compare_at   NUMERIC(14,2),
        last_stock        JSONB NOT NULL DEFAULT '{}'::jsonb,
        updated_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (channel, sku)
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS shopify_links_product_idx ON %I.shopify_links (channel, product_gid)
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
