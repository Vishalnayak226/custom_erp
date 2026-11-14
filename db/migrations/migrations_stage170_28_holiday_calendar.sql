-- Stage 170.28: holiday calendar and weekly offs, per location (blank location = every location without
-- one of its own). With no calendar, leave and attendance count exactly as before. Additive only.

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
        ('HolidayCalendar', 'HR', 'hr', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('HolidayCalendar', 'code', 'Calendar name', 'Data', TRUE, NULL, 1),
        ('HolidayCalendar', 'location', 'Location (blank = all)', 'Data', FALSE, NULL, 2),
        ('HolidayCalendar', 'weekly_off', 'Weekly off days (Sun, Sat...)', 'Data', FALSE, NULL, 3),
        ('HolidayCalendar', 'holidays', 'Holidays', 'JSONTable', FALSE, '[{"key":"date","label":"Date","type":"date","required":true},{"key":"name","label":"Holiday","type":"data"}]', 4),
        ('HolidayCalendar', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin', 'HolidayCalendar', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'HolidayCalendar', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'HolidayCalendar', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
