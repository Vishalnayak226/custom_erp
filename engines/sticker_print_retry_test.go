package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 55.19: a retried print with the same request key returns the labels
// again but writes the print history once; without a key, behaviour is as
// before (each request is its own run).
func TestStickerPrintRetryWritesHistoryOnce(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	for id, data := range map[string]map[string]interface{}{
		"PRN-1":    {"code": "PRN-1", "name": "Label printer", "status": "Active"},
		"ITEM-R-1": {"code": "R-1", "name": "Retry one", "barcode": "8900000000011"},
		"ITEM-R-2": {"code": "R-2", "name": "Retry two"},
	} {
		doctype := "Item"
		if id == "PRN-1" {
			doctype = "Printer"
		}
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatal(err)
		}
	}
	rows := func() int {
		var n int
		db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".sticker_print_log WHERE sku IN ('R-1', 'R-2')").Scan(&n)
		return n
	}
	key := StickerPrintOptions{RequestKey: "retry-key-1"}
	first, err := PrintStickers(tenantID, []string{"R-1", "R-2"}, "PRN-1", "admin", "", 2, key)
	if err != nil || len(first) != 2 {
		t.Fatalf("first print: %v %v", first, err)
	}
	again, err := PrintStickers(tenantID, []string{"R-1", "R-2"}, "PRN-1", "admin", "", 2, key)
	if err != nil || len(again) != 2 || again[0].Barcode != "8900000000011" || again[1].Barcode != "R-2" {
		t.Fatalf("a retry still returns the labels (barcode, or the SKU when there is none): %+v %v", again, err)
	}
	if rows() != 2 {
		t.Fatalf("history rows after a keyed retry = %d, want 2", rows())
	}
	if _, err := PrintStickers(tenantID, []string{"R-1"}, "PRN-1", "admin", "reprint: torn", 1); err != nil {
		t.Fatal(err)
	}
	if rows() != 3 {
		t.Fatalf("an unkeyed print is its own run: rows = %d, want 3", rows())
	}

	// Stage 133.3: a preview returns the same labels and writes no history,
	// even with the key a later Print reuses; that Print then logs once.
	preview := StickerPrintOptions{RequestKey: "preview-key-1", Preview: true}
	shown, err := PrintStickers(tenantID, []string{"R-1", "R-2"}, "PRN-1", "admin", "", 3, preview)
	if err != nil || len(shown) != 2 || shown[0].Barcode != "8900000000011" || shown[0].Name != "Retry one" {
		t.Fatalf("preview labels: %+v %v", shown, err)
	}
	if rows() != 3 {
		t.Fatalf("history rows after a preview = %d, want 3 (nothing logged)", rows())
	}
	if _, err := PrintStickers(tenantID, []string{"R-1", "R-2"}, "PRN-1", "admin", "", 3, StickerPrintOptions{RequestKey: "preview-key-1"}); err != nil {
		t.Fatal(err)
	}
	if rows() != 5 {
		t.Fatalf("the print after a preview logs its run: rows = %d, want 5", rows())
	}
	if _, err := PrintStickers(tenantID, []string{"R-1"}, "NO-SUCH-PRN", "admin", "", 1, preview); err == nil {
		t.Fatal("a preview still checks the printer like a print does")
	}
}
