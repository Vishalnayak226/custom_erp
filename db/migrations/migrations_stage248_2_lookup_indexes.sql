-- 248.2 and 248.3 (ask 251, user 2026-11-23: "Build all of above"): indexes
-- for the lookups that read every row of one record type. The GIN index on
-- data serves only `data @> ...`, not `data->>'x' = $1`, so these were
-- sequential scans:
--   * Item by code or barcode - ResolveItemBySKU (engines/item_lookup.go),
--     called once per line by sales orders, stock documents, delivery
--     challans, offline till sync and bulk uploads; 212 ms a call on 100k
--     items, 0.29 ms with these two indexes and the query unchanged. Also the
--     till's offer lookups and the bin-capacity joins.
--   * ProductFamily by code - item setup, aliases, the stock view.
--   * StockLedgerEntry by item - batch MRP choice at the till, batch and
--     serial history, "has this item moved" checks (385 ms at 200k rows).
--   * Paid POSCart by store and time - the store app's Today screen.
--   * SalesInvoice by customer_id and by customer name - credit check at sale,
--     receipts, Customer 360 (the credit query ORs the two, so both are needed).
--   * VendorInvoice by grn_id - GRN posting, charges, payables.
--   * Bin by bin_code - putaway, bin move, FEFO allocation, counts.
-- No predicate names deleted_at: a partial index is used only when the query
-- repeats its predicate, and several of these lookups do not filter it.
--
-- Additive and safe to run twice. Each build takes a SHARE lock on that
-- client's documents table, so writes wait while it runs - seconds on today's
-- clients.

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name = 'tenant_default'
       OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
    ORDER BY (schema_name <> 'tenant_default'), schema_name
  LOOP
    IF to_regclass(format('%I.documents', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_item_code ON %I.documents ((data->>'code')) WHERE doctype = 'Item'$f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_item_barcode ON %I.documents ((data->>'barcode')) WHERE doctype = 'Item'$f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_product_family_code ON %I.documents ((data->>'code')) WHERE doctype = 'ProductFamily'$f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_stock_ledger_item ON %I.documents ((data->>'item_id')) WHERE doctype = 'StockLedgerEntry'$f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_poscart_paid_location ON %I.documents ((data->>'location'), created_at) WHERE doctype = 'POSCart' AND status = 'Paid'$f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_sales_invoice_customer_id ON %I.documents ((data->>'customer_id')) WHERE doctype = 'SalesInvoice'$f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_sales_invoice_customer ON %I.documents ((data->>'customer')) WHERE doctype = 'SalesInvoice'$f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_vendor_invoice_grn ON %I.documents ((data->>'grn_id')) WHERE doctype = 'VendorInvoice'$f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_bin_code ON %I.documents ((data->>'bin_code')) WHERE doctype = 'Bin'$f$, schema_rec.schema_name);
  END LOOP;
END $$;
