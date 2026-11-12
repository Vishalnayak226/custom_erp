-- ---------------------------------------------------------------------------
-- Stage 55.16 (Batch D) - fixed asset depreciation is posted monthly, and a
-- disposal records its proceeds and the gain or loss
-- (engines/asset_depreciation.go).
--
--   * 1410 Accumulated Depreciation (Asset, contra - carries a credit
--     balance against 1400 Fixed Assets): Cr each month's charge, Dr at
--     disposal.
--   * 5310 Depreciation Expense: Dr each month's charge.
--   * 4300 Gain on Asset Disposal: Cr when proceeds exceed the book value
--     (a shortfall still goes to 5300 Loss on Asset Disposal).
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('1410', 'Accumulated Depreciation', 'Asset'),
('5310', 'Depreciation Expense', 'Expense'),
('4300', 'Gain on Asset Disposal', 'Revenue')
ON CONFLICT (account_code) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.gl_accounts (account_code, account_name, account_type) VALUES
        ('1410', 'Accumulated Depreciation', 'Asset'),
        ('5310', 'Depreciation Expense', 'Expense'),
        ('4300', 'Gain on Asset Disposal', 'Revenue')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
