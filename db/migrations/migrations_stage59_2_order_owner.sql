-- ---------------------------------------------------------------------------
-- Stage 59.2 - owner on demand: one owner (3PL client) per order.
--
-- SalesOrder.owner_id is the client whose stock fills the order (set at
-- intake, or before release with POST /api/v1/wms/order-owner); each
-- FulfillmentTask inherits it at release (engines.ResolveTaskOwnerQ), and
-- bin_allocation rows already carry owner_id (59.1). Field registration only
-- - the values live in documents.data - plus an index for the owner-capped
-- allocation query. Additive and idempotent, replayed into every tenant schema.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT v.d, 'owner_id', v.l, 'Data', FALSE, NULL, 90
        FROM (VALUES ('SalesOrder', 'Owner (3PL client)'), ('FulfillmentTask', 'Owner (3PL client)')) AS v(d, l)
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = v.d)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'bin_allocation') THEN
      EXECUTE format($sql$
        CREATE INDEX IF NOT EXISTS idx_bin_allocation_open_owner
            ON %I.bin_allocation (sku, location_code, owner_id) WHERE status = 'Open'
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
