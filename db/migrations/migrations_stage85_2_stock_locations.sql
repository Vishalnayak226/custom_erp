-- ---------------------------------------------------------------------------
-- Stage 85.B (user, 2026-10-18): store stock locations, owned by the ERP.
--
-- "location storewise ... Saleable, Non saleable. I should have that control.
-- It is not wms depenednt. Few client might not ask wms. It is erp's part.
-- WMS has to adopt and know that from erp."
--
-- A master over the stock buckets inventory_availability already has, so no
-- stock row moves (77.0's decision):
--
--   MAIN       available    the store's selling stock - always sellable
--   INWARD     inward       received (GRN / transfer in), not yet on sale  NEW
--   INTRANSIT  in_transit   sent on a transfer, not yet received
--   QCHOLD     qc_hold      waiting for a quality check     - Sellable is yours
--   DAMAGE     damaged      damaged                         - Sellable is yours
--   ONHOLD     hold_qty     held by a hold code (recall, dispute)
--   PURCHRET   rtv          set aside to go back to a vendor              NEW
--
-- plus any number of the client's own locations ("Alteration", "Display"),
-- which hold stock away from sale: their quantities live in
-- stock_location_balance and are summed into inventory_availability.blocked,
-- which every availability formula already subtracts (and nothing else
-- writes). The two new columns default to 0, so every figure stays as it is
-- until stock is moved there.
--
-- store_stock_locations: which locations each store uses. A store with no rows
-- uses every location marked "for new stores", so existing stores need no
-- backfill and a new store picks up the defaults on its own.
--
-- stock_location_defaults: each transaction's from / to location. Seeded with
-- what the code does today, so nothing changes on deploy.
--
-- StockMove: the record of a move between two locations of one store
-- (Stock -> Move Stock). Engine-written: create on StockMove means "may move
-- stock", and the generic record API refuses to write it.
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
       WHERE table_schema = schema_rec.schema_name AND table_name = 'inventory_availability');

    EXECUTE format($sql$
      ALTER TABLE %I.inventory_availability ADD COLUMN IF NOT EXISTS inward INT NOT NULL DEFAULT 0
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      ALTER TABLE %I.inventory_availability ADD COLUMN IF NOT EXISTS rtv INT NOT NULL DEFAULT 0
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.stock_locations (
        code            VARCHAR(30)  PRIMARY KEY,
        name            VARCHAR(80)  NOT NULL,
        bucket          VARCHAR(20)  NOT NULL
                        CONSTRAINT stock_locations_bucket_check CHECK (bucket IN ('available', 'inward', 'in_transit', 'qc_hold', 'damaged', 'hold_qty', 'rtv', 'custom')),
        sellable        BOOLEAN      NOT NULL DEFAULT FALSE,
        applies_to      VARCHAR(12)  NOT NULL DEFAULT 'Both'
                        CONSTRAINT stock_locations_applies_check CHECK (applies_to IN ('Store', 'Warehouse', 'Both')),
        default_for_new BOOLEAN      NOT NULL DEFAULT TRUE,
        active          BOOLEAN      NOT NULL DEFAULT TRUE,
        display_order   INT          NOT NULL DEFAULT 100,
        description     TEXT         NOT NULL DEFAULT '',
        updated_by      VARCHAR(100),
        updated_at      TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);
    -- One location per built-in bucket; the client's own are all 'custom'.
    EXECUTE format($sql$
      CREATE UNIQUE INDEX IF NOT EXISTS stock_locations_bucket_uq ON %I.stock_locations (bucket) WHERE bucket <> 'custom'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.store_stock_locations (
        location_code  VARCHAR(100) NOT NULL,
        stock_location VARCHAR(30)  NOT NULL,
        PRIMARY KEY (location_code, stock_location)
      )
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.stock_location_defaults (
        txn_type      VARCHAR(40)  PRIMARY KEY,
        from_location VARCHAR(30)  NOT NULL DEFAULT '',
        to_location   VARCHAR(30)  NOT NULL DEFAULT '',
        updated_by    VARCHAR(100),
        updated_at    TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.stock_location_balance (
        sku            VARCHAR(100) NOT NULL,
        location_code  VARCHAR(100) NOT NULL,
        stock_location VARCHAR(30)  NOT NULL,
        qty            INT          NOT NULL DEFAULT 0 CONSTRAINT stock_location_balance_qty_check CHECK (qty >= 0),
        updated_at     TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (sku, location_code, stock_location)
      )
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.stock_locations (code, name, bucket, sellable, applies_to, default_for_new, display_order, description) VALUES
        ('MAIN',      'Main',            'available',  TRUE,  'Both',      TRUE, 10, 'The selling stock on the floor and shelves'),
        ('INWARD',    'Inward',          'inward',     FALSE, 'Both',      TRUE, 20, 'Received (goods receipt or transfer in), not yet put on sale'),
        ('INTRANSIT', 'In-Transit',      'in_transit', FALSE, 'Both',      TRUE, 30, 'Sent on a transfer, not yet received'),
        ('QCHOLD',    'QC Hold',         'qc_hold',    FALSE, 'Both',      TRUE, 40, 'Waiting for a quality check'),
        ('DAMAGE',    'Damage',          'damaged',    FALSE, 'Both',      TRUE, 50, 'Damaged stock'),
        ('ONHOLD',    'On Hold',         'hold_qty',   FALSE, 'Both',      TRUE, 60, 'Held by a hold code (recall, dispute)'),
        ('PURCHRET',  'Purchase Return', 'rtv',        FALSE, 'Warehouse', TRUE, 70, 'Set aside to go back to the vendor')
      ON CONFLICT (code) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- Today's behaviour, transaction by transaction.
    EXECUTE format($sql$
      INSERT INTO %I.stock_location_defaults (txn_type, from_location, to_location) VALUES
        ('POS Sale',          'MAIN',      ''),
        ('Goods Receipt',     '',          'MAIN'),
        ('Transfer Out',      'MAIN',      'INTRANSIT'),
        ('Transfer In',       'INTRANSIT', 'MAIN'),
        ('Sales Return',      '',          'QCHOLD'),
        ('Purchase Return',   'MAIN',      ''),
        ('Damage',            'MAIN',      'DAMAGE'),
        ('Stock Move',        'MAIN',      'DAMAGE')
      ON CONFLICT (txn_type) DO NOTHING
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('StockMove', 'Inventory', 'inventory', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('StockMove', 'location',      'Store',          'Data',   TRUE,  NULL, 1),
        ('StockMove', 'sku',           'Item',           'Data',   TRUE,  NULL, 2),
        ('StockMove', 'item_name',     'Item name',      'Data',   FALSE, NULL, 3),
        ('StockMove', 'qty',           'Quantity',       'Number', TRUE,  NULL, 4),
        ('StockMove', 'from_location', 'From location',  'Data',   TRUE,  NULL, 5),
        ('StockMove', 'to_location',   'To location',    'Data',   TRUE,  NULL, 6),
        ('StockMove', 'reason',        'Reason',         'Data',   FALSE, NULL, 7),
        ('StockMove', 'moved_by',      'Moved by',       'Data',   FALSE, NULL, 8)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'StockMove', TRUE, TRUE, FALSE, FALSE),
             ('Store Manager', 'StockMove', TRUE, TRUE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('SM', 'SM', '/', 5, 'ANNUAL', TRUE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
