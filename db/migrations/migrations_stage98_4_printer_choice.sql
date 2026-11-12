-- ---------------------------------------------------------------------------
-- Stage 98.4 (user, 2026-10-19: "ERP will be installed in 100 stores. All
-- printer will be different ... It should ask one time which printer for what
-- txn once then nver ask. But user can change it.").
--
-- PrinterAssignment: which printer a computer uses for each kind of print -
-- computer (as QZ Tray names it) + kind -> a Printer record. Kept on the
-- server (decision D3), so it survives a browser reset and can be seen and
-- changed centrally under Setup > Printing. Saved by the print itself the
-- first time a kind prints on a computer (POST /api/v1/print/qz/assignments);
-- the list is for looking and changing.
--
-- Printer."Default For" gains the document kinds, so a store-wide default can
-- name them too. Each is appended only where it is missing.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
  kind TEXT;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('PrinterAssignment', 'Inventory', 'stickers', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('PrinterAssignment', 'computer',   'Computer (as QZ Tray names it)', 'Data',   TRUE,  NULL, 1),
        ('PrinterAssignment', 'print_kind', 'Kind of print',                  'Select', TRUE,  'Purchase Order,Goods Receipt,Transfer,Sales Invoice,Returns and Notes,Receipt,Sticker,Shipping Label', 2),
        ('PrinterAssignment', 'printer',    'Printer',                        'Link',   TRUE,  'Printer', 3),
        ('PrinterAssignment', 'location',   'Store',                          'Link',   FALSE, 'Location', 4),
        ('PrinterAssignment', 'status',     'Status',                         'Select', TRUE,  'Active,Inactive', 5)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('HR/Admin', 'PrinterAssignment', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'PrinterAssignment', TRUE, TRUE, TRUE, FALSE),
        ('Cashier', 'PrinterAssignment', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    FOREACH kind IN ARRAY ARRAY['Purchase Order', 'Goods Receipt', 'Transfer', 'Sales Invoice', 'Returns and Notes'] LOOP
      EXECUTE format($sql$
        UPDATE %I.doctype_fields
           SET options = options || ',' || $1
         WHERE doctype_name = 'Printer' AND fieldname = 'print_role'
           AND COALESCE(options, '') <> ''
           AND (',' || options || ',') NOT LIKE ('%%,' || $1 || ',%%')
      $sql$, schema_rec.schema_name) USING kind;
    END LOOP;
  END LOOP;
END
$mig$;
