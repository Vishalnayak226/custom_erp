-- ---------------------------------------------------------------------------
-- Stage 148.36: custom-order configurator (module custom_orders, off by default).
--
-- Four doctypes, all behind the module (a tenant with no entitlement row reads
-- as off, so nothing below is visible until an admin turns the module on):
--   MetalRate        rate per gram for a metal and purity on a day
--   CustomChoice     the small masters: metal, purity, stone, size, making type
--   CustomOrderRule  adds a setting or stone line when a choice is picked
--   CustomOrderQuote what the configurator saved: the rate used and each part
--                    (written by the engine only, read-only to every role)
-- No client row is written. Additive and idempotent, replayed into every tenant.
-- ---------------------------------------------------------------------------
INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('custom_orders', 'Custom orders', 'Made-to-order jewellery: the day''s metal rate, making charge and stone lines priced into an item and a draft order.', FALSE, FALSE)
ON CONFLICT (module_key) DO NOTHING;

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
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('MetalRate', 'Sales', 'custom_orders', 'Master'),
             ('CustomChoice', 'Sales', 'custom_orders', 'Master'),
             ('CustomOrderRule', 'Sales', 'custom_orders', 'Master'),
             ('CustomOrderQuote', 'Sales', 'custom_orders', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('MetalRate', 'metal', 'Metal', 'Data', TRUE, NULL, 1),
        ('MetalRate', 'purity', 'Purity', 'Data', TRUE, NULL, 2),
        ('MetalRate', 'rate_date', 'Date', 'Date', TRUE, NULL, 3),
        ('MetalRate', 'rate_per_gram', 'Rate per gram', 'Number', TRUE, NULL, 4),

        ('CustomChoice', 'kind', 'Kind', 'Select', TRUE, 'Metal,Purity,Stone,Size,Making type', 1),
        ('CustomChoice', 'value', 'Choice', 'Data', TRUE, NULL, 2),
        ('CustomChoice', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3),
        ('CustomChoice', 'making_basis', 'Making charge', 'Select', FALSE, ',Per gram,%% of metal,Flat', 4),
        ('CustomChoice', 'making_value', 'Making value', 'Number', FALSE, NULL, 5),
        ('CustomChoice', 'hsn_code', 'HSN', 'Data', FALSE, NULL, 6),
        ('CustomChoice', 'gst_rate', 'GST %%', 'Number', FALSE, NULL, 7),

        ('CustomOrderRule', 'line_type', 'Line', 'Select', TRUE, 'Setting,Stone', 1),
        ('CustomOrderRule', 'applies_kind', 'When kind', 'Select', TRUE, 'Metal,Purity,Stone,Size,Making type', 2),
        ('CustomOrderRule', 'choice', 'Is', 'Data', TRUE, NULL, 3),
        ('CustomOrderRule', 'label', 'Label', 'Data', TRUE, NULL, 4),
        ('CustomOrderRule', 'basis', 'Charged', 'Select', TRUE, 'Flat,Per stone,Per gram,%% of metal', 5),
        ('CustomOrderRule', 'amount', 'Amount', 'Number', TRUE, NULL, 6),
        ('CustomOrderRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7),

        ('CustomOrderQuote', 'customer_name', 'Customer', 'Data', FALSE, NULL, 1),
        ('CustomOrderQuote', 'description', 'Piece', 'Data', FALSE, NULL, 2),
        ('CustomOrderQuote', 'net_weight', 'Weight (g)', 'Number', FALSE, NULL, 3),
        ('CustomOrderQuote', 'rate_date', 'Rate date', 'Date', FALSE, NULL, 4),
        ('CustomOrderQuote', 'rate_per_gram', 'Rate used', 'Number', FALSE, NULL, 5),
        ('CustomOrderQuote', 'metal_amount', 'Metal', 'Number', FALSE, NULL, 6),
        ('CustomOrderQuote', 'making_amount', 'Making', 'Number', FALSE, NULL, 7),
        ('CustomOrderQuote', 'extra_amount', 'Setting and stones', 'Number', FALSE, NULL, 8),
        ('CustomOrderQuote', 'total', 'Total', 'Number', FALSE, NULL, 9),
        ('CustomOrderQuote', 'item_id', 'Item', 'Data', FALSE, NULL, 10),
        ('CustomOrderQuote', 'order_id', 'Order', 'Data', FALSE, NULL, 11),
        ('CustomOrderQuote', 'status', 'Status', 'Select', TRUE, 'Ordered', 12)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('Super Admin', 'MetalRate', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'MetalRate', TRUE, TRUE, TRUE, FALSE),
        ('Cashier', 'MetalRate', TRUE, FALSE, FALSE, FALSE),
        ('Super Admin', 'CustomChoice', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'CustomChoice', TRUE, TRUE, TRUE, FALSE),
        ('Cashier', 'CustomChoice', TRUE, FALSE, FALSE, FALSE),
        ('Super Admin', 'CustomOrderRule', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'CustomOrderRule', TRUE, TRUE, TRUE, FALSE),
        ('Cashier', 'CustomOrderRule', TRUE, FALSE, FALSE, FALSE),
        ('Super Admin', 'CustomOrderQuote', TRUE, FALSE, FALSE, FALSE),
        ('Store Manager', 'CustomOrderQuote', TRUE, FALSE, FALSE, FALSE),
        ('Cashier', 'CustomOrderQuote', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
