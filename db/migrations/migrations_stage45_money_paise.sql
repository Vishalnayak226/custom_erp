-- Stage 45: gl_postings.debit/credit move from whole-rupee INT to paise
-- BIGINT (durability audit 2026-07-31 finding #7 / ERP_LOOPHOLES_ANALYSIS.md
-- money precision item). INT truncated every GST component to whole rupees
-- before posting, always downward, so output-tax liability was silently
-- understated on every sale with a fractional tax component - and INT capped
-- a single posting at ~2.14 crore rupees. BIGINT-paise fixes both: no
-- fractional loss, and headroom to ~92 quadrillion rupees.
--
-- The backfill (`* 100`) runs in the same statement batch as the type change
-- so the embedded runner's per-file transaction (db/migrate.go) makes this
-- atomic: either every existing row becomes paise-scaled together with the
-- column, or the whole file rolls back and nothing is half-converted.
-- transaction_debit/transaction_credit are untouched - they are already
-- NUMERIC(18,4) original-currency amounts for FX (Stage 37.1.2), a different
-- concept from the functional-currency debit/credit this file touches.

ALTER TABLE tenant_default.gl_postings ALTER COLUMN debit TYPE BIGINT;
ALTER TABLE tenant_default.gl_postings ALTER COLUMN credit TYPE BIGINT;
UPDATE tenant_default.gl_postings SET debit = debit * 100, credit = credit * 100;

COMMENT ON COLUMN tenant_default.gl_postings.debit IS
    'Paise (1/100 rupee), functional currency. Was whole-rupee INT before Stage 45.';
COMMENT ON COLUMN tenant_default.gl_postings.credit IS
    'Paise (1/100 rupee), functional currency. Was whole-rupee INT before Stage 45.';

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.gl_postings ALTER COLUMN debit TYPE BIGINT', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.gl_postings ALTER COLUMN credit TYPE BIGINT', schema_rec.schema_name);
    EXECUTE format('UPDATE %I.gl_postings SET debit = debit * 100, credit = credit * 100', schema_rec.schema_name);
  END LOOP;
END $$;
