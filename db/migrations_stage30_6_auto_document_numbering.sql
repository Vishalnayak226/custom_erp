-- Stage 30.6: server-generated transaction numbers.
--
-- Every bespoke create form in public/app.js asked the user to type the
-- document number themselves (PO Number, GRN Number, Transfer Number, Claim
-- Number, ...) and sent it as both the document id and its number field. That
-- is the one value that must never come from a browser: two makers on the same
-- screen pick the same number and the second save silently overwrites the
-- first (documents.id is the ON CONFLICT key), and nothing stops a typo'd or
-- deliberately out-of-order number entering the books.
--
-- engines.GenerateSequence has existed since Stage 1 and is already the
-- numbering path for PurchaseRequisition, Payslip, CSV import, and
-- RFQ/PO-on-conversion. This migration adds the series rows the remaining
-- doctypes need so engines/document_numbering.go can route them through the
-- same row-locked counter, with the series still owned by the tenant's own
-- Prefix Configurations screen.

-- 1. Segment control (the shape of the generated number).
--
-- Numbers were hardcoded as <Prefix><Sep><Store><Sep><FinancialYear><Sep><Padded>.
-- include_store makes the store segment optional so a tenant that does not
-- want per-store numbering can have a single clean PO/2026/000001 series.
--
-- The financial-year segment is NOT a separate toggle on purpose: it is
-- already implied by reset_frequency, which until now was read by
-- GenerateSequence and then never used for anything. A number series whose
-- counter resets every year but does not show the year in the number would
-- re-issue PO/000001 twelve months later - a duplicate id, not a preference.
-- So the two are one setting: ANNUAL/MONTHLY show the period segment they
-- reset on, NEVER shows none and never resets. See engines/numbering.go.
ALTER TABLE tenant_default.prefix_configs
  ADD COLUMN IF NOT EXISTS include_store BOOLEAN DEFAULT TRUE;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format(
      'ALTER TABLE %I.prefix_configs ADD COLUMN IF NOT EXISTS include_store BOOLEAN DEFAULT TRUE',
      schema_rec.schema_name
    );
  END LOOP;
END $$;

-- 2. Series rows for the doctypes that never had one.
--
-- PR/PO/GRN/TO/TI/SI (db/migration.sql) and RFQ
-- (db/migrations_stage17f_purchase_requisition.sql) already exist and are left
-- exactly as they are - a tenant that has already edited one of those keeps
-- their edit. Prefixes below are distinct from each other and from the
-- existing rows, which matters because documents.id is unique across every
-- doctype, not per-doctype (a generated number becomes the id).
INSERT INTO tenant_default.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
VALUES
  ('ASN',   'ASN',  '/', 6, 'ANNUAL', TRUE),
  ('QTN',   'QTN',  '/', 6, 'ANNUAL', TRUE),
  ('EXP',   'EXP',  '/', 6, 'ANNUAL', TRUE),
  ('LV',    'LV',   '/', 6, 'ANNUAL', TRUE),
  ('LOAN',  'LOAN', '/', 6, 'ANNUAL', TRUE),
  ('GRV',   'GRV',  '/', 6, 'ANNUAL', TRUE),
  ('PRO',   'PRO',  '/', 6, 'ANNUAL', TRUE),
  ('ATT',   'ATT',  '/', 6, 'ANNUAL', TRUE)
ON CONFLICT (doc_type) DO NOTHING;

-- Backfill every already-provisioned tenant schema; new tenants inherit these
-- from tenant_default at provisioning time (engines/saas.go copies
-- prefix_configs), same pattern as db/migrations_stage30_2_1_grn_location.sql.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format(
      'INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store) VALUES '
      || '(''ASN'', ''ASN'', ''/'', 6, ''ANNUAL'', TRUE),'
      || '(''QTN'', ''QTN'', ''/'', 6, ''ANNUAL'', TRUE),'
      || '(''EXP'', ''EXP'', ''/'', 6, ''ANNUAL'', TRUE),'
      || '(''LV'', ''LV'', ''/'', 6, ''ANNUAL'', TRUE),'
      || '(''LOAN'', ''LOAN'', ''/'', 6, ''ANNUAL'', TRUE),'
      || '(''GRV'', ''GRV'', ''/'', 6, ''ANNUAL'', TRUE),'
      || '(''PRO'', ''PRO'', ''/'', 6, ''ANNUAL'', TRUE),'
      || '(''ATT'', ''ATT'', ''/'', 6, ''ANNUAL'', TRUE) '
      || 'ON CONFLICT (doc_type) DO NOTHING',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
