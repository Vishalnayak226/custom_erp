package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// Stage 85.D1: the bill number is the store's, at most 16 characters,
// consecutive, and a sale that rolls back gives its number back.
func TestPOSBillNumberSeries(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const store = "S85D-STORE"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", store)
		db.DB.Exec("DELETE FROM "+schema+".sequence_counters WHERE doc_type = 'POSBILL' AND store_code = $1", store)
	}
	cleanup()
	defer cleanup()
	raw, _ := json.Marshal(map[string]interface{}{"code": store, "name": "S85D", "type": "Store", "short_code": "JAY"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", store, raw); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	draw := func(commit bool) string {
		tx, err := db.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		n, err := issuePOSBillNumberTx(tx, schema, store, when)
		if err != nil {
			tx.Rollback()
			t.Fatalf("issue: %v", err)
		}
		if commit {
			tx.Commit()
		} else {
			tx.Rollback()
		}
		return n
	}
	first := draw(true)
	if len(first) > 16 || first[:4] != "BJAY" || first[len(first)-6:] != "000001" {
		t.Fatalf("first bill number %q: want BJAY<fy>000001, at most 16 characters", first)
	}
	lost := draw(false)
	second := draw(true)
	if lost != second {
		t.Errorf("a rolled-back sale must give its number back: rolled back %s, next %s", lost, second)
	}
	if second[len(second)-6:] != "000002" {
		t.Errorf("second bill number %q should end 000002", second)
	}
}
