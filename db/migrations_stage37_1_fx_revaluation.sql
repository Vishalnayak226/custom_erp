-- Stage 37.1.3 / 37.1.4 - realised and unrealised FX gain/loss accounts.
--
-- 37.1.2 taught documents and postings to CARRY a currency and a rate. It did
-- not teach them what to do when the rate MOVES, which is the entire point of
-- multi-currency accounting: a USD 1,000 receivable booked at 83.00 and
-- collected at 85.00 brought in 2,000 rupees nobody has recognised anywhere.
-- Those two rupees are a real gain and they need a real account.
--
-- Four accounts, not two, because realised and unrealised must never be summed
-- into one line. A realised gain is cash that has actually moved; an unrealised
-- one is an opinion about an open balance that can reverse itself next month.
-- Every tax authority and every auditor treats them differently, and a single
-- "FX Gain/Loss" account makes the split unrecoverable after the fact.
--
-- Gains are Revenue and losses are Expense rather than one signed account,
-- matching how this chart already splits 4150 Sales Returns from 5150 Purchase
-- Returns: the P&L reads as a statement, not as a set of negative numbers.
--
-- ADDITIVE ONLY. No existing account, posting, column or row is altered. A
-- tenant that never transacts in a foreign currency never posts to any of
-- these four, and its trial balance is byte-identical to what it is today.

INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('4200', 'Realised FX Gain', 'Revenue'),
('4210', 'Unrealised FX Gain', 'Revenue'),
('5600', 'Realised FX Loss', 'Expense'),
('5610', 'Unrealised FX Loss', 'Expense')
ON CONFLICT (account_code) DO NOTHING;

-- Backfill every already-provisioned tenant schema. New tenants inherit these
-- from tenant_default at provisioning time (ProvisionTenantSchema clones
-- gl_accounts), so only pre-existing schemas need the direct write - the same
-- pattern as db/migrations_stage26_6_11_item_tax_treatment.sql.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES '
      '(''4200'', ''Realised FX Gain'', ''Revenue''), '
      '(''4210'', ''Unrealised FX Gain'', ''Revenue''), '
      '(''5600'', ''Realised FX Loss'', ''Expense''), '
      '(''5610'', ''Unrealised FX Loss'', ''Expense'') '
      'ON CONFLICT (account_code) DO NOTHING',
      schema_rec.schema_name
    );
  END LOOP;
END $$;

-- No new table and no new column.
--
-- The revaluation state a document carries (cumulative unrealised amount, the
-- date and rate it was last revalued at, the realised gain/loss booked at
-- settlement) lives as additive JSON keys on the document's existing `data`
-- column, written by the engine. That is deliberate and follows 42.1.4's
-- precedent for receipt lines: a document that has never been revalued is
-- byte-identical to what it is today, and a second table would need its own
-- consistency guarantee against the postings that are already the source of
-- truth. They are engine-written audit fields, not operator-entered, so they
-- also need no doctype_fields row - adding one would put a number an operator
-- must never hand-edit onto the form.
