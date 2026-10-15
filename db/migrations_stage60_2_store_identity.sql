-- ---------------------------------------------------------------------------
-- Stage 60.2 - store identity: Store Name, Store Code, Short Code, Facility
-- Code (the user, 2026-10-14).
--
--   * Location.facility_code - the code OMS uses for this store. Channel
--     orders resolve a store by it as well as by Store Code
--     (engines.ResolveFacilityCode). Unique per tenant (MASTER-0313).
--   * Short Code is now printed inside document numbers in place of the
--     store code (engines.storeNumberSegment), so it is unique too
--     (MASTER-0312).
--   * Labels: "Location Code"/"Location Name" read as Store Code/Store Name.
--     Only a label still at its shipped text is changed - a tenant that
--     renamed it keeps its own.
--
-- Additive only: no stored value changes.
-- ---------------------------------------------------------------------------

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES ('Location', 'facility_code', 'Facility Code (used by OMS)', 'Data', FALSE, NULL, 13)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET label = 'Store Code'
       WHERE doctype_name = 'Location' AND fieldname = 'code' AND label = 'Location Code'
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET label = 'Store Name'
       WHERE doctype_name = 'Location' AND fieldname = 'name' AND label = 'Location Name'
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET label = 'Short Code (printed in document numbers)'
       WHERE doctype_name = 'Location' AND fieldname = 'short_code' AND label = 'Short Code'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
