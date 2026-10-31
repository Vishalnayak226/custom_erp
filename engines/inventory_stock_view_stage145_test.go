package engines

import (
	"custom_erp/db"
	"testing"

	"github.com/lib/pq"
)

// Stage 145: Stock on Hand and the Inventory Dashboard show a stocked SKU by
// its design - the short Item Name ("Antique Necklace"), Design No, barcode
// and its attributes (the design's, overlaid by the combination's).
func TestInventoryStockPageByDesign(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		loc      = "TEST-S145-LOC"
		design   = "TEST-S145-D1"
		sku      = "TEST-S145-D1-RED"
		bare     = "TEST-S145-BARE"
		itemName = "TEST-S145-IN"
		category = "TEST-S145-CAT"
		attr     = "TEST-S145-AC"
	)
	ids := []string{design, sku, bare, itemName, category, attr}
	cleanup := func() {
		if _, err := db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id = ANY($1)`, pq.Array(ids)); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		if _, err := db.DB.Exec(`DELETE FROM `+schema+`.inventory_availability WHERE location_code = $1`, loc); err != nil {
			t.Fatalf("cleanup stock: %v", err)
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, d := range []struct{ id, doctype, data string }{
		{category, "Category", `{"code":"` + category + `","name":"Antique"}`},
		{itemName, "ItemName", `{"code":"` + itemName + `","name":"Antique Necklace","category":"` + category + `"}`},
		{attr, "ItemAttribute", `{"code":"` + attr + `","label":"Polish","value_type":"Select","options":"Gold,Silver"}`},
		{design, "ProductFamily", `{"code":"` + design + `","name":"Antique Necklace","item_name":"` + itemName + `","design_attributes":"` + attr + `:Gold;STONE:Ruby"}`},
		{sku, "Item", `{"code":"` + sku + `","name":"Antique Necklace Gold Ruby Red Free Size","family":"` + design + `","category":"` + category + `","barcode":"8901450001","variant_option_values":"COLOUR:Red;STONE:Ruby"}`},
		{bare, "Item", `{"code":"` + bare + `","name":"Plain bag"}`},
	} {
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')`, d.id, d.doctype, d.data); err != nil {
			t.Fatalf("insert %s: %v", d.id, err)
		}
	}
	for _, s := range []struct {
		sku string
		qty int
	}{{sku, 3}, {bare, 2}} {
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)`, s.sku, loc, s.qty); err != nil {
			t.Fatalf("stock: %v", err)
		}
	}

	rows, total, cols, err := InventoryStockPage("default", CurrentStockFilter{Location: loc, Limit: 10})
	if err != nil {
		t.Fatalf("InventoryStockPage: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("total %d, rows %d, want 2", total, len(rows))
	}
	var got map[string]interface{}
	for _, r := range rows {
		if r["sku"] == sku {
			got = r
		}
	}
	if got == nil {
		t.Fatalf("no row for %s: %v", sku, rows)
	}
	if got["item_name"] != "Antique Necklace" || got["design_no"] != design || got["barcode"] != "8901450001" || got["category"] != "Antique" {
		t.Errorf("row = %v, want Antique Necklace / %s / its barcode / Antique", got, design)
	}
	attrs, _ := got["attributes"].(map[string]string)
	if attrs[attr] != "Gold" || attrs["STONE"] != "Ruby" || attrs["COLOUR"] != "Red" {
		t.Errorf("attributes = %v, want the design's Polish and STONE plus the combination's COLOUR", attrs)
	}
	have := map[string]bool{}
	for _, c := range cols {
		have[c.Code] = true
	}
	if !have[attr] || !have["COLOUR"] {
		t.Errorf("attribute columns %v miss %s or COLOUR", cols, attr)
	}
	for _, r := range rows {
		if r["sku"] == bare && (r["item_name"] != "Plain bag" || r["design_no"] != "") {
			t.Errorf("an item with no design: %v", r)
		}
	}

	// The search finds an item by its barcode and by its name.
	for _, q := range []string{"8901450001", "antique necklace gold"} {
		if found, n, err := GetCurrentStockPage("default", CurrentStockFilter{Q: q, Location: loc, Limit: 10}); err != nil || n != 1 || found[0]["sku"] != sku {
			t.Errorf("search %q: %v rows %v err %v", q, n, found, err)
		}
	}

	d, err := InventoryDashboard("default", loc)
	if err != nil {
		t.Fatalf("InventoryDashboard: %v", err)
	}
	if d["designs"] != int64(2) || d["combinations"] != int64(2) || d["total_units"] != int64(5) {
		t.Errorf("totals designs %v combinations %v units %v, want 2 / 2 / 5", d["designs"], d["combinations"], d["total_units"])
	}
	byItem, _ := d["by_item"].([]map[string]interface{})
	if len(byItem) != 2 || byItem[0]["name"] != "Antique Necklace" || byItem[0]["units"] != int64(3) {
		t.Errorf("by_item = %v, want Antique Necklace first with 3", byItem)
	}
	if _, ok := d["low_stock"]; ok {
		t.Error("the dashboard still lists low stock")
	}
}

// TestInventoryStockPageHostileQuery: the release DAST (49.10.4) sends
// q=%00 with extreme paging; the page must refuse or ignore it, never fail
// with a database error (a 500 at the 0.21.0 release gate).
func TestInventoryStockPageHostileQuery(t *testing.T) {
	spInitDB()
	for _, f := range []CurrentStockFilter{
		{Q: "\x00", Limit: 500},
		{Q: "\x00", Location: "HO", Limit: 500},
		{Q: "a\x00b", Location: "HO", Limit: 1, Offset: 1 << 30},
	} {
		if _, _, _, err := InventoryStockPage("default", f); err != nil {
			t.Errorf("InventoryStockPage(%q, %q): %v", f.Q, f.Location, err)
		}
	}
}
