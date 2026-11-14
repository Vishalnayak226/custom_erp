-- Stage 170.17: positions and reporting lines. Position is a master (title, department, grade,
-- reports-to position); Employee gains `position` and `manager` (a link to another Employee).
-- The free-text designation and the older reporting_manager text stay. Additive only.

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default'
        OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('Position', 'HR', 'hr', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('Position', 'code', 'Position code', 'Data', TRUE, NULL, 1),
        ('Position', 'title', 'Title', 'Data', TRUE, NULL, 2),
        ('Position', 'department', 'Department', 'Data', FALSE, NULL, 3),
        ('Position', 'grade', 'Grade', 'Data', FALSE, NULL, 4),
        ('Position', 'reports_to', 'Reports to position', 'Link', FALSE, 'Position', 5),
        ('Position', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6),
        ('Employee', 'position', 'Position', 'Link', FALSE, 'Position', 30),
        ('Employee', 'manager', 'Manager', 'Link', FALSE, 'Employee', 31)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin', 'Position', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'Position', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'Position', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
