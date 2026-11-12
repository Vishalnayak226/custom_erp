-- ---------------------------------------------------------------------------
-- Stage 37.7: Projects & job costing.
--
-- Pre-build audit found no Project/Job concept anywhere, and confirmed the
-- codebase's own conventions point to "Project" as a 4th whole-posting
-- dimension (the CostCenter/Department (26.6.8)/Entity (37.2.1) precedent),
-- not a WIP/running-cost mechanism: every cost-incurring doctype here posts
-- immediately on approval/payment, so there is no pre-GL cost-accumulation
-- stage to attach a running ledger to (unlike item_cost, Stage 37.3, which
-- exists because inventory valuation genuinely needs one). Timesheets/
-- labour-hours-to-project costing would need a new Timesheet doctype plus
-- employee hourly-rate data HR does not have today - a real, separate,
-- larger feature, explicitly out of this stage's scope.
--
-- A real, adjacent gap found while wiring this: PayExpenseClaim
-- (engines/expense.go) posts with NO PostingOptions at all, so
-- ExpenseClaim.department (a field the doctype has had since Stage 1) has
-- never actually reached gl_postings.department. Fixed as part of this
-- stage since it is the same call site being touched for Project.
-- ---------------------------------------------------------------------------

ALTER TABLE tenant_default.gl_postings
    ADD COLUMN IF NOT EXISTS project VARCHAR(50);

CREATE INDEX IF NOT EXISTS idx_gl_postings_project
    ON tenant_default.gl_postings (project, created_at)
    WHERE project IS NOT NULL;

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('Project', 'Finance', 'finance', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Project', 'code', 'Project Code', 'Data', TRUE, NULL, 1),
('Project', 'name', 'Project Name', 'Data', TRUE, NULL, 2),
('Project', 'customer', 'Customer (optional)', 'Data', FALSE, NULL, 3),
('Project', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Project', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Project', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- JournalVoucherOptions.Project (the Entity/37.2.1 precedent) + fixing the
-- ExpenseClaim.department/project posting gap noted above.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('JournalVoucher', 'project', 'Project', 'Link', FALSE, 'Project', 83),
('ExpenseClaim', 'project', 'Project (optional)', 'Link', FALSE, 'Project', 20)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- Stage 35-37 migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
  field_doctypes TEXT[] := ARRAY['Project', 'JournalVoucher', 'ExpenseClaim'];
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'ALTER TABLE IF EXISTS %I.gl_postings ADD COLUMN IF NOT EXISTS project VARCHAR(50)',
      schema_rec.schema_name
    );
    EXECUTE format(
      'CREATE INDEX IF NOT EXISTS idx_gl_postings_project ON %I.gl_postings (project, created_at) WHERE project IS NOT NULL',
      schema_rec.schema_name
    );

    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = 'Project'
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module, module_key = EXCLUDED.module_key, document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = ANY($1)
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label, fieldtype = EXCLUDED.fieldtype, mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options, display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name) USING field_doctypes;

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'Project'
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read, allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update, allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
