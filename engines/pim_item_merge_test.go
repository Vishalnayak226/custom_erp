package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func mergeCleanup(schema string) {
	for _, q := range []string{
		"DELETE FROM %s.documents WHERE id IN ('MG-KEEP','MG-DUP','MG-PO-D','MG-PO-O','MG-BC')",
		"DELETE FROM %s.inventory_availability WHERE sku IN ('MG-KEEP','MG-DUP')",
		"DELETE FROM %s.documents WHERE doctype='StockLedgerEntry' AND data->>'item_id' IN ('MG-KEEP','MG-DUP')",
		"DELETE FROM %s.item_cost WHERE item_code IN ('MG-KEEP','MG-DUP')",
		"DELETE FROM %s.command_idempotency WHERE command = 'item.merge'",
	} {
		_, _ = db.DB.Exec(strings.Replace(q, "%s", schema, 1))
	}
}

func mergeOnHand(schema, sku string) (n float64) {
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(on_hand),0) FROM "+schema+".inventory_availability WHERE sku=$1", sku).Scan(&n)
	return
}

func mergeSeed(t *testing.T, schema string) {
	t.Helper()
	mergeCleanup(schema)
	lifecycleSeedItem(t, schema, "MG-KEEP", map[string]interface{}{"uom": "Nos", "barcode": "8900000000011"})
	lifecycleSeedItem(t, schema, "MG-DUP", map[string]interface{}{"uom": "Nos", "barcode": "8900000000028"})
	for sku, q := range map[string]int{"MG-KEEP": 3, "MG-DUP": 7} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1,'MG-LOC',$2,$2)", sku, q); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = db.DB.Exec("INSERT INTO " + schema + ".item_cost (item_code, cumulative_qty_received, cumulative_value_received_paise, avg_unit_cost_paise) VALUES ('MG-DUP',7,7000,1000),('MG-KEEP',3,1500,500) ON CONFLICT (item_code) DO NOTHING")
	enc, _ := json.Marshal(map[string]interface{}{"items": poLines("MG-DUP")})
	_, _ = db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('MG-PO-D','PurchaseOrder',$1,'Draft','system')", enc)
}

func TestItemMergeMovesStockAndRedirects(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	mergeSeed(t, schema)
	defer mergeCleanup(schema)

	if _, err := MergeItems("default", "MG-KEEP", "MG-DUP", "u", RoleCashier); err == nil || !strings.Contains(err.Error(), "only an admin") {
		t.Fatalf("a cashier must be refused, got %v", err)
	}
	if mergeOnHand(schema, "MG-DUP") != 7 {
		t.Fatal("refused merge moved stock")
	}
	res, err := MergeItems("default", "MG-KEEP", "MG-DUP", "u", RoleSuperAdmin)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if res["units_moved"].(float64) != 7 {
		t.Fatalf("result %v", res)
	}
	if mergeOnHand(schema, "MG-DUP") != 0 || mergeOnHand(schema, "MG-KEEP") != 10 {
		t.Fatalf("stock dup=%v keep=%v, want 0 and 10", mergeOnHand(schema, "MG-DUP"), mergeOnHand(schema, "MG-KEEP"))
	}
	// Reconciliation: the ledger nets to zero across the two items.
	var net float64
	_ = db.DB.QueryRow("SELECT COALESCE(SUM((data->>'qty')::numeric),0) FROM " + schema + ".documents WHERE doctype='StockLedgerEntry' AND data->>'voucher_type'='ItemMerge' AND data->>'item_id' IN ('MG-KEEP','MG-DUP')").Scan(&net)
	if net != 0 {
		t.Fatalf("merge ledger nets %v, want 0", net)
	}
	// Average cost: (3*500 + 7*1000) / 10 = 850.
	var avg int64
	_ = db.DB.QueryRow("SELECT avg_unit_cost_paise FROM " + schema + ".item_cost WHERE item_code='MG-KEEP'").Scan(&avg)
	if avg != 850 {
		t.Fatalf("blended cost %d, want 850", avg)
	}
	// The old code and old barcode resolve to the item that stays.
	for _, ref := range []string{"MG-DUP", "8900000000028"} {
		r, err := ResolveItemBySKU("default", ref)
		if err != nil || mergeCodeOf(r) != "MG-KEEP" {
			t.Fatalf("%s resolves to %v %v", ref, r, err)
		}
	}
	// The Draft PO now names the item that stays.
	var items string
	_ = db.DB.QueryRow("SELECT data->>'items' FROM " + schema + ".documents WHERE id='MG-PO-D'").Scan(&items)
	if !strings.Contains(items, "MG-KEEP") || strings.Contains(items, "MG-DUP") {
		t.Fatalf("draft not re-pointed: %s", items)
	}
	// Replay and a second merge move nothing more.
	if _, err := MergeItems("default", "MG-KEEP", "MG-DUP", "u", RoleSuperAdmin); err == nil {
		t.Fatal("a second merge of the same item must be refused")
	}
	if mergeOnHand(schema, "MG-KEEP") != 10 {
		t.Fatalf("second merge changed stock: %v", mergeOnHand(schema, "MG-KEEP"))
	}
}

func TestItemMergeRefusals(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	mergeSeed(t, schema)
	defer mergeCleanup(schema)

	if _, err := MergeItems("default", "MG-KEEP", "MG-KEEP", "u", RoleSuperAdmin); err == nil {
		t.Fatal("an item cannot merge into itself")
	}
	// Different tax: refused with the field named.
	_, _ = db.DB.Exec("UPDATE " + schema + ".documents SET data = data || '{\"gst_rate\": 12}' WHERE id='MG-DUP'")
	if _, err := MergeItems("default", "MG-KEEP", "MG-DUP", "u", RoleSuperAdmin); err == nil || !strings.Contains(err.Error(), "gst_rate") {
		t.Fatalf("tax mismatch: %v", err)
	}
	_, _ = db.DB.Exec("UPDATE " + schema + ".documents SET data = data || '{\"gst_rate\": 5}' WHERE id='MG-DUP'")
	// An open (approved) PO on the duplicate blocks the merge.
	enc, _ := json.Marshal(map[string]interface{}{"items": poLines("MG-DUP")})
	_, _ = db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('MG-PO-O','PurchaseOrder',$1,'Approved','system')", enc)
	if _, err := MergeItems("default", "MG-KEEP", "MG-DUP", "u", RoleSuperAdmin); err == nil || !strings.Contains(err.Error(), "open document") {
		t.Fatalf("open PO: %v", err)
	}
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id='MG-PO-O'")
	// Stock set aside blocks it.
	_, _ = db.DB.Exec("UPDATE " + schema + ".inventory_availability SET qc_hold = 1 WHERE sku='MG-DUP'")
	if _, err := MergeItems("default", "MG-KEEP", "MG-DUP", "u", RoleSuperAdmin); err == nil || !strings.Contains(err.Error(), "set aside") {
		t.Fatalf("held stock: %v", err)
	}
	if mergeOnHand(schema, "MG-DUP") != 7 || mergeOnHand(schema, "MG-KEEP") != 3 {
		t.Fatal("a refused merge moved stock")
	}
}

func TestItemMergeConcurrent(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	mergeSeed(t, schema)
	defer mergeCleanup(schema)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = MergeItems("default", "MG-KEEP", "MG-DUP", "u", RoleSuperAdmin)
		}()
	}
	wg.Wait()
	if mergeOnHand(schema, "MG-DUP") != 0 || mergeOnHand(schema, "MG-KEEP") != 10 {
		t.Fatalf("after 4 concurrent merges dup=%v keep=%v, want 0 and 10", mergeOnHand(schema, "MG-DUP"), mergeOnHand(schema, "MG-KEEP"))
	}
}
