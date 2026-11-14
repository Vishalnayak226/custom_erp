-- ---------------------------------------------------------------------------
-- Stage 164.1: find a stock ledger entry by its idempotency key without a scan.
--
-- stockLedgerKeyProbe (engines/stock_ledger_idempotency.go) asks "was an entry
-- with this key written already?" before every keyed stock-ledger write. The
-- GIN index on documents.data answered it, but a containment probe on a large
-- ledger still read ~1.9 s at 517k entries. A partial B-tree on the key
-- expression serves the same question as one index seek, and only
-- StockLedgerEntry rows are in it, so it stays small.
--
-- Additive and idempotent, replayed into every tenant schema (tenant_default
-- included, so new clients get it through the schema sync). Writes no rows.
-- CREATE INDEX here takes a SHARE lock on documents: reads go on, writes wait
-- until the build ends.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS idx_documents_stock_ledger_key
        ON %I.documents ((data->>'idempotency_key'))
        WHERE doctype = 'StockLedgerEntry'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
