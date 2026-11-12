-- ---------------------------------------------------------------------------
-- Stages 148.31 / 148.32: wholesale selling (module wholesale, off by default).
--
--   148.31 one invoice per customer per period
--     Customer.bill_to_customer          who receives the balance and the invoice
--     Customer.summary_invoice           Yes: this payer is invoiced once per period
--     Customer.group_by_place_of_supply  Yes: a different ship-to state is its own invoice
--     Customer.group_by_customer_po      Yes: a different customer PO is its own invoice
--     SalesOrderLine.instalments         "qty@date" pieces, shown on the challan
--   148.32 drop-ship
--     Item.dropship                      Yes: the vendor ships it; Item.default_vendor is the vendor
--     SalesOrder.delivery_date           the date the customer is promised (follows to the PO)
--     PurchaseOrder.dropship_order_id    the sales order a drop-ship PO serves (server-set)
--     PurchaseOrder.ship_to_address      the customer's address the vendor ships to (server-set)
--
-- The module is registered switched OFF in the catalog; a tenant with no
-- entitlement row reads as off, and the fields are hidden until it is on. No
-- per-client business row is written. Additive and idempotent, replayed into
-- every tenant schema.
-- ---------------------------------------------------------------------------
INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('wholesale', 'Wholesale', 'One invoice per customer per period, and drop-ship orders the vendor ships.', FALSE, FALSE)
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT v.d, v.f, v.l, v.t, FALSE, v.o, v.n
      FROM (VALUES
        ('Customer', 'bill_to_customer', 'Bill to', 'Link', 'Customer', 90),
        ('Customer', 'summary_invoice', 'One invoice per period', 'Select', ',Yes', 91),
        ('Customer', 'group_by_place_of_supply', 'Split by place of supply', 'Select', ',Yes', 92),
        ('Customer', 'group_by_customer_po', 'Split by customer PO', 'Select', ',Yes', 93),
        ('SalesOrderLine', 'instalments', 'Instalments', 'Data', NULL, 20),
        ('Item', 'dropship', 'Drop-ship', 'Select', ',Yes', 100),
        ('SalesOrder', 'delivery_date', 'Delivery date', 'Date', NULL, 100),
        ('PurchaseOrder', 'dropship_order_id', 'Drop-ship for', 'Link', 'SalesOrder', 130),
        ('PurchaseOrder', 'ship_to_address', 'Ship to', 'Data', NULL, 131)
      ) AS v(d, f, l, t, o, n)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = v.d)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
