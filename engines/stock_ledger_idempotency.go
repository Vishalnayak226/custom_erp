package engines

// stockLedgerKeyProbe is the replay check every keyed stock-ledger write runs
// (insertStockLedgerEntry): has an entry with this idempotency key been
// written already?
//
// Stage 47.9.3 found it on a synthetic tenant (47.9.1): written as
// data->>'idempotency_key' = $1 no index could serve it, so each POS sale line
// read every StockLedgerEntry the tenant had - 84 ms at 24k entries, growing
// with the ledger for ever (a million-bill tenant would spend seconds per
// line). Containment (@>) is served by the GIN index every documents table
// already has on data - 5 ms on the same tenant - and means exactly the same
// thing, because the key is always written as a JSON string.
const stockLedgerKeyProbe = `SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data @> jsonb_build_object('idempotency_key', $1::text))`
