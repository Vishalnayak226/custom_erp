-- ---------------------------------------------------------------------------
-- Stage 146.2.7 / 146.2.10: two unique keys on the loyalty ledger, so points
-- given for an occasion (birthday, anniversary) or earned on an online order
-- can never be written twice - not by a re-run journey, a second trigger or a
-- repeated delivery event. A key, not a check-then-insert.
--
--   * LoyaltyOccasion rows: one per customer and occasion-year ("birthday:2026").
--   * SalesOrder Earn rows: one per customer, order and rule (the base points
--     and each bonus offer are their own rows, told apart by reason).
--
-- Partial indexes: no existing row is touched. Additive and idempotent,
-- replayed into every tenant schema.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'loyalty_point_ledger');

    EXECUTE format($sql$
      CREATE UNIQUE INDEX IF NOT EXISTS uq_loyalty_ledger_occasion
        ON %I.loyalty_point_ledger (customer_id, reference_id)
        WHERE reference_doctype = 'LoyaltyOccasion'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      CREATE UNIQUE INDEX IF NOT EXISTS uq_loyalty_ledger_online_order
        ON %I.loyalty_point_ledger (customer_id, reference_id, COALESCE(reason, ''))
        WHERE reference_doctype = 'SalesOrder' AND transaction_type = 'Earn'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
