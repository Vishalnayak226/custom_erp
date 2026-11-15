package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"

	"github.com/lib/pq"
)

// 49.5.5b (ask 178): a Stock Ledger line keeps what it was posted at - the
// item's UOM, and the rates the posting path knew - so a later change to the
// item master, its price or its tax does not rewrite history. Every key is
// written only when known.
//
// A posting path opts in by putting these keys on the item maps it hands
// PostInventoryLedgerWithVoucherTx (or by setting the fields on the
// PostedStockLine it writes itself). Whatever a path leaves out,
// fillLedgerSnapshot fills as the line is written: the item's UOM and the unit
// cost it stands at.
const (
	ledgerKeyValuationRate = "ledger_valuation_rate" // unit cost the movement was valued at, rupees
	ledgerKeySaleRate      = "ledger_sale_rate"      // unit price charged, rupees
	ledgerKeyTaxRate       = "ledger_tax_rate"       // tax rate on the line, percent
)

type stockLineSnapshot struct {
	UOM           string   `json:"uom,omitempty"`
	ValuationRate float64  `json:"valuation_rate,omitempty"`
	SaleRate      float64  `json:"sale_rate,omitempty"`
	TaxRate       *float64 `json:"tax_rate,omitempty"` // a pointer: 0% is a real rate
}

// stockLineSnapshotFrom reads the rate keys a posting path set on a line. Not
// the line's own "uom": that can be the unit it was bought in, while the
// ledger qty is always in the item's stock unit.
func stockLineSnapshotFrom(item map[string]interface{}) stockLineSnapshot {
	s := stockLineSnapshot{}
	s.ValuationRate, _ = item[ledgerKeyValuationRate].(float64)
	s.SaleRate, _ = item[ledgerKeySaleRate].(float64)
	if t, ok := item[ledgerKeyTaxRate].(float64); ok {
		s.TaxRate = &t
	}
	return s
}

// writeTo adds the snapshot to a ledger entry's document.
func (s stockLineSnapshot) writeTo(doc map[string]interface{}) {
	if s.UOM != "" {
		doc["uom"] = s.UOM
	}
	if s.ValuationRate != 0 {
		doc["valuation_rate"] = s.ValuationRate
	}
	if s.SaleRate != 0 {
		doc["sale_rate"] = s.SaleRate
	}
	if s.TaxRate != nil {
		doc["tax_rate"] = *s.TaxRate
	}
}

// merge is the snapshot of two lines of one SKU and lot on one voucher, which
// post as one movement: each rate weighted by qty, the UOM and tax rate kept
// only where both lines agree.
func (s stockLineSnapshot) merge(qty int, o stockLineSnapshot, oQty int) stockLineSnapshot {
	weighted := func(a, b float64) float64 {
		if a == b || qty+oQty == 0 {
			return a
		}
		return math.Round((a*float64(qty)+b*float64(oQty))/float64(qty+oQty)*100) / 100
	}
	out := stockLineSnapshot{ValuationRate: weighted(s.ValuationRate, o.ValuationRate), SaleRate: weighted(s.SaleRate, o.SaleRate)}
	if s.UOM == o.UOM {
		out.UOM = s.UOM
	}
	if s.TaxRate != nil && o.TaxRate != nil && *s.TaxRate == *o.TaxRate {
		out.TaxRate = s.TaxRate
	}
	return out
}

// withStockLineUOMs fills each line's UOM from the item master as it is at
// posting, in one read for the voucher. A SKU with no item record, or an item
// with no UOM, is left blank.
func withStockLineUOMs(tx *sql.Tx, schema string, lines []PostedStockLine) ([]PostedStockLine, error) {
	var skus []string
	for _, l := range lines {
		if l.UOM == "" && l.SKU != "" {
			skus = append(skus, l.SKU)
		}
	}
	if len(skus) == 0 {
		return lines, nil
	}
	rows, err := tx.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'code', ''), COALESCE(data->>'uom', '') FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL AND (id = ANY($1) OR data->>'code' = ANY($1))`, schema), pq.Array(skus))
	if err != nil {
		return nil, fmt.Errorf("stock ledger UOM: %w", err)
	}
	defer rows.Close()
	byCode, byID := map[string]string{}, map[string]string{}
	for rows.Next() {
		var id, code, uom string
		if err := rows.Scan(&id, &code, &uom); err != nil {
			return nil, err
		}
		byID[id] = uom
		if code != "" {
			byCode[code] = uom
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := append([]PostedStockLine(nil), lines...)
	for i := range out {
		if out[i].UOM != "" {
			continue
		}
		// The code wins over another item's id, as in ResolveItemBySKU.
		if u, ok := byCode[out[i].SKU]; ok {
			out[i].UOM = u
		} else {
			out[i].UOM = byID[out[i].SKU]
		}
	}
	return out, nil
}

// markStockLineRates sets the rates a sale knows on the stock lines of that
// SKU (49.5.5b.2): the unit cost the server valued it at and the unit price
// the till charged.
func markStockLineRates(stock []interface{}, sku string, valuationRate, saleRate float64) {
	for _, line := range stock {
		if m, ok := line.(map[string]interface{}); ok && m["sku"] == sku {
			m[ledgerKeyValuationRate] = valuationRate
			m[ledgerKeySaleRate] = saleRate
		}
	}
}

// markLedgerRates sets what a sales invoice line was billed at on its stock
// line (49.5.5b.3): the unit price before tax and, for a line saved from the
// screen (which always carries rate and gst_rate), the GST rate. A line given
// only unit_price has no tax rate of its own. valuationRate 0 = not known.
func (l salesInvoiceLine) markLedgerRates(line map[string]interface{}, valuationRate float64) {
	if valuationRate != 0 {
		line[ledgerKeyValuationRate] = valuationRate
	}
	switch {
	case l.Rate != 0:
		line[ledgerKeySaleRate] = l.Rate
		line[ledgerKeyTaxRate] = l.GSTRate
	case l.UnitPrice != 0:
		line[ledgerKeySaleRate] = l.UnitPrice
	}
}

// markGRNLedgerRates sets each received line's unit cost as the receipt's
// costing books it (49.5.5b.4): the vendor's invoice rate stamped on the GRN
// line, else the PO's rate. A line neither prices is left without one.
func markGRNLedgerRates(tenantID, schema, grnID string, items []interface{}) {
	if grnID == "" || len(items) == 0 {
		return
	}
	costs := grnLineUnitCostsPaise(tenantID, grnID)
	poID := grnPurchaseOrderID(schema, grnID)
	for _, raw := range items {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		sku, _ := m["sku"].(string)
		unit, ok := costs[sku]
		if !ok {
			unit, ok = resolvePOBaseUnitCostPaise(tenantID, poID, sku)
		}
		if ok && unit > 0 {
			m[ledgerKeyValuationRate] = PaiseToRupees(unit)
		}
	}
}

// grnLineTaxRates reads the GST rate each SKU was received at from a GRN's
// received_items. A SKU whose lines disagree (a multi-PO receipt) has none.
func grnLineTaxRates(receivedItems string) map[string]float64 {
	var lines []map[string]interface{}
	if json.Unmarshal([]byte(receivedItems), &lines) != nil {
		return nil
	}
	out, mixed := map[string]float64{}, map[string]bool{}
	for _, l := range lines {
		sku, _ := l["sku"].(string)
		r, ok := l["gst_rate"]
		if sku == "" || !ok || r == nil || fmt.Sprint(r) == "" {
			continue
		}
		rate := numFromInterface(r)
		if prev, seen := out[sku]; seen && prev != rate {
			mixed[sku] = true
		}
		out[sku] = rate
	}
	for sku := range mixed {
		delete(out, sku)
	}
	return out
}

// ledgerSnapshot is what a purchase-return line goes back at (49.5.5b.5): its
// unit cost and, for accepted stock - the billed part - the GST rate the GRN
// received it at. Set-aside stock was never billed, so carries no tax rate.
func (c *PurchaseReturnable) ledgerSnapshot(l PurchaseReturnLine) stockLineSnapshot {
	s := stockLineSnapshot{ValuationRate: l.UnitCost}
	if l.StockBucket == PurchaseReturnBucketAccepted {
		if r, ok := c.taxRates[l.SKU]; ok {
			s.TaxRate = &r
		}
	}
	return s
}

// markLedgerRates puts ledgerSnapshot's rates on an accepted line handed to
// PostInventoryLedgerWithVoucherTx.
func (c *PurchaseReturnable) markLedgerRates(line map[string]interface{}, l PurchaseReturnLine) {
	s := c.ledgerSnapshot(l)
	if s.ValuationRate != 0 {
		line[ledgerKeyValuationRate] = s.ValuationRate
	}
	if s.TaxRate != nil {
		line[ledgerKeyTaxRate] = *s.TaxRate
	}
}

// returnLedgerRates is what a customer return's stock comes back in at
// (49.5.5b.6), read once per QC.
type returnLedgerRates struct {
	bySKU map[string]stockLineSnapshot // a returned line's SKU: UOM, cost and tax rate
	uom   map[string]string            // every SKU coming back, bundle parts included
}

// customerReturnLedgerRates: each SKU comes back at the cost the sale booked,
// weighted over its lines, with the GST rate its original invoice line billed.
// A till bill's GST is per cart, not per line, so a till return has no tax
// rate - as the sale's own ledger line has none.
func customerReturnLedgerRates(tx *sql.Tx, schema, originalOrderID string, items []returnItemRecord) (returnLedgerRates, error) {
	qty, cost := map[string]int{}, map[string]float64{}
	var lines []PostedStockLine
	for _, it := range items {
		qty[it.SKU] += it.Qty
		cost[it.SKU] += it.OriginalCostPrice * float64(it.Qty)
		lines = append(lines, PostedStockLine{SKU: it.SKU})
		for _, p := range it.Parts {
			lines = append(lines, PostedStockLine{SKU: p.SKU})
		}
	}
	lines, err := withStockLineUOMs(tx, schema, lines)
	if err != nil {
		return returnLedgerRates{}, err
	}
	r := returnLedgerRates{bySKU: map[string]stockLineSnapshot{}, uom: map[string]string{}}
	for _, l := range lines {
		r.uom[l.SKU] = l.UOM
	}
	taxes, err := originalInvoiceTaxRates(tx, schema, originalOrderID)
	if err != nil {
		return returnLedgerRates{}, err
	}
	for sku, n := range qty {
		s := stockLineSnapshot{UOM: r.uom[sku]}
		if n > 0 {
			s.ValuationRate = math.Round(cost[sku]/float64(n)*100) / 100
		}
		if t, ok := taxes[sku]; ok {
			s.TaxRate = &t
		}
		r.bySKU[sku] = s
	}
	return r, nil
}

// part is a bundle component's snapshot: its UOM and its share of the cost
// the sale booked, per unit.
func (r returnLedgerRates) part(p returnPart) stockLineSnapshot {
	s := stockLineSnapshot{UOM: r.uom[p.SKU]}
	if p.Qty > 0 {
		s.ValuationRate = math.Round(PaiseToRupees(p.ValuePaise)/float64(p.Qty)*100) / 100
	}
	return s
}

// originalInvoiceTaxRates reads the GST rate each SKU was billed at on the
// SalesInvoice a return quotes (by its id or its sales order). Only lines
// saved with a rate before tax carry one; a SKU billed at two rates has none.
func originalInvoiceTaxRates(tx *sql.Tx, schema, originalOrderID string) (map[string]float64, error) {
	if originalOrderID == "" {
		return nil, nil
	}
	var raw string
	err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'SalesInvoice' AND deleted_at IS NULL
		AND (id = $1 OR data->>'sales_order_id' = $1) ORDER BY (id = $1) DESC, created_at LIMIT 1`, schema), originalOrderID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("return tax rates: %w", err)
	}
	out, mixed := map[string]float64{}, map[string]bool{}
	for _, l := range salesInvoiceLineItems(raw) {
		if l.Rate == 0 {
			continue
		}
		if prev, seen := out[l.Sku]; seen && prev != l.GSTRate {
			mixed[l.Sku] = true
		}
		out[l.Sku] = l.GSTRate
	}
	for sku := range mixed {
		delete(out, sku)
	}
	return out, nil
}

// fillLedgerSnapshot (49.5.5b.9-.11) gives a ledger line whose path set no UOM
// or valuation rate its item's UOM and the unit cost the item stands at as the
// line is written - moving average, else the standard cost, as
// ResolveQuoteUnitCostPaise reads them. insertStockLedgerEntry, which every
// ledger line goes through, calls it, so no posting path is left without them.
// Read in the posting's own transaction; one query, and none when the path set both.
func fillLedgerSnapshot(q interface {
	QueryRow(string, ...interface{}) *sql.Row
}, schema string, e *StockLedgerEntry) error {
	if e.ItemID == "" || (e.UOM != "" && e.ValuationRate != 0) {
		return nil
	}
	var uom, std string
	var avg int64
	// The code wins over another item's id, as in ResolveItemBySKU.
	if err := q.QueryRow(fmt.Sprintf(`SELECT COALESCE(i.uom, ''), COALESCE(i.std, ''), COALESCE(c.avg_unit_cost_paise, 0)
		FROM (SELECT 1) one
		LEFT JOIN LATERAL (SELECT data->>'uom' AS uom, data->>'standard_cost' AS std FROM %[1]s.documents
			WHERE doctype = 'Item' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1)
			ORDER BY (data->>'code' = $1) DESC LIMIT 1) i ON true
		LEFT JOIN %[1]s.item_cost c ON c.item_code = $1 AND c.cumulative_qty_received > 0`, schema), e.ItemID).Scan(&uom, &std, &avg); err != nil {
		return fmt.Errorf("stock ledger snapshot for %s: %w", e.ItemID, err)
	}
	if e.UOM == "" {
		e.UOM = uom
	}
	if e.ValuationRate == 0 {
		if avg > 0 {
			e.ValuationRate = PaiseToRupees(avg)
		} else if v, _ := parityNumber(std); v > 0 {
			e.ValuationRate = v
		}
	}
	return nil
}
