-- ---------------------------------------------------------------------------
-- Stage 148.6 / 148.7 / 146.4.2: vendor price agreements and PO discounts.
--
-- A vendor agreement is a PriceListVersion with a vendor on it (the sales
-- price-list engine, D2: no second price engine). Its rows are widened so a
-- row can be keyed by item, design or category and carry valid dates, qty
-- breaks, a line discount, the vendor's item code, a pack multiple, a "count
-- qty across the PO" flag, buy-N-get-M and a commitment. The PO gets an order
-- discount; its lines get a line discount. Item gets a default vendor.
--
-- Metadata only (doctype_fields). No document row is written. Safe to re-run;
-- tenant_default first, then every other tenant.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PriceListVersion', 'vendor', 'Vendor (blank = sales list)', 'Link', FALSE, 'Vendor', 8),
('PurchaseOrder', 'order_discount_pct', 'Order discount %', 'Number', FALSE, NULL, 40),
('PurchaseOrder', 'order_discount_amt', 'Order discount', 'Number', FALSE, NULL, 41),
('Item', 'default_vendor', 'Default Vendor', 'Link', FALSE, 'Vendor', 77)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

UPDATE tenant_default.doctype_fields SET options = '[{"key":"sku","label":"Item","type":"link","link":"Item"},
   {"key":"design","label":"Design","type":"data"},
   {"key":"category","label":"Category","type":"data"},
   {"key":"price","label":"Price","type":"number"},
   {"key":"min_qty","label":"Qty from","type":"number"},
   {"key":"max_qty","label":"Qty to","type":"number"},
   {"key":"discount_pct","label":"Discount %","type":"number"},
   {"key":"vendor_code","label":"Vendor code","type":"data"},
   {"key":"pack_multiple","label":"Pack of","type":"number"},
   {"key":"count_across_po","label":"Count across PO","type":"select","options":",Yes"},
   {"key":"buy_qty","label":"Buy","type":"number"},
   {"key":"get_qty","label":"Get free","type":"number"},
   {"key":"valid_from","label":"Valid from","type":"data"},
   {"key":"valid_to","label":"Valid to","type":"data"},
   {"key":"commit_qty","label":"Commit qty","type":"number"},
   {"key":"commit_value","label":"Commit value","type":"number"}]'
WHERE doctype_name = 'PriceListVersion' AND fieldname = 'items';

UPDATE tenant_default.doctype_fields SET options = '[{"key":"sku","label":"Item","type":"link","link":"Item","required":true},
                     {"key":"qty","label":"Qty","type":"number","required":true},
                     {"key":"rate","label":"Purchase Price","type":"number","required":true},
                     {"key":"mrp","label":"MRP","type":"number"},
                     {"key":"discount_pct","label":"Discount %","type":"number"},
                     {"key":"discount_amt","label":"Discount","type":"number"}]'
WHERE doctype_name = 'PurchaseOrder' AND fieldname = 'items';

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
        WHERE (doctype_name, fieldname) IN (('PriceListVersion', 'vendor'), ('PurchaseOrder', 'order_discount_pct'),
                                            ('PurchaseOrder', 'order_discount_amt'), ('Item', 'default_vendor'))
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        UPDATE %1$I.doctype_fields t SET options = d.options
        FROM tenant_default.doctype_fields d
        WHERE t.doctype_name = d.doctype_name AND t.fieldname = d.fieldname
          AND (d.doctype_name, d.fieldname) IN (('PriceListVersion', 'items'), ('PurchaseOrder', 'items'))
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
