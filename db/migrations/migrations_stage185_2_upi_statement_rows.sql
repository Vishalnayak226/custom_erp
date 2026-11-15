-- ---------------------------------------------------------------------------
-- 185.2 (ask 185, owner 2026-11-14): BharatPe / PhonePe / Paytm statements.
--
-- A UPI provider's statement lists each customer payment, not the payout.
-- AcquirerSettlement gains the customer's UPI ref (RRN), the till bill it
-- was matched to and the result of that check. A row with a UPI ref is
-- matched to its bill and summed with the other payments of its payout
-- before the bank match (engines/acquirer_upi_statements.go); a row without
-- one is a payout, as before. Additive and safe to run twice.
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
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('AcquirerSettlement', 'payment_ref', 'Customer UPI ref (RRN)', 'Data', FALSE, NULL, 12),
        ('AcquirerSettlement', 'bill',        'Till bill',              'Data', FALSE, NULL, 13),
        ('AcquirerSettlement', 'bill_check',  'Bill check',             'Data', FALSE, NULL, 14)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
