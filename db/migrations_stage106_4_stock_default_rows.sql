-- ---------------------------------------------------------------------------
-- Stage 106.4 (user, 2026-10-19): "Everything should be editable" - Stock
-- Locations > Transactions. Sales Return's "To" becomes where a return checked
-- Good lands. Good returns have always gone to Main; the row was seeded
-- 'QCHOLD' only as a label while it could not be changed, so the seed (never
-- edited - it had no Save) is set to what actually happens. Nothing else moves.
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
       WHERE table_schema = schema_rec.schema_name AND table_name = 'stock_location_defaults');

    EXECUTE format($sql$
      UPDATE %I.stock_location_defaults SET to_location = 'MAIN'
       WHERE txn_type = 'Sales Return' AND to_location = 'QCHOLD' AND updated_by IS NULL
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
