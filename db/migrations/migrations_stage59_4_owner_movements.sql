-- ---------------------------------------------------------------------------
-- Stage 59.4 - the owner travels with every movement.
--
-- GRN.owner_id: the client a receipt was received for; putaway against the
-- GRN (grn_id) records the units as that owner's. CycleCountLine.owner_id:
-- a count of one owner's stock carries the owner onto its ledger line.
-- Field registration only. Additive and idempotent, replayed into every
-- tenant schema.
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
      SELECT v.d, 'owner_id', 'Owner (3PL client, optional)', 'Data', FALSE, NULL, 90
        FROM (VALUES ('GRN'), ('CycleCountLine')) AS v(d)
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = v.d)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
