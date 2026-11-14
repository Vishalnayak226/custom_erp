package engines

// stockLedgerKeyProbe is the replay check every keyed stock-ledger write runs
// (insertStockLedgerEntry): has an entry with this idempotency key been
// written already?
//
// Stage 47.9.3 first moved it from data->>'idempotency_key' = $1 (no index, a
// read of every entry) to containment (@>), served by the GIN index on data.
// Stage 164.1 found that still ~1.9 s at 517k entries and gave it its own
// partial B-tree, idx_documents_stock_ledger_key, on exactly this expression
// and the doctype predicate below; the query is written to match that index
// (keep the two in step). The key is always written as a JSON string, so
// ->> reads it back unchanged.
const stockLedgerKeyProbe = `SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'idempotency_key' = $1::text)`
