package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 49.5.6a: what counts as an item's ERP barcode for an offline sale's
// check - its barcode, its GTIN, its receipt-date barcodes, and its SKU only
// when it has no barcode (the code its sticker prints then). Anything else,
// or no scan at all, is a mismatch.
func TestStage4956aOfflineSaleBarcodeCheck(t *testing.T) {
	db.InitDB(testConnStr())
	var present bool
	if err := db.DB.QueryRow(`SELECT to_regclass('tenant_default.pos_offline_sales') IS NOT NULL`).Scan(&present); err != nil || !present {
		t.Skip("migrations_stage49_5_6a_offline_sale_validation.sql is not applied to this database")
	}
	s := stage4911Suffix()
	labelled, bare := "OFS-L-"+s, "OFS-B-"+s
	seed := func(id, doctype string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')`, id, doctype, b); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id) })
	}
	seed(labelled, "Item", map[string]interface{}{"code": labelled, "name": "Labelled", "barcode": "890" + s, "gtin": "0890" + s})
	seed(bare, "Item", map[string]interface{}{"code": bare, "name": "Bare"})
	seed("DATED-"+s, "ItemBarcode", map[string]interface{}{"code": "DATED-" + s, "item": labelled, "kind": "Dated"})

	lines, matched, err := CheckOfflineSaleLines("default", []OfflineSaleItem{
		{Sku: labelled, Qty: 1, Scanned: "890" + s},         // its barcode
		{Sku: labelled, Qty: 1, Scanned: "0890" + s},        // its GTIN
		{Sku: labelled, Qty: 1, Scanned: "DATED-" + s},      // a receipt-date barcode
		{Sku: bare, Qty: 1, Scanned: bare},                  // no barcode: the SKU is the label
		{Sku: labelled, Qty: 1, Scanned: labelled},          // has a barcode: the SKU is not one
		{Sku: labelled, Qty: 1, Scanned: ""},                // searched, not scanned
		{Sku: bare, Qty: 1, Scanned: "890" + s},             // another item's barcode
		{Sku: "OFS-NONE-" + s, Qty: 1, Scanned: "anything"}, // no such item
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, true, true, true, false, false, false, false}
	for i, l := range lines {
		if l.Match != want[i] {
			t.Errorf("line %d (%s scanned %q, ERP %v): match %v, want %v", i, l.Sku, l.Scanned, l.ERPBarcodes, l.Match, want[i])
		}
	}
	if matched != 4 {
		t.Errorf("matched %d, want 4", matched)
	}
	if lines[0].Name != "Labelled" {
		t.Errorf("line name %q, want the item's name", lines[0].Name)
	}
}
