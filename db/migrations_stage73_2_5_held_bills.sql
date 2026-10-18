-- ---------------------------------------------------------------------------
-- Stage 73.2.5 - hold and resume a bill.
--
--   * POSHeldBill: a cart parked at the till - its cart number, store,
--     cashier, customer, coupon codes, lines and a note - with status
--     Held -> Resumed (or Discarded). Engine-written only, read-only
--     everywhere. The cart number is the sale's identity, so a resumed bill
--     checks out under the number it was held with and the checkout's own
--     claim on that number is what keeps it from ever posting twice.
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
      INSERT INTO %I.doctype_meta (name, module, document_type, module_key)
      VALUES ('POSHeldBill', 'Sales', 'Transaction', 'sales')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('POSHeldBill', 'cart_number', 'Bill number', 'Data', TRUE, NULL, 1),
        ('POSHeldBill', 'location', 'Store', 'Data', TRUE, NULL, 2),
        ('POSHeldBill', 'cashier', 'Held by', 'Data', TRUE, NULL, 3),
        ('POSHeldBill', 'customer_id', 'Customer', 'Data', FALSE, NULL, 4),
        ('POSHeldBill', 'line_count', 'Items', 'Number', FALSE, NULL, 5),
        ('POSHeldBill', 'total', 'Total when held', 'Number', FALSE, NULL, 6),
        ('POSHeldBill', 'note', 'Note', 'Data', FALSE, NULL, 7),
        ('POSHeldBill', 'resumed_by', 'Resumed by', 'Data', FALSE, NULL, 8),
        ('POSHeldBill', 'status', 'Status', 'Select', TRUE, 'Held,Resumed,Discarded', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'POSHeldBill', TRUE, FALSE, FALSE, FALSE),
             ('Store Manager', 'POSHeldBill', TRUE, FALSE, FALSE, FALSE),
             ('Cashier', 'POSHeldBill', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
