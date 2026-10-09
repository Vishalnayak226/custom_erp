package engines

import (
	"encoding/json"
	"strings"
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
	// Stage 58: the whole Item record rides along for the sticker studio.
	if l.Fields["gst_rate"] != "5" || l.Fields["tax_treatment"] != "Taxable" {
		t.Errorf("expected Item fields on the label, got %v", l.Fields)
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

// Stage 58: any scalar Item field reaches the label (so the studio can print
// weight/purity/size/MRP), nested values and oversized text do not, and an
// element naming such a field reads it through StickerFieldText.
func TestStickerItemFieldsAndFieldFallback(t *testing.T) {
	fields := stickerItemFields(map[string]interface{}{
		"gross_weight":  38.505,
		"purity_karat":  "925",
		"is_hallmarked": true,
		"empty":         "",
		"variants":      []interface{}{"a"},
		"huge":          strings.Repeat("x", 501),
	})
	want := map[string]string{"gross_weight": "38.505", "purity_karat": "925", "is_hallmarked": "true"}
	if len(fields) != len(want) {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
	for k, v := range want {
		if fields[k] != v {
			t.Errorf("fields[%q] = %q, want %q", k, fields[k], v)
		}
	}
	label := StickerLabel{SKU: "S1", Fields: fields}
	if got := StickerFieldText(StickerElement{Field: "gross_weight"}, label); got != "38.505" {
		t.Errorf("StickerFieldText(gross_weight) = %q", got)
	}
	if got := StickerFieldText(StickerElement{Field: "sku"}, label); got != "S1" {
		t.Errorf("StickerFieldText(sku) = %q", got)
	}
	if got := StickerFieldText(StickerElement{Field: "no_such_field"}, label); got != "" {
		t.Errorf("unknown field should print empty, got %q", got)
	}
}

// Stage 58: the server-side ZPL path (kept for API clients) understands the
// studio's text elements - custom text with {field} placeholders, prefix and
// suffix around a bound field - prints kind=barcode from any field, and skips
// kinds only the browser engine can draw instead of misprinting them.
func TestZPLStickerElementStage58Kinds(t *testing.T) {
	label := StickerLabel{SKU: "RNG-1", Barcode: "8901234567890", Fields: map[string]string{"gross_weight": "38.505"}}
	if got := StickerFieldText(StickerElement{Field: "custom", Text: "W:{gross_weight} gm / {sku}"}, label); got != "W:38.505 gm / RNG-1" {
		t.Errorf("custom text = %q", got)
	}
	if got := StickerFieldText(StickerElement{Field: "gross_weight", Prefix: "W:", Suffix: " gm"}, label); got != "W:38.505 gm" {
		t.Errorf("prefix/suffix = %q", got)
	}
	if got := StickerFieldText(StickerElement{Field: "custom", Text: "W:{net_weight} gm"}, label); got != "" {
		t.Errorf("custom text whose only field is blank should print nothing, got %q", got)
	}
	if got := StickerFieldText(StickerElement{Field: "purity", Prefix: "P:"}, label); got != "" {
		t.Errorf("blank field with prefix should print nothing, got %q", got)
	}
	if z := zplStickerElement(StickerElement{Kind: "barcode", Field: "sku", HMM: 8}, label, 203); !strings.Contains(z, "^FDRNG-1^FS") {
		t.Errorf("kind=barcode from sku: %q", z)
	}
	for _, kind := range []string{"qr", "line", "box", "image"} {
		if z := zplStickerElement(StickerElement{Kind: kind, Field: "sku", WMM: 10, HMM: 10}, label, 203); z != "" {
			t.Errorf("kind %s should be skipped on the server path, got %q", kind, z)
		}
	}
}
