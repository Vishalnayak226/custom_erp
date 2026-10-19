-- ---------------------------------------------------------------------------
-- Stage 108 (user, 2026-10-19, New Location form): "Facility Level will be
-- linked to store/warehouse/ho."
--
-- Location.level gains HO and Store, so it can follow the location's Type
-- (the form sets Store -> Store, Warehouse -> Warehouse, HO -> HO; the user
-- may still change it). Company / Division / DC stay, so no stored level
-- loses its option. Only a field still at its shipped options is changed -
-- a tenant that edited the list keeps its own.
--
-- Additive and idempotent, replayed into every tenant schema. No stored
-- value changes.
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
      UPDATE %I.doctype_fields SET options = 'HO,Company,Division,DC,Warehouse,Store'
       WHERE doctype_name = 'Location' AND fieldname = 'level' AND options = 'Company,Division,DC,Warehouse'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
