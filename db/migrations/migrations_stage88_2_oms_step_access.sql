-- ---------------------------------------------------------------------------
-- Stage 88.2 (found walking OMS ▸ Pick / Pack / Ship as a Store Manager,
-- 2026-10-21): the Store Manager could create shipping packages, manifests
-- and gate passes but could not read the fulfillment tasks or the courier
-- bookings they come from, so Pick, Pack and Ship (and the old Fulfillment
-- screen before them) failed for the role that runs a store's dispatch.
-- Cashier already had both. Settlements stay with finance roles; its menu
-- entry now shows only to a role that can read them.
--
-- Additive: a tenant's own row for the role is never changed.
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
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('Store Manager', 'FulfillmentTask', TRUE, TRUE, TRUE, FALSE),
             ('Store Manager', 'LogisticsBooking', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
