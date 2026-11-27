-- Item 267.3: declare MaterialIssue.requested_by. The engine has always stored
-- the requesting user there (engines/material_issue.go); declaring it lets the
-- Material Issue list's "My requests" view filter on the server, so a request
-- past the first page is still found. Additive, safe to run twice; no data
-- changes.
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
        ('MaterialIssue', 'requested_by', 'Requested by', 'Data', FALSE, NULL, 11)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
