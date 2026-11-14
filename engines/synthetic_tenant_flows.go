package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Item 47.9.1 (remainder): the other business flows of a synthetic tenant,
// the parallel sales loader and backdated history. Every flow goes through
// the engine a real tenant uses; nothing here writes a business table
// directly except the one dated update in backdate.

// synthBill is one planned POS bill. The plan is drawn from the seeded
// generator in bill order, so it is the same whether one worker or many
// ring it up afterwards.
type synthBill struct {
	cart, store, mode, customer string
	storeIdx                    int
	lines                       []QuoteLineRequest
	wantReturn                  bool
}

// synthPlanChunk is how many bills are planned at a time; it bounds memory on
// the 1m shape without changing the draw order.
const synthPlanChunk = 20_000

func (g *synthGen) workers() int {
	if g.opts.Workers > 1 {
		return min(min(g.opts.Workers, len(g.stores)), 64)
	}
	return 1
}

// planBills draws bills [from, to) in order.
func (g *synthGen) planBills(from, to int, large map[int]bool) []synthBill {
	s := g.shape
	modes := []string{"Cash", "Card", "UPI"}
	out := make([]synthBill, 0, to-from)
	for b := from; b < to; b++ {
		storeIdx := g.rng.IntN(len(g.stores))
		bill := synthBill{cart: fmt.Sprintf("SYN-CART-%07d", b+1), store: g.stores[storeIdx], storeIdx: storeIdx,
			mode: modes[g.rng.IntN(len(modes))]}
		if s.Customers > 0 && g.rng.IntN(100) < s.CustomerBillPct {
			bill.customer = fmt.Sprintf("SYN-C%07d", 1+g.rng.IntN(s.Customers))
		}
		bill.lines = g.billLines(large[b])
		bill.wantReturn = !large[b] && g.rng.IntN(100) < s.ReturnPct && (len(bill.lines) > 1 || bill.lines[0].Qty > 1)
		out = append(out, bill)
	}
	return out
}

// runBill rings up one bill and its partial return.
func (g *synthGen) runBill(b synthBill) error {
	if err := g.measure("sale", 1, func() error { return g.ringUp(b.cart, b.store, b.mode, b.customer, b.lines) }); err != nil {
		return fmt.Errorf("bill %s: %w", b.cart, err)
	}
	if b.wantReturn {
		if err := g.measure("return", 1, func() error { return g.partialReturn(b.cart, b.store, b.lines[0].Sku) }); err != nil {
			return fmt.Errorf("return of %s: %w", b.cart, err)
		}
	}
	return nil
}

// runBills rings up a planned chunk. With one worker it is a plain loop. With
// more, the bills are sharded by store: a store's bills run in order on one
// worker, and stores share no stock rows, so the result is the one a single
// worker gives (the tests compare the checksums).
func (g *synthGen) runBills(bills []synthBill, done *int, total int) error {
	n := g.workers()
	if n == 1 {
		for _, b := range bills {
			if err := g.runBill(b); err != nil {
				return err
			}
			*done++
			if *done%1000 == 0 {
				g.logf("  %d / %d bills", *done, total)
			}
		}
		return nil
	}
	shards := make([][]synthBill, n)
	for _, b := range bills {
		shards[b.storeIdx%n] = append(shards[b.storeIdx%n], b)
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		firstEr error
	)
	for _, shard := range shards {
		wg.Add(1)
		go func(shard []synthBill) {
			defer wg.Done()
			for _, b := range shard {
				mu.Lock()
				stop := firstEr != nil
				mu.Unlock()
				if stop {
					return
				}
				err := g.runBill(b)
				mu.Lock()
				if err != nil && firstEr == nil {
					firstEr = err
				}
				*done++
				if *done%1000 == 0 {
					g.logf("  %d / %d bills", *done, total)
				}
				mu.Unlock()
			}
		}(shard)
	}
	wg.Wait()
	return firstEr
}

// retryDeadlock runs one single-transaction step again when the database
// picked it as a deadlock victim - the parallel loader's stores meet on the
// shared number series and item cost rows. The step rolled back, so the retry
// is clean.
func retryDeadlock(fn func() error) error {
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		if err = fn(); err == nil || !isRetryableTxError(err) {
			return err
		}
		time.Sleep(time.Duration(10*(attempt+1)) * time.Millisecond)
	}
	return err
}

func isRetryableTxError(err error) bool {
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "deadlock detected") || strings.Contains(m, "could not serialize access")
}

func (g *synthGen) insertDoc(id, doctype, status string, data map[string]interface{}) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, $2, $3, $4, $5)`, g.schema), id, doctype, raw, status, synthUser)
	return err
}

func synthJSONLines(rows ...map[string]interface{}) string {
	raw, _ := json.Marshal(rows)
	return string(raw)
}

// synthPurchase is one planned purchase order: every random choice is made
// up front, in order, so the posting below can run on a worker pool without
// changing what the seed produces.
type synthPurchase struct {
	poID, grnID, priorID, vendor, wh string
	n                                int
	net, gross                       float64
	poLines, rcvLines, prior         []map[string]interface{}
}

// purchasing raises Approved purchase orders to the shape's vendors and
// receives each through the real GRN step flow (invoice number, date and total
// checked, priced against the order, posted with its stock and GL). Ask 166
// (47.9.1): the orders are planned serially from the seed, then received on
// the loader's worker pool (each receipt is its own transaction chain, and a
// receipt that loses a lock race with another is retried), the way the masters
// and opening stock are.
func (g *synthGen) purchasing() error {
	s := g.shape
	if s.PurchaseOrders == 0 || len(g.whs) == 0 || s.Vendors == 0 {
		return nil
	}
	plans := make([]synthPurchase, s.PurchaseOrders)
	for p := range plans {
		pl := &plans[p]
		pl.n = p + 1
		pl.poID = fmt.Sprintf("SYN-PO-%05d", p+1)
		pl.grnID = fmt.Sprintf("SYN-GRN-%05d", p+1)
		pl.priorID = fmt.Sprintf("SYN-GRN-PRIOR-%05d", p+1)
		pl.vendor = fmt.Sprintf("SYN-V%05d", 1+g.rng.IntN(s.Vendors))
		pl.wh = g.whs[g.rng.IntN(len(g.whs))]
		nLines := 1 + g.rng.IntN(3)
		var net float64
		seen := map[int]bool{}
		for len(pl.poLines) < nLines {
			at := 1 + g.rng.IntN(len(g.items)-1)
			if seen[at] {
				continue
			}
			seen[at] = true
			it := g.items[at]
			qty := 10 + g.rng.IntN(40)
			rate := math.Round(float64(it.cost)) / 100
			net += float64(qty) * rate
			pl.poLines = append(pl.poLines, map[string]interface{}{"sku": it.code, "qty": qty, "rate": rate})
			pl.rcvLines = append(pl.rcvLines, map[string]interface{}{"sku": it.code, "qty": qty, "invoice_rate": rate})
		}
		// The price last paid is what "Post all" compares against, so each item
		// has an earlier receipt at the same rate (the books' opening purchases).
		pl.prior = make([]map[string]interface{}, len(pl.rcvLines))
		for i, l := range pl.rcvLines {
			pl.prior[i] = map[string]interface{}{"sku": l["sku"], "qty": 1, "invoice_rate": l["invoice_rate"]}
		}
		pl.net = math.Round(net*100) / 100
		pl.gross = math.Round(net*105) / 100
	}
	return g.forEachParallel(len(plans), func(i int) error {
		var err error
		for try := 0; try < 4; try++ {
			if err = g.receivePurchase(&plans[i], try > 0); err == nil || !isRetryablePGError(err) {
				break
			}
		}
		return err
	})
}

// isRetryablePGError reports a lock-order loss (deadlock 40P01) or a
// serialization failure (40001) between two receipts that touch one item.
func isRetryablePGError(err error) bool {
	m := err.Error()
	return strings.Contains(m, "40P01") || strings.Contains(m, "deadlock detected") || strings.Contains(m, "40001")
}

// receivePurchase writes one order's prior receipt, the order and its GRN and
// posts it. A retry finds the documents of the failed try and goes on from the
// post, so nothing is written twice.
func (g *synthGen) receivePurchase(pl *synthPurchase, retry bool) error {
	if !retry {
		if err := g.insertDoc(pl.priorID, "GRN", "Approved", map[string]interface{}{"code": pl.priorID, "vendor": pl.vendor, "location": pl.wh,
			"received_items": synthJSONLines(pl.prior...)}); err != nil {
			return fmt.Errorf("%s: %w", pl.priorID, err)
		}
		if err := g.insertDoc(pl.poID, "PurchaseOrder", "Approved", map[string]interface{}{"code": pl.poID, "vendor": pl.vendor,
			"gst_mode": GSTModeExclusive, "total_amount": pl.net, "grand_total": pl.gross, "items": synthJSONLines(pl.poLines...)}); err != nil {
			return fmt.Errorf("%s: %w", pl.poID, err)
		}
	}
	payload := map[string]interface{}{"flow": "steps", "po_ids": pl.poID, "vendor": pl.vendor, "location": pl.wh,
		"vendor_invoice_no": fmt.Sprintf("SYN-INV-%05d", pl.n), "vendor_invoice_date": synthBaseDate, "vendor_invoice_amount": pl.gross,
		"received_items": synthJSONLines(pl.rcvLines...)}
	err := g.measure("receipt", 1, func() error {
		if !retry {
			if err := PrepareGRNPricing(g.tenantID, true, payload); err != nil {
				return err
			}
			if err := PrepareGRNSteps(g.tenantID, synthUser, true, payload); err != nil {
				return err
			}
			if err := PrepareGRNBatchNumbers(g.tenantID, true, payload); err != nil {
				return err
			}
			if err := validateGRNRules(g.tenantID, "", payload); err != nil {
				return err
			}
			payload["code"] = pl.grnID
			if err := g.insertDoc(pl.grnID, "GRN", strField(payload, "status"), payload); err != nil {
				return err
			}
			SyncGRNLineRows(g.tenantID, pl.grnID)
		}
		_, err := PostAllGRNStep(g.tenantID, synthUser, "Super Admin", "", pl.grnID)
		return err
	})
	if err != nil {
		return fmt.Errorf("purchase order %s: %w", pl.poID, err)
	}
	return nil
}

// manufacturing builds finished goods from two components each: a BOM, a
// production order, material issued from the warehouse, the order completed.
func (g *synthGen) manufacturing() error {
	s := g.shape
	if s.ProductionOrders == 0 || len(g.whs) == 0 || len(g.items) < 4 {
		return nil
	}
	for k := 0; k < s.ProductionOrders; k++ {
		wh := g.whs[g.rng.IntN(len(g.whs))]
		parent := 1 + g.rng.IntN(len(g.items)-3)
		a, b := g.items[parent+1], g.items[parent+2]
		qty := 1 + g.rng.IntN(5)
		aQty, bQty := 1+g.rng.IntN(3), 1+g.rng.IntN(3)
		bomID, proID := fmt.Sprintf("SYN-BOM-%05d", k+1), fmt.Sprintf("SYN-PRO-%05d", k+1)
		comps, _ := json.Marshal([]map[string]interface{}{{"sku": a.code, "qty": aQty}, {"sku": b.code, "qty": bQty}})
		if err := g.receive(wh, "SYN-MFG-RM-"+proID, map[string]int{a.code: qty * aQty, b.code: qty * bQty}); err != nil {
			return fmt.Errorf("%s: components: %w", proID, err)
		}
		if err := g.insertDoc(bomID, "BOM", "Active", map[string]interface{}{"code": bomID, "parent_item": g.items[parent].code,
			"components": string(comps), "status": "Active"}); err != nil {
			return fmt.Errorf("%s: %w", bomID, err)
		}
		if err := g.insertDoc(proID, "ProductionOrder", "Draft", map[string]interface{}{"bom_id": bomID, "quantity": qty,
			"location": wh, "status": "Draft"}); err != nil {
			return fmt.Errorf("%s: %w", proID, err)
		}
		err := g.measure("production", 1, func() error {
			if err := IssueProductionMaterial(g.tenantID, proID); err != nil {
				return err
			}
			return CompleteProductionOrder(g.tenantID, proID)
		})
		if err != nil {
			return fmt.Errorf("production order %s: %w", proID, err)
		}
	}
	return nil
}

// salesOrders places online orders (the OMS path: reservation, sourcing,
// straight to Pick) against the stores' stock.
func (g *synthGen) salesOrders() error {
	s := g.shape
	if s.SalesOrders == 0 {
		return nil
	}
	for k := 0; k < s.SalesOrders; k++ {
		store := g.stores[g.rng.IntN(len(g.stores))]
		nLines := 1 + g.rng.IntN(3)
		var lines []SalesOrderLineInput
		seen := map[int]bool{}
		for len(lines) < nLines {
			at := 1 + g.rng.IntN(len(g.items)-1)
			if seen[at] {
				continue
			}
			seen[at] = true
			lines = append(lines, SalesOrderLineInput{SKU: g.items[at].code, Qty: 1 + g.rng.IntN(2), UnitPrice: g.items[at].price})
		}
		cust := 1
		if s.Customers > 0 {
			cust = 1 + g.rng.IntN(s.Customers)
		}
		in := SalesOrderInput{Channel: "Synthetic", ChannelOrderID: fmt.Sprintf("SYN-SO-%05d", k+1),
			CustomerName: fmt.Sprintf("Synthetic customer %d", cust), CustomerID: fmt.Sprintf("SYN-C%07d", cust),
			ShippingAddress: "1 MG Road", ShippingCity: "Bengaluru", ShippingState: "Karnataka", ShippingPincode: "560001",
			PaymentStatus: "Confirmed", PreferredLocation: store, SkipPending: true, AcceptedBy: synthManager, Lines: lines}
		err := g.measure("sales_order", 1, func() error { _, err := CreateSalesOrder(g.tenantID, in); return err })
		if err != nil {
			return fmt.Errorf("sales order %s: %w", in.ChannelOrderID, err)
		}
	}
	return nil
}

// backdate spreads the tenant's documents over the last HistoryDays days,
// oldest first in the order they were made, so dated lists, aging and month
// filters have a past to read. The audit trail is left alone (its signature
// covers its time) and so is the general ledger, which is append-only by
// design: its month-end postings already carry their own dates. Content is
// untouched, so the checksum does not move.
func (g *synthGen) backdate() error {
	days := g.shape.HistoryDays
	if days <= 0 {
		return nil
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		WITH ranked AS (
			SELECT id, doctype, created_at,
			       row_number() OVER (ORDER BY created_at, id) AS rn, count(*) OVER () AS n
			  FROM %[1]s.documents)
		UPDATE %[1]s.documents d
		   SET created_at = now() - make_interval(secs => ($1::float8 * 86400.0) * (1.0 - r.rn::float8 / r.n::float8)),
		       updated_at = now() - make_interval(secs => ($1::float8 * 86400.0) * (1.0 - r.rn::float8 / r.n::float8))
		  FROM ranked r WHERE d.id = r.id AND d.doctype = r.doctype`, g.schema), float64(days)); err != nil {
		return fmt.Errorf("documents: %w", err)
	}
	return nil
}

var errSynthMeasureWorkers = errors.New("Measure needs a single worker: the footprint is read from transaction ids, which parallel workers share")
