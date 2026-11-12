-- ---------------------------------------------------------------------------
-- Stage 72.3-72.7 - sales-triggered consumption, the exception queue and Gift
-- With Purchase offers (the user's blueprint, sections 9, 10 and 18.1).
--
--   * BackflushRule (Setup -> Inventory -> Backflush Rule): sold item /
--     attribute / category / item type / every item -> the component the
--     store uses up with it (engines/backflush_rules.go). Numbered BF-0001.
--   * ItemException (Setup -> Inventory -> Item Exception): written by the
--     engine only - a component that went short, a refused non-sellable or
--     promo-only line. Store managers set Status = Resolved with a comment.
--   * Offer: new type "Gift With Purchase" with gift_item / gift_qty
--     (engines/pos_offers.go evaluateGiftOffers). The Select is widened only
--     where it still has the shipped option list.
--
-- Additive and idempotent, replayed into every tenant schema.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('BackflushRule', 'Inventory', 'inventory', 'Master'),
             ('ItemException', 'Inventory', 'inventory', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('BackflushRule', 'code', 'Rule ID', 'Data', TRUE, NULL, 1),
        ('BackflushRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 2),
        ('BackflushRule', 'basis', 'Applies to', 'Select', TRUE, 'Item,Attribute,Category,Item Type,All items', 3),
        ('BackflushRule', 'basis_value', 'Which (item / Size:Large / category / item type)', 'Data', FALSE, NULL, 4),
        ('BackflushRule', 'location', 'Store (blank = every store)', 'Link', FALSE, 'Location', 5),
        ('BackflushRule', 'component_item', 'Consumed item (box, bag, paper)', 'Link', TRUE, 'Item', 6),
        ('BackflushRule', 'qty_per_unit', 'Qty per unit sold', 'Number', FALSE, NULL, 7),
        ('BackflushRule', 'group', 'Group (Box, Butter paper...)', 'Data', FALSE, NULL, 8),
        ('BackflushRule', 'priority', 'Priority (higher wins a tie)', 'Number', FALSE, NULL, 9),
        ('BackflushRule', 'override_allowed', 'Cashier may change it', 'Select', FALSE, 'Yes,No', 10),
        ('BackflushRule', 'reason_required', 'Reason needed when changed', 'Select', FALSE, 'No,Yes', 11),
        ('BackflushRule', 'no_component_allowed', '"None" allowed', 'Select', FALSE, 'Yes,No', 12),
        ('BackflushRule', 'short_stock_action', 'When the store is short', 'Select', FALSE, 'Allow and report,Warn,Block', 13),
        ('BackflushRule', 'effective_from', 'Effective from', 'Date', FALSE, NULL, 14),
        ('BackflushRule', 'effective_to', 'Effective to', 'Date', FALSE, NULL, 15),
        ('ItemException', 'process_type', 'Type', 'Select', FALSE, 'Backflush short stock,Backflush mapping missing,Non-sellable POS attempt,Promo-only POS attempt,Stored value', 1),
        ('ItemException', 'source_doctype', 'Source', 'Data', FALSE, NULL, 2),
        ('ItemException', 'source_id', 'Source number', 'Data', FALSE, NULL, 3),
        ('ItemException', 'sku', 'SKU', 'Data', FALSE, NULL, 4),
        ('ItemException', 'location', 'Store', 'Data', FALSE, NULL, 5),
        ('ItemException', 'error_code', 'Code', 'Data', FALSE, NULL, 6),
        ('ItemException', 'message', 'What happened', 'Data', FALSE, NULL, 7),
        ('ItemException', 'qty', 'Qty', 'Number', FALSE, NULL, 8),
        ('ItemException', 'attempt_count', 'Attempts', 'Number', FALSE, NULL, 9),
        ('ItemException', 'status', 'Status', 'Select', TRUE, 'Open,Logged,Resolved', 10),
        ('ItemException', 'assigned_to', 'Assigned to', 'Data', FALSE, NULL, 11),
        ('ItemException', 'resolution_comment', 'Resolution', 'Data', FALSE, NULL, 12),
        ('Offer', 'gift_item', 'Gift item (Gift With Purchase)', 'Link', FALSE, 'Item', 40),
        ('Offer', 'gift_qty', 'Gift qty (per bill, or per Buy Qty units)', 'Number', FALSE, NULL, 41)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = 'Percentage Off,Flat Off,Buy X Get Y,Bundle Price,Gift With Purchase'
       WHERE doctype_name = 'Offer' AND fieldname = 'offer_type'
         AND options = 'Percentage Off,Flat Off,Buy X Get Y,Bundle Price'
    $sql$, schema_rec.schema_name);

    -- Rules: whoever maintains Items maintains the rules. Exceptions: managers
    -- read and resolve them; nobody creates one by hand.
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, 'BackflushRule', allow_read, allow_create, allow_update, allow_create
        FROM %I.role_permissions WHERE doctype_name = 'Item'
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'ItemException', TRUE, FALSE, TRUE, FALSE),
             ('Store Manager', 'ItemException', TRUE, FALSE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('BackflushRule', 'BF', '-', 4, 'NEVER', FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
