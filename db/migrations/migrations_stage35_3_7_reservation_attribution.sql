-- Stage 35.3.7: attribute a stock reservation to the order line that made it,
-- and sweep the ones nothing is waiting on any more.
--
-- Before this, inventory_reservation held (sku, location, quantity, type) and
-- nothing else. Two consequences, both real:
--
--  1. The OMS order detail could only match reservations by the (sku, location)
--     pairs an order's lines allocated to, so it could show another order's
--     reservation. Releasing a line's stock released the OLDEST matching row,
--     not provably that line's. Correct in aggregate - the same quantity
--     returned to the same pool - but not attributable.
--
--  2. expires_at was written on every row and read by nothing. Every
--     reservation was effectively permanent, so an abandoned cart hold held
--     stock out of the sellable pool for ever.
--
-- Both columns are nullable: rows written before this migration have no order,
-- and the release path still falls back to the old heuristic for them rather
-- than refusing to release stock it cannot attribute.

ALTER TABLE tenant_default.inventory_reservation
    ADD COLUMN IF NOT EXISTS order_id VARCHAR(100),
    ADD COLUMN IF NOT EXISTS line_id VARCHAR(100);

COMMENT ON COLUMN tenant_default.inventory_reservation.order_id IS
    'SalesOrder this reservation was made for. NULL for a cart/manual hold, and for any row written before Stage 35.3.7.';
COMMENT ON COLUMN tenant_default.inventory_reservation.line_id IS
    'SalesOrderLine this reservation backs. NULL where order_id is NULL.';

-- The release path looks up by line, and the sweeper looks up by order. Both
-- are partial: the overwhelming majority of a busy tenant's rows are attributed,
-- and an index that skips the NULLs stays small.
CREATE INDEX IF NOT EXISTS idx_inventory_reservation_line
    ON tenant_default.inventory_reservation (line_id)
    WHERE line_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_inventory_reservation_order
    ON tenant_default.inventory_reservation (order_id)
    WHERE order_id IS NOT NULL;
-- The sweeper's other half scans unattributed rows by expiry.
CREATE INDEX IF NOT EXISTS idx_inventory_reservation_expiry
    ON tenant_default.inventory_reservation (expires_at)
    WHERE order_id IS NULL;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'ALTER TABLE IF EXISTS %I.inventory_reservation
         ADD COLUMN IF NOT EXISTS order_id VARCHAR(100),
         ADD COLUMN IF NOT EXISTS line_id VARCHAR(100)',
      schema_rec.schema_name
    );
    EXECUTE format(
      'CREATE INDEX IF NOT EXISTS idx_inventory_reservation_line
         ON %I.inventory_reservation (line_id) WHERE line_id IS NOT NULL',
      schema_rec.schema_name
    );
    EXECUTE format(
      'CREATE INDEX IF NOT EXISTS idx_inventory_reservation_order
         ON %I.inventory_reservation (order_id) WHERE order_id IS NOT NULL',
      schema_rec.schema_name
    );
    EXECUTE format(
      'CREATE INDEX IF NOT EXISTS idx_inventory_reservation_expiry
         ON %I.inventory_reservation (expires_at) WHERE order_id IS NULL',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
