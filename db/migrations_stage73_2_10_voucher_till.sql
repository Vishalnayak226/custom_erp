-- ---------------------------------------------------------------------------
-- Stage 73.2.10 - issued vouchers at the till.
--
-- A Voucher gains an optional start date and channel (Any / POS / Online).
-- Both blank keep today's behaviour: usable from issue, everywhere.
--
-- Additive and idempotent, replayed into every tenant schema.
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
      VALUES
        ('Voucher', 'valid_from', 'Valid From (optional)', 'Date', FALSE, NULL, 8),
        ('Voucher', 'channel', 'Where it can be used', 'Select', FALSE, 'Any,POS,Online', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
