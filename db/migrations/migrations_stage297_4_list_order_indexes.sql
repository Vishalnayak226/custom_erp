-- ---------------------------------------------------------------------------
-- Ask 297.4: record lists that stay fast at 1 crore rows.
--
-- The 1-crore scratch run (1 crore stock ledger rows, 30 lakh bills, 10 lakh
-- items) timed the list endpoint (internal/server/handlers_core_doc_engine.go)
-- and found two plan problems this file fixes:
--
-- 1. A list is in id order within one record type. No index held
--    (doctype, id), so page 1 of the item list walked the primary key across
--    every record type, or read and sorted every item: 9-11 s. With the
--    index it is under 1 ms, and a deep page reads ids from the index alone.
--
-- 2. A list filtered on a field (the stock ledger of one item) planned as if
--    every value matched 0.5% of rows. Postgres ignores the statistics of a
--    partial index, and every index on a field here is partial (one record
--    type), so it walked the stock ledger in id order looking for 50 rows of
--    an item that has 10: 17-40 s. A statistics object on the same field
--    gives the planner the real spread, and it then uses the field's index:
--    under 1 ms. Statistics cost nothing on a write.
--
-- The fields are the ones the busy record types filter on and already index
-- (stock ledger, bills, sales orders and lines, invoices, refunds,
-- shipments). Additive and idempotent, replayed into every tenant schema
-- (tenant_default too, so a new client's documents table copies both through
-- LIKE ... INCLUDING ALL). Writes no rows. CREATE INDEX takes a SHARE lock on
-- documents: reads go on, writes wait until the build ends. ANALYZE fills the
-- new statistics now instead of at the next autovacuum.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
  field TEXT;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS idx_documents_doctype_id_live
        ON %I.documents (doctype, id)
        WHERE deleted_at IS NULL
    $sql$, schema_rec.schema_name);

    FOREACH field IN ARRAY ARRAY['item_id', 'customer_id', 'customer', 'order_id', 'sales_order_id',
      'channel', 'channel_order_id', 'payment_state', 'location', 'sku', 'location_code',
      'original_order_id', 'hold_reason', 'customer_phone']
    LOOP
      EXECUTE format($sql$
        CREATE STATISTICS IF NOT EXISTS %I.%I ON ((data->>%L)) FROM %I.documents
      $sql$, schema_rec.schema_name, 'documents_' || field || '_stats', field, schema_rec.schema_name);
    END LOOP;

    EXECUTE format('ANALYZE %I.documents', schema_rec.schema_name);
  END LOOP;
END
$mig$;
