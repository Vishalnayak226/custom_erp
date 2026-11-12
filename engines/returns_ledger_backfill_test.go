package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 132.20: a return QC'd before the 132.13 fix gets its missing stock
// ledger line - one per SKU that came back on hand, at its QC time, marked
// backfilled - and a second run writes nothing.
func TestBackfillReturnStockLedger(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	clean := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = 'T13220-RR'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = 'T13220-RR'")
	}
	clean()
	defer clean()
	insertTestDoc(t, schema, "T13220-RR", "ReturnRequest", map[string]interface{}{
		"return_location": "HO", "status": "QC Complete",
		"items": []map[string]interface{}{
			{"sku": "T13220-A", "qty": 2, "disposition": "Sellable"},
			{"sku": "T13220-B", "qty": 1, "disposition": "Missing"}, // nothing came back
			{"sku": "T13220-C", "qty": 1, "disposition": "Damaged"},
		}})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'QC Complete', updated_at = '2026-09-01 11:00:00' WHERE id = 'T13220-RR'")

	n, err := BackfillReturnStockLedger("default")
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if n < 2 {
		t.Fatalf("backfill wrote %d lines, want at least the 2 of T13220-RR", n)
	}
	type line struct{ qty, status, at, mark string }
	got := map[string]line{}
	rows, err := db.DB.Query("SELECT data->>'item_id', data->>'qty', COALESCE(data->>'to_status', ''), to_char(created_at, 'YYYY-MM-DD HH24:MI'), COALESCE(data->>'backfilled', '') FROM " + schema + ".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = 'T13220-RR'")
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	for rows.Next() {
		var sku string
		var l line
		rows.Scan(&sku, &l.qty, &l.status, &l.at, &l.mark)
		got[sku] = l
	}
	rows.Close()
	if a := got["T13220-A"]; a.qty != "2" || a.status != "available" || a.at != "2026-09-01 11:00" || a.mark != "132.20" {
		t.Errorf("Sellable line: %+v", a)
	}
	if c := got["T13220-C"]; c.qty != "1" || c.status != "damaged" {
		t.Errorf("Damaged line: %+v", c)
	}
	if _, ok := got["T13220-B"]; ok || len(got) != 2 {
		t.Errorf("lines %v - want A and C only (B was Missing)", got)
	}
	if again, err := BackfillReturnStockLedger("default"); err != nil || again != 0 {
		t.Errorf("second run wrote %d (err %v), want 0", again, err)
	}
}
