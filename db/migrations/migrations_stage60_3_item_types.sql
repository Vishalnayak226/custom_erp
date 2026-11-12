-- ---------------------------------------------------------------------------
-- Stage 60.3/60.4 - Item Type master with barcode rules and behaviour
-- (engines/item_types.go). The user, 2026-10-14: "item type default Product;
-- accessory / non-asset / goes free with item / gift / backflush I should be
-- able to define, and the barcode will be fixed for that SKU", with barcode
-- generation configurable per client, per item type and per item.
--
--   * ItemType (Setup -> Item Type): its code is the type's name.
--   * Item.item_type becomes a picker of ItemType (was Select Stock/Fixed
--     Asset). Stored values are NOT rewritten: blank and "Stock" read as
--     Product (engines.ItemTypeName) and are normalised when the Item is next
--     saved. "Fixed Asset" is seeded as a type, so those Items still resolve.
--   * Item.barcode_generate_at / barcode_per_receipt_date - per-Item override,
--     "Item type default" keeps the type's rule.
--   * Item.free_with - the parent SKU a Free With Item type rides along with.
--   * Seven seeded types. The non-Product ones carry "Fixed per SKU" barcodes,
--     as the user asked; Fixed Asset issues none (assets carry an asset tag).
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('ItemType', 'Inventory', 'inventory', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('ItemType', 'code', 'Item Type', 'Data', TRUE, NULL, 1),
        ('ItemType', 'description', 'Description', 'Data', FALSE, NULL, 2),
        ('ItemType', 'barcode_generate_at', 'Generate barcode at', 'Select', FALSE, 'Client default,SKU creation,Goods receipt,Off', 3),
        ('ItemType', 'barcode_per_receipt_date', 'Barcode per receipt date', 'Select', FALSE, 'Client default,Fixed per SKU,New per receipt date', 4),
        ('ItemType', 'sell_at_zero_price', 'Sell at zero price (gift / free)', 'Select', FALSE, 'No,Yes', 5),
        ('ItemType', 'needs_parent_item', 'Only with its parent item (free with item)', 'Select', FALSE, 'No,Yes', 6),
        ('ItemType', 'backflush', 'Backflush (consumed at production completion)', 'Select', FALSE, 'No,Yes', 7),
        ('ItemType', 'fixed_asset', 'Fixed asset (bought, never sold)', 'Select', FALSE, 'No,Yes', 8),
        ('ItemType', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- Whoever can read Items can read their types (the Item form's picker);
    -- only roles that can create Items may change types.
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, 'ItemType', allow_read, allow_create, allow_update, FALSE
        FROM %I.role_permissions WHERE doctype_name = 'Item'
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    -- Item.item_type: Select -> Link ItemType. Only the shipped shape is
    -- converted; a tenant that reshaped the field keeps its own.
    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET fieldtype = 'Link', options = 'ItemType', label = 'Item Type'
       WHERE doctype_name = 'Item' AND fieldname = 'item_type'
         AND fieldtype = 'Select' AND options = 'Stock,Fixed Asset'
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT v.* FROM (VALUES
        ('Item', 'item_type', 'Item Type', 'Link', FALSE, 'ItemType', 40),
        ('Item', 'barcode_generate_at', 'Generate barcode at', 'Select', FALSE, 'Item type default,SKU creation,Goods receipt,Off', 41),
        ('Item', 'barcode_per_receipt_date', 'Barcode per receipt date', 'Select', FALSE, 'Item type default,Fixed per SKU,New per receipt date', 42),
        ('Item', 'free_with', 'Free with item', 'Link', FALSE, 'Item', 43)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'Item')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    EXECUTE format($sql$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT t.code, 'ItemType', jsonb_build_object(
               'code', t.code, 'description', t.description,
               'barcode_generate_at', t.gen_at, 'barcode_per_receipt_date', t.per_date,
               'sell_at_zero_price', t.zero_price, 'needs_parent_item', t.needs_parent,
               'backflush', t.backflush, 'fixed_asset', t.fixed_asset, 'status', 'Active'),
             'Active', 'system'
        FROM (VALUES
          ('Product',        'Default - a stock item bought and sold',              'Client default', 'Client default', 'No',  'No',  'No',  'No'),
          ('Accessory',      'Sold alongside a main product',                        'Client default', 'Fixed per SKU',  'No',  'No',  'No',  'No'),
          ('Non-Asset',      'Stock item that is not a fixed asset (consumables)',   'Client default', 'Fixed per SKU',  'No',  'No',  'No',  'No'),
          ('Free With Item', 'Given free with its parent item',                      'Client default', 'Fixed per SKU',  'Yes', 'Yes', 'No',  'No'),
          ('Gift',           'Given free - sells at zero price',                     'Client default', 'Fixed per SKU',  'Yes', 'No',  'No',  'No'),
          ('Backflush',      'Consumed automatically when production completes',     'Client default', 'Fixed per SKU',  'No',  'No',  'Yes', 'No'),
          ('Fixed Asset',    'Bought on a PO and raised as an Asset, never sold',    'Off',            'Fixed per SKU',  'No',  'No',  'No',  'Yes')
        ) AS t(code, description, gen_at, per_date, zero_price, needs_parent, backflush, fixed_asset)
      WHERE NOT EXISTS (SELECT 1 FROM %I.documents d WHERE d.id = t.code)
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
