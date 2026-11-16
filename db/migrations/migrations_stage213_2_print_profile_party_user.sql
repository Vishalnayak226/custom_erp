-- Item 213.2 (user, 2026-11-15: "Same pos will do b2c and b2b bill and it
-- will have different printer or same for diff user ... It will default
-- mapped"): a Print Profile can also name B2B / B2C (B2B = the other party has
-- a GSTIN) and one user (login name). Its Paper list gains Letter and the
-- 80 mm and 58 mm rolls, so a till bill's default paper can be set here.
-- Additive: two new fields; the options change only where they are still
-- the shipped ',A4,A5'. Safe to run twice.
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
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('PrintProfile', 'party_type', 'B2B / B2C',                  'Select', FALSE, 'All,B2B,B2C', 102),
        ('PrintProfile', 'user',       'Only for user (login name)', 'Data',   FALSE, NULL,          103)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = ',A4,A5,Letter,80 mm roll (3 inch),58 mm roll (2 inch)'
       WHERE doctype_name = 'PrintProfile' AND fieldname = 'paper' AND options = ',A4,A5'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
