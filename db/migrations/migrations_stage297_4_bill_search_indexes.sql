-- ---------------------------------------------------------------------------
-- Ask 297.4: finding a bill stays fast on 30 lakh bills.
--
-- POS > Sales History and the POS return's "find the bill" box searched every
-- paid bill for the typed text. On the 1-crore scratch run a bill number or a
-- mobile with no date ran past three minutes. SearchSalesHistory now looks a
-- bill up by its number, its cart id, its GSTIN or its customer once bills
-- are many, and these indexes are how:
--
--   idx_documents_pos_bill_number   the printed bill number. It also serves
--                                   the offline till-number check (every
--                                   synced offline bill) and a credit note's
--                                   source-bill lookup, which read every bill.
--   idx_documents_pos_b2b_gstin     a B2B bill's GSTIN (only bills that have one).
--   idx_documents_pos_paid_created  paid bills newest first at every store.
--   idx_documents_return_original   a bill's returns: the list's "returned"
--                                   mark, read for each row shown.
--   idx_documents_customer_phone    a customer by mobile: the search's
--                                   customers, and the till's customer lookup
--                                   on every bill, which read every customer
--                                   (230 ms on 2 lakh customers).
--
-- Additive and safe to run twice. Each build takes a SHARE lock on its
-- documents table, so writes wait while it runs: about a minute an index on
-- the 1-crore scratch table, well under a second at today's sizes. Writes
-- no rows. ANALYZE then gives the planner figures for the new expressions:
-- without them it guessed thousands of bills per number and read every bill
-- (22 s on the scratch run). It samples the table and blocks no reads or writes.
-- ---------------------------------------------------------------------------
DO $mig$
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
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_pos_bill_number ON %I.documents ((data->>'bill_number')) WHERE doctype = 'POSCart'$f$,
      schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_pos_b2b_gstin ON %I.documents ((data->'b2b'->>'gstin')) WHERE doctype = 'POSCart' AND (data->'b2b'->>'gstin') IS NOT NULL$f$,
      schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_pos_paid_created ON %I.documents (created_at) WHERE doctype = 'POSCart' AND status = 'Paid' AND deleted_at IS NULL$f$,
      schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_return_original ON %I.documents ((data->>'original_order_id')) WHERE doctype = 'ReturnRequest' AND deleted_at IS NULL$f$,
      schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_customer_phone ON %I.documents ((data->>'phone')) WHERE doctype = 'Customer' AND deleted_at IS NULL$f$,
      schema_rec.schema_name);
    EXECUTE format('ANALYZE %I.documents', schema_rec.schema_name);
  END LOOP;
END
$mig$;
