-- ---------------------------------------------------------------------------
-- Stage 166.1: a customer's loyalty tier spend without reading every refund.
--
-- customerTierSpend (engines/loyalty_tiering.go) runs on every bill of a
-- customer on a tiered programme. It sums the customer's paid POSCarts by
-- data->>'customer_id' and the Processed RefundRequests whose
-- data->>'original_order_id' is one of those carts. No index served either
-- expression, so each bill read every paid cart and every refund the client
-- had (47.9.1's 100k run: ~10 min per 1,000 bills on a dev box).
--
-- Two partial B-trees, each holding only its own record type, so they stay
-- small. Additive and idempotent, replayed into every tenant schema
-- (tenant_default included, so new clients get them through the schema sync).
-- Writes no rows. CREATE INDEX takes a SHARE lock on documents: reads go on,
-- writes wait until the build ends.
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
       WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS idx_documents_poscart_paid_customer
        ON %I.documents ((data->>'customer_id'), created_at)
        WHERE doctype = 'POSCart' AND status = 'Paid'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS idx_documents_refund_original_order
        ON %I.documents ((data->>'original_order_id'))
        WHERE doctype = 'RefundRequest'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
