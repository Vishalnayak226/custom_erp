-- ---------------------------------------------------------------------------
-- Stage 134.1c: a starter Reprint reason for the MRP-change sticker reprint.
-- With "Reasons must be picked from the reason codes" on, the hand-over from
-- an MRP change had no code that fitted (lost / damaged / copy) and was
-- refused. RPT-MRP is added where missing, created by 'system' like the
-- 74.1.1 starters; existing codes and reasons are untouched.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');
    EXECUTE format($sql$
      INSERT INTO %1$I.documents (id, doctype, data, status, created_by)
      SELECT 'RPT-MRP', 'ReasonCode', jsonb_build_object('code', 'RPT-MRP', 'description', 'Price (MRP) changed', 'category', 'Reprint',
             'status', 'Active', 'loss_class', 'None', 'evidence_required', 'No', 'when_to_use', 'New labels after an MRP change.'), 'Active', 'system'
       WHERE EXISTS (SELECT 1 FROM %1$I.doctype_meta WHERE name = 'ReasonCode')
      ON CONFLICT (id) DO NOTHING$sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
