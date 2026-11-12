-- ---------------------------------------------------------------------------
-- Stage 148.20 / 148.21: journal lines with their own dimensions, and journal
-- templates.
--
-- gl_postings gets four nullable columns a manual journal line can fill:
-- location (store), employee, tag, line_narration. The cost centre and
-- department columns already exist; a line's own value is written there and a
-- line without one keeps the voucher's, so reports that group by dimension need
-- no change. Nothing is written into any existing row.
--
-- JournalDimensionRule: which dimension an account range must carry on a manual
-- journal line (kind Required), plus the one exception list (kind Exempt). An
-- empty table behaves exactly as today. JournalTemplate: a saved line layout,
-- never posted by itself. Both are new doctypes with no rows.
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_postings') THEN
      EXECUTE format('ALTER TABLE %I.gl_postings ADD COLUMN IF NOT EXISTS location TEXT', schema_rec.schema_name);
      EXECUTE format('ALTER TABLE %I.gl_postings ADD COLUMN IF NOT EXISTS employee TEXT', schema_rec.schema_name);
      EXECUTE format('ALTER TABLE %I.gl_postings ADD COLUMN IF NOT EXISTS tag TEXT', schema_rec.schema_name);
      EXECUTE format('ALTER TABLE %I.gl_postings ADD COLUMN IF NOT EXISTS line_narration TEXT', schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('JournalDimensionRule', 'Finance', 'finance', 'Master'),
             ('JournalTemplate', 'Finance', 'finance', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('JournalDimensionRule', 'kind', 'Kind', 'Select', TRUE, 'Required,Exempt', 1),
        ('JournalDimensionRule', 'account_from', 'Account from', 'Data', TRUE, NULL, 2),
        ('JournalDimensionRule', 'account_to', 'Account to', 'Data', FALSE, NULL, 3),
        ('JournalDimensionRule', 'dimension', 'Dimension', 'Select', FALSE, 'cost_center,department,store,employee,tag', 4),
        ('JournalDimensionRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),
        ('JournalTemplate', 'name', 'Name', 'Data', TRUE, NULL, 1),
        ('JournalTemplate', 'narration', 'Narration', 'Data', FALSE, NULL, 2),
        ('JournalTemplate', 'split_mode', 'Split', 'Select', TRUE, 'Amount,Percent', 3),
        ('JournalTemplate', 'lines', 'Lines', 'Text', TRUE, NULL, 4),
        ('JournalTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('Super Admin', 'JournalDimensionRule', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'JournalDimensionRule', TRUE, TRUE, TRUE, TRUE),
        ('Accountant', 'JournalDimensionRule', TRUE, TRUE, TRUE, TRUE),
        ('Super Admin', 'JournalTemplate', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'JournalTemplate', TRUE, TRUE, TRUE, TRUE),
        ('Accountant', 'JournalTemplate', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'JournalTemplate', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
