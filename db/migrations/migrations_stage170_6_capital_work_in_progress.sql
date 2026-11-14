-- ---------------------------------------------------------------------------
-- Stage 170.6: capital work in progress. An Asset can sit in status CWIP while
-- vendor bill lines are charged to it (Dr 1450 Capital Work in Progress); the
-- Capitalise step moves the total to the fixed asset account.
--
-- Adds account 1450 (ON CONFLICT DO NOTHING), two Asset display fields and the
-- CWIP option on the Asset status list (only while it still holds the old
-- list). Additive and safe to run twice. The release counts the new
-- gl_accounts row as a data change.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
          ('1450', 'Capital Work in Progress', 'Asset')
        ON CONFLICT DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('Asset', 'cwip_budget',    'CWIP budget',     'Number', FALSE, NULL, 14),
        ('Asset', 'cwip_collected', 'CWIP collected',  'Number', FALSE, NULL, 15)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET options = 'Draft,CWIP,Capitalised,Disposed'
       WHERE doctype_name = 'Asset' AND fieldname = 'status' AND options = 'Draft,Capitalised,Disposed'
    $sql$, schema_rec.schema_name);

  END LOOP;
END $mig$;
