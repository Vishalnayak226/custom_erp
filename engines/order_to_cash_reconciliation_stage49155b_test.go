package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 49.5.5 (next slice): per-SKU lines and stock ownership as exceptions,
// both dismissible and reopenable through the same framework.
func TestStage49155PerSkuAndStockOwnership(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	lines := func(l ...interface{}) string {
		var out []map[string]interface{}
		for i := 0; i+1 < len(l); i += 2 {
			out = append(out, map[string]interface{}{"sku": l[i], "qty": l[i+1]})
		}
		b, _ := json.Marshal(out)
		return string(b)
	}
	// SO-S: shipped A5+B2, invoiced A2+B5: totals match (7 = 7), SKUs do not
	insert("SO-S", "SalesOrder", "Confirmed", map[string]interface{}{"customer": "C1"})
	insert("SP-S", "ShippingPackage", "Shipped", map[string]interface{}{"order_id": "SO-S", "items": lines("A", 5, "B", 2)})
	insert("SI-S", "SalesInvoice", "Approved", map[string]interface{}{"sales_order_id": "SO-S", "items": lines("A", 2, "B", 5), "total_amount": 700.0, "customer": "C1"})
	// SO-T: shipped A3, invoiced A1: the order row names the SKU
	insert("SO-T", "SalesOrder", "Confirmed", map[string]interface{}{"customer": "C2"})
	insert("SP-T", "ShippingPackage", "Shipped", map[string]interface{}{"order_id": "SO-T", "items": lines("A", 3)})
	insert("SI-T", "SalesInvoice", "Approved", map[string]interface{}{"sales_order_id": "SO-T", "items": lines("A", 1), "total_amount": 100.0, "customer": "C2"})

	find := func(rows []OrderToCashRow, kind, ref string) *OrderToCashRow {
		for i := range rows {
			if rows[i].Kind == kind && rows[i].Ref == ref {
				return &rows[i]
			}
		}
		return nil
	}
	// stock ownership: LOC-1 mixed, LOC-2 owned by OWN-A but holds OWN-B stock, LOC-3 clean
	for _, q := range []string{
		"INSERT INTO " + schema + ".bin_stock_owner (bin_code, sku, owner_id, location_code, qty) VALUES ('B1','A','OWN-A','LOC-1',4),('B2','A','OWN-B','LOC-1',6)",
		"INSERT INTO " + schema + ".warehouse_owner (location_code, owner_id) VALUES ('LOC-2','OWN-A'),('LOC-3','OWN-A')",
		"INSERT INTO " + schema + ".bin_stock_owner (bin_code, sku, owner_id, location_code, qty) VALUES ('B3','A','OWN-B','LOC-2',3),('B4','A','OWN-A','LOC-3',9)",
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rows, err := OrderToCashMismatches(tenantID, false)
	if err != nil {
		t.Fatalf("OrderToCashMismatches: %v", err)
	}
	if r := find(rows, "SKU shipped not invoiced", "SO-S|A"); r == nil || r.Sku != "A" || r.Difference != 3 {
		t.Errorf("SO-S|A: %+v, want SKU A shipped 3 more than invoiced", r)
	}
	if r := find(rows, "SKU invoiced more than shipped", "SO-S|B"); r == nil || r.Difference != 3 {
		t.Errorf("SO-S|B: %+v, want SKU B invoiced 3 more than shipped", r)
	}
	if r := find(rows, "Shipped not invoiced", "SO-T"); r == nil || !strings.Contains(r.Sku, "A (shipped 3, invoiced 1)") {
		t.Errorf("SO-T: %+v, want the order row to name SKU A", r)
	}
	if r := find(rows, "Stock of more than one owner", "LOC-1"); r == nil || r.Difference != 2 || r.ShippedQty != 10 {
		t.Errorf("LOC-1: %+v, want 2 owners, qty 10", r)
	}
	if r := find(rows, "Stock not from the owner", "LOC-2"); r == nil || r.Sku != "OWN-B" || r.ShippedQty != 3 {
		t.Errorf("LOC-2: %+v, want OWN-B stock of 3", r)
	}
	for _, r := range rows {
		if r.Ref == "LOC-3" {
			t.Errorf("a clean location was listed: %+v", r)
		}
	}

	// dismiss, then reopen, a stock-ownership mismatch
	if err := SetReconciliationException(tenantID, "Stock of more than one owner", "LOC-1", "Dismissed", "3PL transition", "", "asha"); err != nil {
		t.Fatalf("dismiss stock ownership: %v", err)
	}
	rows, _ = OrderToCashMismatches(tenantID, false)
	if find(rows, "Stock of more than one owner", "LOC-1") != nil {
		t.Errorf("dismissed stock-ownership mismatch still listed")
	}
	if err := SetReconciliationException(tenantID, "Stock of more than one owner", "LOC-1", "Open", "back on", "", "asha"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	rows, _ = OrderToCashMismatches(tenantID, false)
	if find(rows, "Stock of more than one owner", "LOC-1") == nil {
		t.Errorf("reopened stock-ownership mismatch not listed again")
	}
	// the figures change: a dismissal lapses
	_ = SetReconciliationException(tenantID, "Stock of more than one owner", "LOC-1", "Dismissed", "again", "", "asha")
	if _, err := db.DB.Exec("UPDATE " + schema + ".bin_stock_owner SET qty = 7 WHERE bin_code = 'B2'"); err != nil {
		t.Fatal(err)
	}
	rows, _ = OrderToCashMismatches(tenantID, false)
	if find(rows, "Stock of more than one owner", "LOC-1") == nil {
		t.Errorf("a dismissal should lapse when the quantity changes")
	}
}
