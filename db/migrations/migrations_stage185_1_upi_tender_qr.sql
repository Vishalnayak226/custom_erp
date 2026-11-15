-- ---------------------------------------------------------------------------
-- 185.1 (ask 185, owner 2026-11-14: "Build bharatpe integration"): a UPI QR
-- on the till for the exact bill amount.
--
-- BharatPe, PhonePe and Paytm business accounts have no public merchant API
-- for a small store, but each pays out to a UPI ID. Location.upi_id is the
-- store's UPI ID; POSTender.upi_id overrides it for one UPI tender (a second
-- provider). The till draws a standard upi://pay QR with the amount filled
-- in and the cashier keys the UTR as before. Blank = no QR, as before.
-- Additive and safe to run twice.
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
        ('POSTender', 'upi_id', 'UPI ID (blank = the store''s)', 'Data', FALSE, NULL, 8),
        ('Location',  'upi_id', 'UPI ID for the till QR',        'Data', FALSE, NULL, 46)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
