-- ---------------------------------------------------------------------------
-- Stage 73.1.4 - loyalty redemption controls on every path that burns points.
--
--   * loyalty_redemption_otp_challenges.consumed_at: a verified one-time code
--     is spent by the one redemption it authorised (the till's sale, or the
--     OTP flow's own burn) and can never authorise a second one.
--   * LoyaltyRedemptionRequest gains channel, location and reason_code: a till
--     redemption above the approval value is raised from the till, approved
--     by a manager at that store, and burned by the sale itself.
--   * ReasonCode category "Loyalty Redemption" for the reason a cashier gives.
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
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'loyalty_redemption_otp_challenges') THEN
      EXECUTE format($sql$
        ALTER TABLE %I.loyalty_redemption_otp_challenges ADD COLUMN IF NOT EXISTS consumed_at TIMESTAMP
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('LoyaltyRedemptionRequest', 'channel', 'Raised from', 'Select', FALSE, 'API,POS', 6),
        ('LoyaltyRedemptionRequest', 'location', 'Store', 'Link', FALSE, 'Location', 7),
        ('LoyaltyRedemptionRequest', 'reason_code', 'Reason', 'Link', FALSE, 'ReasonCode', 8)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = options || ',Loyalty Redemption'
       WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
         AND options NOT LIKE '%%Loyalty Redemption%%'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
