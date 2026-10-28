-- ---------------------------------------------------------------------------
-- Stage 135.23: clients provisioned after Stage 85.2 have no stock locations.
--
-- 85.2 seeded the built-in locations and transaction defaults into the
-- clients that existed when it ran, but ProvisionTenantSchema never copied
-- them, so every client created since has empty tables and Move Stock refuses
-- every move. Provisioning now copies them (engines/saas.go); this fills the
-- clients already created that way.
--
-- Only a client whose table is empty is touched, so a client that has its
-- locations (or has changed them) is left exactly as it is. The rows are
-- tenant_default's built-in ones - the same source provisioning uses - and a
-- default naming a location the client does not have is left blank.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
  is_empty   BOOLEAN;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                  WHERE table_schema = 'tenant_default' AND table_name = 'stock_location_defaults') THEN
    RETURN;
  END IF;

  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'stock_location_defaults');

    EXECUTE format('SELECT NOT EXISTS (SELECT 1 FROM %I.stock_locations)', schema_rec.schema_name) INTO is_empty;
    IF is_empty THEN
      EXECUTE format($sql$
        INSERT INTO %I.stock_locations (code, name, bucket, sellable, applies_to, default_for_new, active, display_order, description)
        SELECT code, name, bucket, sellable, applies_to, default_for_new, active, display_order, description
          FROM tenant_default.stock_locations WHERE bucket <> 'custom'
        ON CONFLICT DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    EXECUTE format('SELECT NOT EXISTS (SELECT 1 FROM %I.stock_location_defaults)', schema_rec.schema_name) INTO is_empty;
    IF is_empty THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.stock_location_defaults (txn_type, from_location, to_location)
        SELECT d.txn_type,
               CASE WHEN EXISTS (SELECT 1 FROM %1$I.stock_locations l WHERE l.code = d.from_location) THEN d.from_location ELSE '' END,
               CASE WHEN EXISTS (SELECT 1 FROM %1$I.stock_locations l WHERE l.code = d.to_location) THEN d.to_location ELSE '' END
          FROM tenant_default.stock_location_defaults d
        ON CONFLICT DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
