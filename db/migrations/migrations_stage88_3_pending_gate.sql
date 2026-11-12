-- ---------------------------------------------------------------------------
-- Stage 88.3.1 (D2): the Pending gate's reasons.
--
-- Rejecting a pending order cancels it, and cancellation requires an Active
-- reason code (engines.CancelOrder); a short-pick on OMS ▸ Pick requires one
-- too (engines.ShortPickLine). Stage 74.1.1 seeded codes for the purchase and
-- stock screens but none for these two categories, so on a new client Reject
-- and Short-pick refused every answer until someone built the codes. These
-- are the usual ones; a client's own codes and edits are never touched.
--
-- The gate itself needs no schema change: acceptance_status and channel_push
-- live on the order's data (engines/oms_pending_gate.go), and its one setting
-- (oms.accept_channel_orders) is registered in code.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');
    EXECUTE format($sql$
      INSERT INTO %1$I.documents (id, doctype, data, status, created_by)
      SELECT v.id, 'ReasonCode', jsonb_build_object('code', v.id, 'description', v.description, 'category', v.category,
             'status', 'Active', 'loss_class', 'None', 'evidence_required', 'No', 'when_to_use', v.when_to_use), 'Active', 'system'
      FROM (VALUES
        ('CAN-CUSTOMER', 'Customer asked to cancel', 'Cancellation', 'The buyer cancelled before it shipped.'),
        ('CAN-NO-STOCK', 'Out of stock', 'Cancellation', 'The stock is not there to fill it.'),
        ('CAN-PRICE', 'Price or listing error', 'Cancellation', 'Listed at the wrong price or with the wrong details.'),
        ('CAN-ADDRESS', 'Cannot deliver to the address', 'Cancellation', 'No courier serves it, or the address is incomplete.'),
        ('CAN-FRAUD', 'Suspected fraud', 'Cancellation', 'Payment or buyer details look wrong.'),
        ('SP-NOT-FOUND', 'Not found at its location', 'Short Pick', 'The units are not where the stock says.'),
        ('SP-DAMAGED', 'Found damaged', 'Short Pick', 'The units are there but cannot be sent.'),
        ('SP-COUNT', 'Fewer than recorded', 'Short Pick', 'Some are there, fewer than the stock says.')
      ) AS v(id, description, category, when_to_use)
      ON CONFLICT (id) DO NOTHING$sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
