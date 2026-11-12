package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
)

// synthStockTotals is what a receipt leaves behind that must agree across the
// stock, the stock ledger, the item cost and the GL.
type synthStockTotals struct {
	availability, ledger, costRows, costQty, costValue, glDebit, glCredit, ledgerRows int64
	mismatches                                                                        int64
}

func readSynthStockTotals(t *testing.T, schema string) synthStockTotals {
	t.Helper()
	var v synthStockTotals
	one := func(dst *int64, q string) {
		t.Helper()
		if err := db.DB.QueryRow(fmt.Sprintf(q, schema)).Scan(dst); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	one(&v.availability, `SELECT COALESCE(SUM(on_hand), 0) FROM %s.inventory_availability`)
	one(&v.ledger, `SELECT COALESCE(SUM((data->>'qty')::numeric), 0)::bigint FROM %s.documents WHERE doctype = 'StockLedgerEntry'`)
	one(&v.ledgerRows, `SELECT COUNT(*) FROM %s.documents WHERE doctype = 'StockLedgerEntry'`)
	one(&v.costRows, `SELECT COUNT(*) FROM %s.item_cost`)
	one(&v.costQty, `SELECT COALESCE(SUM(cumulative_qty_received), 0)::bigint FROM %s.item_cost`)
	one(&v.costValue, `SELECT COALESCE(SUM(cumulative_qty_received * avg_unit_cost_paise), 0)::bigint FROM %s.item_cost`)
	one(&v.glDebit, `SELECT COALESCE(SUM(debit), 0)::bigint FROM %s.gl_postings`)
	one(&v.glCredit, `SELECT COALESCE(SUM(credit), 0)::bigint FROM %s.gl_postings`)
	// Every stocked (sku, location) must have as many units on hand as its
	// ledger entries sum to, for the stock that only receipts and sales moved.
	one(&v.mismatches, `SELECT COUNT(*) FROM (
		SELECT a.sku, a.location_code FROM %[1]s.inventory_availability a
		LEFT JOIN (SELECT data->>'item_id' sku, data->>'warehouse_id' loc, SUM((data->>'qty')::numeric) q
			FROM %[1]s.documents WHERE doctype = 'StockLedgerEntry' GROUP BY 1, 2) l
		ON l.sku = a.sku AND l.loc = a.location_code
		WHERE a.on_hand <> COALESCE(l.q, 0)) x`)
	return v
}

// Item 47.9.1: the batch loader builds the tenant the one-line-at-a-time loader
// builds. Same checksum and counts, and the stock, the stock ledger, the item
// cost and the GL agree with each other and with the slow build, to the unit
// and the paisa; masters and cost on a worker pool change nothing.
func TestSyntheticTenantBatchLoadMatchesLineByLine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two tenants")
	}
	synthTestEnv(t)
	shape := SynthShape{Name: "bulk-test", Items: 60, Stores: 3, Warehouses: 2, BinsPerWarehouse: 40, StockedBinPct: 30, Vendors: 3, Customers: 10,
		Bills: 40, MaxLinesPerBill: 3, HotSKUPct: 40, LargeOrders: 1, LargeOrderLines: 12, ReturnPct: 20, CustomerBillPct: 40,
		AuditTrail: 20, Owners: 2, Months: 2, MonthEndEntries: 3}
	defer func(old bool) { synthBatchLoad = old }(synthBatchLoad)
	synthBatchLoad = false
	slow := synthRun(t, "slow", SynthOptions{Seed: 11, Custom: &shape})
	synthBatchLoad = true
	fast := synthRun(t, "fast", SynthOptions{Seed: 11, Custom: &shape, Workers: 3})
	if slow.Checksum != fast.Checksum {
		t.Fatalf("batch checksum %s differs from line-by-line %s", fast.Checksum, slow.Checksum)
	}
	for k, v := range slow.Counts {
		if k == "audit_logs" || k == "audit_actions" {
			continue
		}
		if fast.Counts[k] != v {
			t.Errorf("count %s: line-by-line %d, batch %d", k, v, fast.Counts[k])
		}
	}
	a, b := readSynthStockTotals(t, slow.Schema), readSynthStockTotals(t, fast.Schema)
	if a != b {
		t.Errorf("stock totals differ:\n line-by-line %+v\n batch        %+v", a, b)
	}
	if b.availability == 0 || b.ledgerRows == 0 || b.costRows == 0 || b.glDebit == 0 {
		t.Fatalf("nothing was stocked, costed or posted: %+v", b)
	}
	if b.glDebit != b.glCredit {
		t.Errorf("receipt GL does not balance: debit %d, credit %d", b.glDebit, b.glCredit)
	}
	if b.mismatches != 0 {
		t.Errorf("%d stocked lines whose on-hand differs from their stock ledger", b.mismatches)
	}
}

// A receipt written twice is written once: the second run finds its ledger key
// and changes nothing, the replay rule the line-by-line path has.
func TestSyntheticBulkReceiptReplayWritesOnce(t *testing.T) {
	synthTestEnv(t)
	tiny := SynthShape{Name: "bulk-replay", Items: 12, Stores: 1, Warehouses: 1, BinsPerWarehouse: 4, StockedBinPct: 0, Vendors: 1, Customers: 2,
		Bills: 1, MaxLinesPerBill: 1, HotSKUPct: 10, ReturnPct: 0, AuditTrail: 1, Owners: 1, Months: 1, MonthEndEntries: 1}
	res := synthRun(t, "replay", SynthOptions{Seed: 3, Custom: &tiny})
	before := readSynthStockTotals(t, res.Schema)
	g := &synthGen{tenantID: res.TenantID, schema: res.Schema}
	if err := g.writeReceiptRows("SYN-S001", "SYN-OPEN-SYN-S001", []PostedStockLine{{SKU: "SYN-I000001", Qty: 5}, {SKU: "SYN-I000002", Qty: 7}}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if after := readSynthStockTotals(t, res.Schema); after != before {
		t.Errorf("a replayed receipt changed the totals:\n before %+v\n after  %+v", before, after)
	}
}
