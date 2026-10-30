-- ---------------------------------------------------------------------------
-- Stage 140.12 - what Guide's whole-organisation run found.
--
--   * GL 5295 Cash Short / Over (140.12.11): a till closed with a counted
--     drawer different from the expected cash posts the difference here
--     (short: Dr 5295 / Cr 1100; over: Dr 1100 / Cr 5295).
--   * AcquirerSettlement (140.12.12): one card / UPI payout from the
--     acquirer's daily settlement file, imported through the standard CSV
--     import and matched to the bank credit that empties 1101 / 1102.
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
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta') THEN
      EXECUTE format($sql$
        INSERT INTO %I.doctype_meta (name, module, document_type, module_key)
        VALUES ('AcquirerSettlement', 'Finance', 'Transaction', 'finance')
        ON CONFLICT (name) DO NOTHING
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        VALUES
          ('AcquirerSettlement', 'settlement_date', 'Settlement date', 'Date', TRUE, NULL, 1),
          ('AcquirerSettlement', 'mode', 'Mode', 'Select', TRUE, 'Card,UPI', 2),
          ('AcquirerSettlement', 'sales_date', 'Sales date', 'Date', FALSE, NULL, 3),
          ('AcquirerSettlement', 'gross', 'Gross', 'Number', FALSE, NULL, 4),
          ('AcquirerSettlement', 'fee', 'Fee', 'Number', FALSE, NULL, 5),
          ('AcquirerSettlement', 'net', 'Net paid', 'Number', TRUE, NULL, 6),
          ('AcquirerSettlement', 'utr', 'UTR', 'Data', FALSE, NULL, 7),
          ('AcquirerSettlement', 'terminal', 'Terminal / MID', 'Data', FALSE, NULL, 8),
          ('AcquirerSettlement', 'match_status', 'Match', 'Select', FALSE, 'Pending,Matched,Exception', 9),
          ('AcquirerSettlement', 'exception', 'Exception', 'Data', FALSE, NULL, 10),
          ('AcquirerSettlement', 'bank_line', 'Bank line', 'Data', FALSE, NULL, 11)
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type)
        VALUES ('5295', 'Cash Short / Over', 'Expense')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
