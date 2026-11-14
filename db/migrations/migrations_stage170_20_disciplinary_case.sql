-- Stage 170.20: disciplinary case. Restricted like a grievance: HR and the people named on the case read
-- it. `readers` is kept by the server (the employee, the investigator and the people named); the
-- document API's scope matches on it. Additive only.

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
        ('DisciplinaryCase', 'HR', 'hr', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('DisciplinaryCase', 'code', 'Case no', 'Data', TRUE, NULL, 1),
        ('DisciplinaryCase', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
        ('DisciplinaryCase', 'category', 'Category', 'Select', TRUE, 'Conduct,Attendance,Performance,Safety,Policy breach,Other', 3),
        ('DisciplinaryCase', 'description', 'What happened', 'Data', TRUE, NULL, 4),
        ('DisciplinaryCase', 'investigator', 'Investigator (employee)', 'Link', FALSE, 'Employee', 5),
        ('DisciplinaryCase', 'named_people', 'Named people (employee codes, comma separated)', 'Data', FALSE, NULL, 6),
        ('DisciplinaryCase', 'outcome_notes', 'Outcome', 'Data', FALSE, NULL, 7),
        ('DisciplinaryCase', 'readers', 'Readers', 'Data', FALSE, NULL, 8),
        ('DisciplinaryCase', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin', 'DisciplinaryCase', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'DisciplinaryCase', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'DisciplinaryCase', TRUE, FALSE, FALSE, FALSE),
        ('Cashier', 'DisciplinaryCase', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules');
    EXECUTE format($sql$
      INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
        ('DisciplinaryCase', 0, NULL, 'HR/Admin')
      ON CONFLICT (doctype, min_amount) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
