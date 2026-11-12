-- Stage 94.15: POS bills are paid to the whole rupee. Revenue stays the lines'
-- own value; the round-off line (under half a rupee either way) is booked here,
-- in the sale's own revenue voucher, and reversed here when the units come back.
--
-- Additive: one new account code. A tenant whose bills come to whole rupees
-- never posts to it.
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('5280', 'Rounding Off', 'Expense')
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
      'INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES (''5280'', ''Rounding Off'', ''Expense'') ON CONFLICT (account_code) DO NOTHING',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
