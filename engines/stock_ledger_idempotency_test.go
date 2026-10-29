package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 47.9.3 regression: the stock-ledger replay check is served by an
// index (it read every ledger entry before), and still makes a replayed
// write a no-op.
func TestStockLedgerKeyProbeUsesAnIndex(t *testing.T) {
	db.InitDB(testConnStr())
	key := fmt.Sprintf("probe-47-9-3-%d", time.Now().UnixNano())
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'StockLedgerEntry' AND data->>'idempotency_key' = $1`, key)
	for i := 0; i < 2; i++ {
		if err := WriteStockLedgerEntry("default", StockLedgerEntry{ItemID: "PROBE-SKU", WarehouseID: "HO", Qty: 1,
			VoucherType: "Probe", VoucherID: "PROBE", IdempotencyKey: key}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'StockLedgerEntry' AND data->>'idempotency_key' = $1`, key).Scan(&n)
	if n != 1 {
		t.Errorf("a replayed keyed write left %d entries, want 1", n)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Can the index every documents table has on data - GIN, jsonb_ops -
	// answer the check? A temporary copy holding only that index and 20,000
	// keyed entries says, without depending on which index the planner would
	// prefer for one particular table's statistics.
	for _, stmt := range []string{
		`CREATE TEMP TABLE documents (LIKE tenant_default.documents INCLUDING DEFAULTS) ON COMMIT DROP`,
		`CREATE INDEX ON pg_temp.documents USING gin (data)`,
		`INSERT INTO pg_temp.documents (id, doctype, data, status, created_by)
		 SELECT 'SLE-' || g, 'StockLedgerEntry', jsonb_build_object('idempotency_key', 'k-' || g, 'item_id', 'SKU-' || (g % 500), 'qty', 1), 'Active', 'system'
		 FROM generate_series(1, 20000) g`,
		`ANALYZE pg_temp.documents`,
		// Below the size where a full read stops being cheapest; price it out
		// so the plan shows which index, if any, can answer the key.
		`SET LOCAL enable_seqscan = off`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	explain := func(query string) string {
		t.Helper()
		rows, err := tx.Query("EXPLAIN "+fmt.Sprintf(query, "pg_temp"), "k-12345")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var plan strings.Builder
		for rows.Next() {
			var line string
			_ = rows.Scan(&line)
			plan.WriteString(line + "\n")
		}
		return plan.String()
	}
	if plan := explain(stockLedgerKeyProbe); !strings.Contains(plan, "Index Cond: (data @>") {
		t.Errorf("the replay check is not index-served:\n%s", plan)
	}
	// The shape it replaced could only read the whole table.
	if plan := explain(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'idempotency_key' = $1)`); !strings.Contains(plan, "Seq Scan") {
		t.Errorf("expected the old shape to need a full read:\n%s", plan)
	}
}
