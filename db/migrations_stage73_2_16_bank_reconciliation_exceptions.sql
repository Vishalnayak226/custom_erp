-- ---------------------------------------------------------------------------
-- Stage 73.2.16 - bank reconciliation: manual match, rule match, an exception
-- queue with owner and reason, and marketplace / POS payouts matched to bank
-- lines (engines/bank_reconciliation_workbench.go).
--
-- Before: auto-match was exact amount within +/-3 days, and whatever was
-- left was a list of ids. Now a statement line is Unmatched, Matched (auto,
-- rule, manual or payout - recorded with who, when and any difference),
-- Exception (owned, with a reason, aged) or Explained (a Finance Head /
-- Admin closed the exception with a note; nothing in the books matches it).
--
--   * BankStatementLine gains the match / exception fields as doctype
--     fields (its data is JSONB; these make them visible on the generic
--     screen) and the two new match_status values.
--   * 5230 Card / UPI Settlement Fees: the acquirer's deduction when a card
--     or UPI payout is matched to a bank credit and the clearing account
--     (1101 / 1102) is cleared to the bank.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('5230', 'Card / UPI Settlement Fees', 'Expense')
ON CONFLICT (account_code) DO NOTHING;

UPDATE tenant_default.doctype_fields SET options = 'Unmatched,Matched,Exception,Explained'
WHERE doctype_name = 'BankStatementLine' AND fieldname = 'match_status' AND options = 'Unmatched,Matched';

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('BankStatementLine', 'match_method', 'Matched By Method', 'Data', FALSE, NULL, 8),
('BankStatementLine', 'matched_by', 'Matched By', 'Data', FALSE, NULL, 9),
('BankStatementLine', 'matched_at', 'Matched At', 'Data', FALSE, NULL, 10),
('BankStatementLine', 'match_difference', 'Match Difference', 'Number', FALSE, NULL, 11),
('BankStatementLine', 'exception_owner', 'Exception Owner', 'Data', FALSE, NULL, 12),
('BankStatementLine', 'exception_reason', 'Exception Reason', 'Text', FALSE, NULL, 13),
('BankStatementLine', 'exception_raised_at', 'Exception Raised At', 'Data', FALSE, NULL, 14),
('BankStatementLine', 'resolution_note', 'Resolution Note', 'Text', FALSE, NULL, 15)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

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
        INSERT INTO %1$I.gl_accounts (account_code, account_name, account_type)
        VALUES ('5230', 'Card / UPI Settlement Fees', 'Expense') ON CONFLICT (account_code) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      EXECUTE format($sql$
        UPDATE %1$I.doctype_fields SET options = 'Unmatched,Matched,Exception,Explained'
        WHERE doctype_name = 'BankStatementLine' AND fieldname = 'match_status' AND options = 'Unmatched,Matched'
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
        WHERE doctype_name = 'BankStatementLine' AND fieldname IN ('match_method', 'matched_by', 'matched_at', 'match_difference',
              'exception_owner', 'exception_reason', 'exception_raised_at', 'resolution_note')
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
