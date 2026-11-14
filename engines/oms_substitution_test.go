package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 73.4.2.5: a line swapped for its item's set substitute keeps its
// quantity and price, moves the reservation, and is refused when no
// substitute is set.
func TestStage73425SubstituteOrderLine(t *testing.T) {
	f := newCreditFixture(t)
	f.seed("ITEM-SUB", "Item", "Active", map[string]interface{}{"code": "ITEM-SUB", "name": "Carton XL", "sale_price": 550.0, "hsn_code": "4819", "tax_treatment": "Exempt"})
	if _, err := PostInventoryLedger(f.tenantID, "HO", []interface{}{map[string]interface{}{"sku": "ITEM-SUB", "qty": 20.0}}, false); err != nil {
		t.Fatal(err)
	}
	order := f.order("Asha", "", 5)
	lineOf := func() (id, sku string, qty int, price float64) {
		t.Helper()
		if err := db.DB.QueryRow(`SELECT id, data->>'sku', (data->>'qty')::int, (data->>'unit_price')::numeric FROM `+f.schema+`.documents
			WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL AND COALESCE(data->>'line_status', '') <> 'Cancelled'`, order).Scan(&id, &sku, &qty, &price); err != nil {
			t.Fatalf("line: %v", err)
		}
		return
	}
	lineID, _, _, _ := lineOf()

	if _, err := SubstituteOrderLine(f.tenantID, order, lineID, "mgr1"); codeOf(err) != "ORDERM-0467" {
		t.Fatalf("no substitute set: want ORDERM-0467, got %v", err)
	}

	if _, err := db.DB.Exec(`UPDATE ` + f.schema + `.documents SET data = data || '{"substitute_item": "ITEM-SUB"}' WHERE id = 'ITEM-CC'`); err != nil {
		t.Fatal(err)
	}
	if got := LineSubstitute(f.tenantID, "ITEM-CC"); got != "ITEM-SUB" {
		t.Fatalf("LineSubstitute: got %q", got)
	}
	if _, err := SubstituteOrderLine(f.tenantID, order, lineID, "mgr1"); err != nil {
		t.Fatalf("SubstituteOrderLine: %v", err)
	}
	_, sku, qty, price := lineOf()
	if sku != "ITEM-SUB" || qty != 5 || price != 500 {
		t.Fatalf("new line: got %s x%d at %.2f", sku, qty, price)
	}
	if s := f.field("SalesOrder", order, "order_status"); s != "Reserved" {
		t.Fatalf("order: want Reserved, got %s", s)
	}
	reservedOf := func(sku string) (n int) {
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(reserved), 0) FROM `+f.schema+`.inventory_availability WHERE sku = $1`, sku).Scan(&n)
		return
	}
	if reservedOf("ITEM-SUB") != 5 || reservedOf("ITEM-CC") != 0 {
		t.Fatalf("reserved: ITEM-SUB %d (want 5), ITEM-CC %d (want 0)", reservedOf("ITEM-SUB"), reservedOf("ITEM-CC"))
	}
}
