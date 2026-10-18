-- ---------------------------------------------------------------------------
-- Stage 73.2.9 - customer reference on B2B sales.
--
--   * customer_reference on SalesOrder and SalesInvoice: a business
--     customer's own purchase order number or reference. Optional. Entered on
--     the order (Order Management -> + New Order, or the order's Edit) or on the invoice form,
--     carried from the order onto the invoice raised from it, printed on the
--     invoice, and listed by the Sales by Customer Reference report.
--
-- Additive and idempotent, replayed into every tenant schema. Existing orders
-- and invoices are untouched - they simply have no reference.
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
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('SalesOrder', 'customer_reference', 'Customer PO / Reference', 'Data', FALSE, NULL, 4),
        ('SalesInvoice', 'customer_reference', 'Customer PO / Reference', 'Data', FALSE, NULL, 2)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
