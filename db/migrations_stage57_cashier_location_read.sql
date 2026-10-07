-- ---------------------------------------------------------------------------
-- Stage 57 (found recording the POS SOP chapter, 2026-10-07): Cashier can read
-- Location and LegalEntity.
--
-- Stage 53.3 made the till's Store box a Location picker (sellable locations
-- only), but Location's grants were seeded in Stage 17h for HR/Admin and Store
-- Manager only. A Cashier's picker request was therefore refused (GLOBAL-0011),
-- the box offered nothing, and a cashier could not open a till at all - only
-- an admin or store manager could. The Cashier role template (engines/
-- role_templates.go) already declares Core as readable; Location is the Core
-- master the till cannot work without. LegalEntity is the other: the receipt
-- header (Stage 53.12) reads the store's legal entity for its name and GSTIN,
-- and that read was refused the same way. Those are the two rows added here;
-- the rest of Core (Department, CostCenter, HSNCode) a till never reads.
--
-- Read only. Additive and idempotent (ON CONFLICT DO NOTHING), replayed into
-- every tenant schema; an administrator's own change to this row is kept.
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
      VALUES
        ('Cashier', 'Location', TRUE, FALSE, FALSE, FALSE),
        ('Cashier', 'LegalEntity', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
