-- ---------------------------------------------------------------------------
-- Stage 73.2.6 - exchange at the till with net settlement.
--
--   * ExchangeCredit: the money value of items returned in an exchange,
--     held for the new sale rung up with them. Open -> Used. Engine-written
--     only, read-only everywhere.
--   * GL 2620 Exchange Credit Clearing (Liability): the returned items'
--     refund credits it, the new sale debits it as a tender, and anything the
--     new sale did not use is paid out from the till against it - so it nets
--     to zero for every completed exchange.
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
      VALUES ('ExchangeCredit', 'Sales', 'Transaction', 'sales')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('ExchangeCredit', 'original_order_id', 'Returned against bill', 'Data', TRUE, NULL, 1),
        ('ExchangeCredit', 'return_request_id', 'Return', 'Data', TRUE, NULL, 2),
        ('ExchangeCredit', 'location', 'Store', 'Data', TRUE, NULL, 3),
        ('ExchangeCredit', 'customer_id', 'Customer', 'Data', FALSE, NULL, 4),
        ('ExchangeCredit', 'amount', 'Credit', 'Number', TRUE, NULL, 5),
        ('ExchangeCredit', 'used_on', 'Used on bill', 'Data', FALSE, NULL, 6),
        ('ExchangeCredit', 'used_amount', 'Used', 'Number', FALSE, NULL, 7),
        ('ExchangeCredit', 'paid_out', 'Paid back to the customer', 'Number', FALSE, NULL, 8),
        ('ExchangeCredit', 'status', 'Status', 'Select', TRUE, 'Open,Used', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'ExchangeCredit', TRUE, FALSE, FALSE, FALSE),
             ('Store Manager', 'ExchangeCredit', TRUE, FALSE, FALSE, FALSE),
             ('Cashier', 'ExchangeCredit', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type)
        VALUES ('2620', 'Exchange Credit Clearing', 'Liability')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
