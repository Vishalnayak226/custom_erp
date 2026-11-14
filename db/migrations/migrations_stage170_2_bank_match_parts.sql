-- ---------------------------------------------------------------------------
-- Stage 170.2: part-match a bank line. One row pairs an amount of one book
-- entry with one statement line; a book entry may be spread over several
-- statement lines and a statement line over several entries. Additive: an
-- exact match still uses gl_postings.matched_statement_line_id as before.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_postings');
    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.bank_match_parts (
        part_id BIGSERIAL PRIMARY KEY,
        statement_line_id VARCHAR(100) NOT NULL,
        posting_id UUID NOT NULL,
        amount_paise BIGINT NOT NULL CHECK (amount_paise > 0),
        matched_by VARCHAR(100),
        note TEXT,
        matched_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
      )$sql$, schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS bank_match_parts_line_idx ON %I.bank_match_parts (statement_line_id)', schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS bank_match_parts_posting_idx ON %I.bank_match_parts (posting_id)', schema_rec.schema_name);
  END LOOP;
END
$mig$;

-- The status list gains "Part Matched"; the line keeps what it matched and
-- what is open (both set by the engine, read-only in the form).
DO $mig2$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($sql$
      UPDATE %1$I.doctype_fields SET options = 'Unmatched,Matched,Exception,Explained,Part Matched'
       WHERE doctype_name = 'BankStatementLine' AND fieldname = 'match_status' AND options = 'Unmatched,Matched,Exception,Explained'
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES ('BankStatementLine', 'matched_amount', 'Matched Amount', 'Number', FALSE, NULL, 30),
             ('BankStatementLine', 'open_amount', 'Open Amount', 'Number', FALSE, NULL, 31)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig2$;
