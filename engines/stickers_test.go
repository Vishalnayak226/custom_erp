package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// TestPrintStickersBarcodeSVG (Stage 42.1.11) locks down PrintStickers'
// wiring of a real Code 128 barcode into the browser print fallback: a label
// with a Code-Set-B-encodable barcode gets a non-empty BarcodeSVG, and the
// existing "unregistered SKU falls back to printing the SKU itself as the
// barcode" behaviour (Stage MB 15.3) still renders one too.
func TestPrintStickersBarcodeSVG(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sku = "SKU-STICKER-TEST"
	const printerCode = "PRN-STICKER-TEST"
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND data->>'code' = '" + sku + "'")
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Printer' AND data->>'code' = '" + printerCode + "'")
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".sticker_print_log WHERE sku LIKE 'SKU-STICKER-TEST%'")
	defer func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND data->>'code' = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Printer' AND data->>'code' = '" + printerCode + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".sticker_print_log WHERE sku LIKE 'SKU-STICKER-TEST%'")
	}()

	itemData, _ := json.Marshal(map[string]interface{}{
		"code": sku, "name": "Sticker Test Item", "barcode": "BC-" + sku,
		"hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 5,
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')",
		"ITEM-"+sku, itemData); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	printerData, _ := json.Marshal(map[string]interface{}{"code": printerCode, "name": "Sticker Test Printer", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Printer', $2, 'Active', 'system')",
		"PRNDOC-"+printerCode, printerData); err != nil {
		t.Fatalf("seed printer: %v", err)
	}

	labels, err := PrintStickers(tenantID, []string{sku}, printerCode, "system", "", 1)
	if err != nil {
		t.Fatalf("PrintStickers: %v", err)
	}
	if len(labels) != 1 {
		t.Fatalf("expected exactly 1 label, got %d", len(labels))
	}
	l := labels[0]
	if l.Barcode != "BC-"+sku {
		t.Errorf("expected barcode=BC-%s, got %q", sku, l.Barcode)
	}
	if l.BarcodeSVG == "" {
		t.Error("expected a non-empty BarcodeSVG for an encodable barcode value")
	}

	// An unregistered SKU still prints, falling back to the SKU itself as
	// the barcode (Stage MB 15.3) - it must still render a real barcode.
	unregLabels, err := PrintStickers(tenantID, []string{"SKU-STICKER-UNREGISTERED"}, printerCode, "system", "", 1)
	if err != nil {
		t.Fatalf("PrintStickers (unregistered): %v", err)
	}
	if len(unregLabels) != 1 || unregLabels[0].Barcode != "SKU-STICKER-UNREGISTERED" || unregLabels[0].BarcodeSVG == "" {
		t.Errorf("expected the unregistered SKU to fall back to itself as a rendered barcode, got %+v", unregLabels)
	}
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".sticker_print_log WHERE sku = 'SKU-STICKER-UNREGISTERED'")
}
