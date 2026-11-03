-- Stage 146.1.4: a reorder config can carry a minimum order, a maximum stock
-- and an order multiple. Additive; an empty field behaves exactly as before.

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ReorderPointConfig', 'min_order_qty', 'Min Order Qty', 'Number', FALSE, NULL, 7),
('ReorderPointConfig', 'max_stock_qty', 'Max Stock Qty', 'Number', FALSE, NULL, 8),
('ReorderPointConfig', 'order_multiple', 'Order Multiple', 'Number', FALSE, NULL, 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
       WHERE doctype_name = 'ReorderPointConfig' AND fieldname IN ('min_order_qty', 'max_stock_qty', 'order_multiple')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
