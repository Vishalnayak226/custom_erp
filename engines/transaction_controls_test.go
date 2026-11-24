package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
)

// 250.2: the daily reconciliation run flags stock that drifts from its ledger
// and bins that claim more than their store holds; stock not yet in a bin is
// not flagged.
func TestReconciliationFlagsStockDrift(t *testing.T) {
	tenantID, schema, _ := stage140Tenant(t)
	const loc, sku = "WH2502", "TEE2502"
	registered := map[string]bool{}
	for _, c := range reconciliationChecks {
		registered[c.id] = true
	}
	if !registered["stock-ledger"] || !registered["bin-stock"] {
		t.Fatalf("the daily run does not include both stock checks: %v", registered)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	tied := func(name string, check func(string) (bool, float64, string, error)) (bool, string) {
		t.Helper()
		ok, _, detail, err := check(schema)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return ok, detail
	}

	// Stock posted with its ledger line, part of it in a bin: both checks tie.
	if _, err := PostInventoryLedgerWithVoucher(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 5.0}}, false, "Test", "T250-2", "tester"); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO {s}.bin_stock (bin_code, sku, location_code, qty) VALUES ('BIN-2502', $1, $2, 3)`, sku, loc)
	if ok, detail := tied("stock-ledger", checkStockLedger); !ok {
		t.Fatalf("stock posted with its ledger line is flagged: %s", detail)
	}
	if ok, detail := tied("bin-stock", checkBinStock); !ok {
		t.Fatalf("2 units not yet in a bin are flagged: %s", detail)
	}

	// On-hand changed with no ledger line.
	exec(`UPDATE {s}.inventory_availability SET on_hand = on_hand + 3, available = available + 3 WHERE sku = $1 AND location_code = $2`, sku, loc)
	if ok, detail := tied("stock-ledger", checkStockLedger); ok || !strings.Contains(detail, sku+"@"+loc) {
		t.Fatalf("on-hand off its ledger: tied=%v detail=%q, want flagged naming %s@%s", ok, detail, sku, loc)
	}

	// Bins claiming more than the store holds (8 on hand, 9 in bins).
	exec(`UPDATE {s}.bin_stock SET qty = 9 WHERE bin_code = 'BIN-2502' AND sku = $1`, sku)
	if ok, detail := tied("bin-stock", checkBinStock); ok || !strings.Contains(detail, sku+"@"+loc) {
		t.Fatalf("bins over their store: tied=%v detail=%q, want flagged naming %s@%s", ok, detail, sku, loc)
	}
}
