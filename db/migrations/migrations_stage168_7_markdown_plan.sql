-- ---------------------------------------------------------------------------
-- Markdown calendar: a planned price reduction. A plan names items or a
-- category, a percent off or a fixed price, a date window and the stores it
-- covers (blank = all). The price is resolved when a sale is priced; the item
-- master price is never rewritten. A plain Master doctype. Additive; nothing
-- changes until someone saves a plan and sets it Scheduled.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('MarkdownPlan', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('MarkdownPlan', 'name', 'Name', 'Data', TRUE, NULL, 1),
('MarkdownPlan', 'items', 'Items', 'Data', FALSE, NULL, 2),
('MarkdownPlan', 'category', 'Category', 'Data', FALSE, NULL, 3),
('MarkdownPlan', 'percent_off', 'Percent off', 'Number', FALSE, NULL, 4),
('MarkdownPlan', 'fixed_price', 'Fixed price', 'Number', FALSE, NULL, 5),
('MarkdownPlan', 'start_date', 'Start', 'Date', TRUE, NULL, 6),
('MarkdownPlan', 'end_date', 'End', 'Date', TRUE, NULL, 7),
('MarkdownPlan', 'stores', 'Stores', 'Data', FALSE, NULL, 8),
('MarkdownPlan', 'status', 'Status', 'Select', TRUE, 'Draft,Scheduled,Running,Ended,Cancelled', 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'MarkdownPlan', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'MarkdownPlan', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = 'MarkdownPlan'
      ON CONFLICT (name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'MarkdownPlan'
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'MarkdownPlan'
      ON CONFLICT (role, doctype_name) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
