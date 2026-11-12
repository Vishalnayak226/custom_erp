-- ---------------------------------------------------------------------------
-- Stage 65.3 (user, 2026-10-16): the parent of a design in the PO.
--
-- "Item name can be made in two parts. Ask Category and Product Type", and
-- "HSN once defined for an item name never ask". So:
--
-- 1. Category and ProductType - two small Master Data lists (code numbered on
--    the server, name, status).
-- 2. ItemName - Category + Product Type ("Necklaces" + "Antique" =
--    "Necklaces Antique", name editable) carrying the HSN code and GST rate
--    every design under it inherits.
-- 3. ProductFamily (the design) gains the item name it belongs to and the
--    design number the buyer types (the vendor's design no, say) - its code
--    stays the system's own Design/... id.
--
-- engines/po_design.go numbers the three masters and fills ItemName's name
-- and a design's HSN / GST; this file only declares the fields. Additive and
-- idempotent, replayed into every tenant schema; nothing existing is changed.
-- ---------------------------------------------------------------------------
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
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('Category', 'Master Data', 'master_data', 'Master'),
             ('ProductType', 'Master Data', 'master_data', 'Master'),
             ('ItemName', 'PIM', 'pim', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('Category', 'code', 'Code', 'Data', TRUE, NULL, 1),
        ('Category', 'name', 'Category', 'Data', TRUE, NULL, 2),
        ('Category', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3),
        ('ProductType', 'code', 'Code', 'Data', TRUE, NULL, 1),
        ('ProductType', 'name', 'Product Type', 'Data', TRUE, NULL, 2),
        ('ProductType', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3),
        ('ItemName', 'code', 'Code', 'Data', TRUE, NULL, 1),
        ('ItemName', 'category', 'Category', 'Link', TRUE, 'Category', 2),
        ('ItemName', 'product_type', 'Product Type', 'Link', TRUE, 'ProductType', 3),
        ('ItemName', 'name', 'Item Name', 'Data', TRUE, NULL, 4),
        ('ItemName', 'hsn_code', 'HSN Code', 'Data', TRUE, NULL, 5),
        ('ItemName', 'gst_rate', 'GST Rate (%%)', 'Number', TRUE, NULL, 6),
        ('ItemName', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7),
        ('ProductFamily', 'item_name', 'Item Name', 'Link', FALSE, 'ItemName', 16),
        ('ProductFamily', 'design_number', 'Design Number', 'Data', FALSE, NULL, 17)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- Same shape as ProductFamily's own rows: admins manage them, the store
    -- reads them.
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'Category', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'ProductType', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'ItemName', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'Category', TRUE, FALSE, FALSE, FALSE),
        ('Store Manager', 'ProductType', TRUE, FALSE, FALSE, FALSE),
        ('Store Manager', 'ItemName', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('Category', 'CAT', '/', 6, 'ANNUAL', TRUE),
               ('ProductType', 'PTYPE', '/', 6, 'ANNUAL', TRUE),
               ('ItemName', 'ITEMNAME', '/', 6, 'ANNUAL', TRUE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
