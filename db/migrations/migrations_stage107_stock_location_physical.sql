-- ---------------------------------------------------------------------------
-- Stage 107 (user, 2026-10-19): "Non saleable and Physically Not Available
-- also required. Ex- Intransit will not be avalible"
--
-- A stock location says whether it sells (Stage 85.B) and now also whether its
-- stock is physically in the store. Stored as not_physical (default FALSE) so
-- every location, and every caller that does not send it, stays "in the
-- store". In-Transit is the one built-in that is not: it is already outside
-- on_hand, this only says so. A client's own location may be marked either way;
-- a stock count's expected qty leaves out what sits in one marked not here.
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
       WHERE table_schema = schema_rec.schema_name AND table_name = 'stock_locations');

    EXECUTE format($sql$
      ALTER TABLE %I.stock_locations ADD COLUMN IF NOT EXISTS not_physical BOOLEAN NOT NULL DEFAULT FALSE
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.stock_locations SET not_physical = TRUE WHERE bucket = 'in_transit' AND NOT not_physical
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
