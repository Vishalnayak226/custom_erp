-- ---------------------------------------------------------------------------
-- Ask 297.4: closing a till stays fast on 30 lakh bills.
--
-- The till's drawer figures (SessionCashPosition: the close, the session
-- summary, the day register per till) find the session's paid bills by
-- data->>'pos_session'. Nothing indexed it, so each look read every paid
-- bill: 7.4 s a query on the 1-crore scratch run, and closing a till took
-- 31 s (the summary and the close each read the bills twice). The day
-- register does the same for every till of the day.
--
--   idx_documents_pos_cart_session      a till session's bills.
--   idx_documents_pos_refund_session    the refunds and exchange credits
--                                       paid from a till's drawer.
--   idx_documents_cash_movement_session a till's paid-ins, paid-outs and
--                                       safe drops.
--   customer_advances, customer_advance_refunds, pos_offline_sales
--                                       (pos_session): the advances and
--                                       held offline cash of a till.
--
-- The statistics objects give the planner the real spread of the two
-- fields: it ignores the figures of a partial index. Additive and safe to
-- run twice. Each documents build takes a SHARE lock, so writes wait while it
-- runs: well under a second at today's sizes. Writes no rows. ANALYZE samples
-- the table and blocks no reads or writes.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
  tbl TEXT;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF to_regclass(format('%I.documents', schema_rec.schema_name)) IS NOT NULL THEN
      EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_pos_cart_session ON %I.documents ((data->>'pos_session')) WHERE doctype = 'POSCart'$f$,
        schema_rec.schema_name);
      EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_pos_refund_session ON %I.documents ((data->>'pos_session')) WHERE doctype IN ('RefundRequest', 'ExchangeCredit')$f$,
        schema_rec.schema_name);
      EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_cash_movement_session ON %I.documents ((data->>'session_id')) WHERE doctype = 'POSCashMovement'$f$,
        schema_rec.schema_name);
      EXECUTE format($f$CREATE STATISTICS IF NOT EXISTS %I.documents_pos_session_stats ON ((data->>'pos_session')) FROM %I.documents$f$,
        schema_rec.schema_name, schema_rec.schema_name);
      EXECUTE format($f$CREATE STATISTICS IF NOT EXISTS %I.documents_session_id_stats ON ((data->>'session_id')) FROM %I.documents$f$,
        schema_rec.schema_name, schema_rec.schema_name);
      EXECUTE format('ANALYZE %I.documents', schema_rec.schema_name);
    END IF;

    FOREACH tbl IN ARRAY ARRAY['customer_advances', 'customer_advance_refunds', 'pos_offline_sales']
    LOOP
      IF EXISTS (SELECT 1 FROM information_schema.columns
                  WHERE table_schema = schema_rec.schema_name AND table_name = tbl AND column_name = 'pos_session') THEN
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON %I.%I (pos_session)', tbl || '_pos_session_idx', schema_rec.schema_name, tbl);
      END IF;
    END LOOP;
  END LOOP;
END
$mig$;
