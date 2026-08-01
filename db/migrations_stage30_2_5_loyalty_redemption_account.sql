-- Stage 30.2.5 (docs/UX_MANUAL_AUDIT.md): loyalty points are now burned at
-- checkout and applied to the sale automatically, instead of being burned the
-- instant the cashier clicked "Redeem Points" and then typed into a line's
-- price by hand (or forgot to).
--
-- That needs somewhere for the points half of the payment to land. Revenue is
-- still credited at the full sale value - the goods were sold for that price,
-- and the GST posting on top of it is computed on that same value - so the
-- debit side splits: cash actually collected to the payment clearing account,
-- and the points portion here. An expense, not a contra-revenue: this MVP
-- never recognized a liability when the points were earned (engines/loyalty.go
-- is a plain append-only ledger, by design), so the redemption is recognized
-- as the cost of running the loyalty programme at the point it is incurred.
--
-- Additive: one new account code. A tenant that never redeems points never
-- posts to it.
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('5250', 'Loyalty Points Redeemed', 'Expense')
ON CONFLICT (account_code) DO NOTHING;

-- Backfill every already-provisioned tenant schema; new tenants inherit it
-- from tenant_default at provisioning time.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES (''5250'', ''Loyalty Points Redeemed'', ''Expense'') ON CONFLICT (account_code) DO NOTHING',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
