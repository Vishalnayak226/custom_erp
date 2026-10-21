-- ---------------------------------------------------------------------------
-- Stage 74.2.3: a stock adjustment says what kind of loss it is - Shrinkage,
-- Damage, Expiry or Theft - so a Damage or Expiry loss posts through its own
-- posting-profile event (74.1.3: adjustment_damage / adjustment_expiry) and
-- the loss reports can split by it. Blank keeps the plain gain / loss posting.
-- Additive and idempotent, replayed into every tenant schema.
-- This file sorts before 88.5's, which creates InventoryAdjustment, so on a
-- fresh database it creates the same doctype row first (88.5 then skips it).
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
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('InventoryAdjustment', 'Inventory', 'inventory', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES ('InventoryAdjustment', 'loss_class', 'Loss class', 'Select', FALSE, ',Shrinkage,Damage,Expiry,Theft', 40)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
