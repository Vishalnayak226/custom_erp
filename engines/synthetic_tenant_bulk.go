package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"sync"

	"github.com/lib/pq"
)

// Item 47.9.1: the synthetic tenant's receipts in batches. A receipt through
// PostInventoryLedgerWithVoucher costs three statements a line (availability
// upsert, replay probe, ledger insert), which put opening stock at ~3,000
// ledger rows a minute and the 100k shape out of reach. receiveBulk writes the
// same rows - the same availability, the same ledger documents (built by
// stockLedgerEntryDoc, the one place their shape lives), the same item cost
// and the same GL entry - with a few statements per thousand lines, in one
// transaction, so a failure still leaves nothing half-loaded.
//
// synthBatchLoad exists so a test can build one tenant both ways and compare
// the totals; nothing else turns it off.
var synthBatchLoad = true

// synthBulkChunk is how many lines go in one statement.
const synthBulkChunk = 2000

// receiveBulk is receiveCosted for a large receipt.
func (g *synthGen) receiveBulk(loc, voucher string, qty map[string]int, withCost bool) error {
	return g.measure("receipt", 1, func() error {
		skus := make([]string, 0, len(qty))
		for sku := range qty {
			skus = append(skus, sku)
		}
		sort.Strings(skus)
		lines := make([]PostedStockLine, 0, len(skus))
		var value int64
		for _, sku := range skus {
			if qty[sku] == 0 {
				continue
			}
			lines = append(lines, PostedStockLine{SKU: sku, Qty: qty[sku]})
			value += g.costOf(sku) * int64(qty[sku])
		}
		if err := g.writeReceiptRows(loc, voucher, lines); err != nil {
			return err
		}
		if withCost {
			skuList := make([]string, len(lines))
			for i, l := range lines {
				skuList[i] = l.SKU
			}
			if err := g.recordCosts(skuList, func(sku string) float64 { return float64(qty[sku]) }); err != nil {
				return err
			}
		}
		if value == 0 {
			return nil
		}
		return PostDoubleEntry(g.tenantID, "StockReceipt", voucher, map[string]int64{"1200": value}, map[string]int64{"2100": value},
			synthBaseDate, "synth:"+voucher)
	})
}

// writeReceiptRows is the stock half of a receipt: availability and ledger
// entries, one transaction, a handful of statements. A receipt whose first
// ledger key is already there was written whole by an earlier run (one
// transaction), so it is skipped, the replay rule the single path has.
func (g *synthGen) writeReceiptRows(loc, voucher string, lines []PostedStockLine) error {
	if len(lines) == 0 {
		return nil
	}
	schema := g.schema
	entries := stockLedgerEntriesFor(loc, "StockReceipt", voucher, synthUser, lines)
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRow(fmt.Sprintf(stockLedgerKeyProbe, schema), entries[0].IdempotencyKey).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	for at := 0; at < len(lines); at += synthBulkChunk {
		part := lines[at:min(at+synthBulkChunk, len(lines))]
		skus := make([]string, len(part))
		qtys := make([]int64, len(part))
		for i, l := range part {
			skus[i], qtys[i] = l.SKU, int64(l.Qty)
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			INSERT INTO %[1]s.inventory_availability (sku, location_code, on_hand, available)
			SELECT s, $1, q, q FROM unnest($2::text[], $3::bigint[]) AS t(s, q)
			ON CONFLICT (sku, location_code) DO UPDATE SET
				on_hand = %[1]s.inventory_availability.on_hand + EXCLUDED.on_hand,
				available = %[1]s.inventory_availability.available + EXCLUDED.available,
				updated_at = CURRENT_TIMESTAMP`, schema), loc, pq.Array(skus), pq.Array(qtys)); err != nil {
			return err
		}
		ids := make([]string, 0, len(part))
		docs := make([]string, 0, len(part))
		for _, e := range entries[at:min(at+synthBulkChunk, len(entries))] {
			id, data, err := stockLedgerEntryDoc(e)
			if err != nil {
				return err
			}
			ids, docs = append(ids, id), append(docs, string(data))
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by)
			SELECT i, 'StockLedgerEntry', d::jsonb, 'Active', 'system' FROM unnest($1::text[], $2::text[]) AS t(i, d)`, schema),
			pq.Array(ids), pq.Array(docs)); err != nil {
			return fmt.Errorf("stock ledger for %s at %s: %w", voucher, loc, err)
		}
	}
	return tx.Commit()
}

// recordCosts records each SKU's receipt cost, on a bounded pool: the SKUs share
// no cost row, so the order does not change the result.
func (g *synthGen) recordCosts(skus []string, qtyOf func(string) float64) error {
	n := g.loadWorkers()
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		next    int
		firstEr error
	)
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if firstEr != nil || next >= len(skus) {
					mu.Unlock()
					return
				}
				sku := skus[next]
				next++
				mu.Unlock()
				err := RecordStockReceiptCost(g.tenantID, sku, qtyOf(sku), g.costOf(sku))
				if err != nil {
					mu.Lock()
					if firstEr == nil {
						firstEr = err
					}
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	return firstEr
}

// loadWorkers is the size of the pool the master and cost loaders use: the
// -workers setting, 1 beside Measure (its windows are read from transaction
// ids that parallel workers share).
func (g *synthGen) loadWorkers() int {
	if g.meter != nil || g.opts.Workers < 2 {
		return 1
	}
	return min(g.opts.Workers, 16)
}

// forEachParallel runs fn(0..n-1) on loadWorkers() goroutines (inline when the
// load is serial) and returns the first error. Each fn call is its own
// transaction in every caller, so the work only has to be independent per index.
func (g *synthGen) forEachParallel(n int, fn func(i int) error) error {
	workers := 1
	if synthBatchLoad {
		workers = g.loadWorkers()
	}
	if workers <= 1 || n < 2 {
		for i := 0; i < n; i++ {
			if err := fn(i); err != nil {
				return err
			}
		}
		return nil
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				if err := fn(i); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
				}
			}
		}()
	}
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
	return first
}
