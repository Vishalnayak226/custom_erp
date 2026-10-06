package engines

import (
	"custom_erp/db"
	"database/sql"
	"testing"
	"time"
)

// Stage 57.7: receipt-date barcodes are valid EAN-13s in the GS1 internal
// range, carry the day, and cannot collide with stable "020..." codes.
func TestDatedEANFromParts(t *testing.T) {
	day := time.Date(2026, 10, 6, 15, 0, 0, 0, time.Local)
	code, err := datedEANFromParts(day, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 13 || code[:8] != "02261006" || code[8:12] != "0007" {
		t.Fatalf("got %q, want 02 + 261006 + 0007 + check digit", code)
	}
	check, err := gs1CheckDigit(code[:12])
	if err != nil || int(code[12]-'0') != check {
		t.Fatalf("bad check digit on %q", code)
	}
	if code[:3] == "020" {
		t.Fatalf("dated code %q collides with the stable 020 range", code)
	}
	if _, err := datedEANFromParts(day, 10000); err == nil {
		t.Fatal("a 10,000th code in one day should be refused, not wrapped")
	}
	if _, err := datedEANFromParts(day, 0); err == nil {
		t.Fatal("sequence 0 should be refused")
	}
}

// Stage 57.7: with per-receipt-date barcodes on, a receipt registers one
// dated code per SKU per day, that code scans as the SKU, the receipt's
// stickers get it, and a second receipt the same day reuses it. With
// generation off, no permanent barcode is issued at receipt.
func TestEnsureReceiptBarcodesPerDate(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('Item', 'TEE-RED-M', '{"code":"TEE-RED-M","name":"Tee Red M"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if err := SetSetting(tenantID, "inventory.barcode_generate_at", "off", "test"); err != nil {
		t.Fatalf("set generate_at: %v", err)
	}
	if err := SetSetting(tenantID, "inventory.barcode_per_receipt_date", "yes", "test"); err != nil {
		t.Fatalf("set per_receipt_date: %v", err)
	}
	items := []interface{}{map[string]interface{}{"sku": "TEE-RED-M", "qty": 4.0}}

	EnsureReceiptBarcodes(tenantID, "GRN-T1", items)

	var permanent sql.NullString
	if err := db.DB.QueryRow(`SELECT data->>'barcode' FROM ` + schemaName + `.documents WHERE id='TEE-RED-M'`).Scan(&permanent); err != nil {
		t.Fatal(err)
	}
	if permanent.Valid && permanent.String != "" {
		t.Errorf("generation is off, but receipt issued permanent barcode %q", permanent.String)
	}
	dated := ReceiptBarcodeFor(tenantID, "TEE-RED-M", "GRN-T1")
	if len(dated) != 13 || dated[:2] != "02" {
		t.Fatalf("receipt-date barcode = %q, want an 02-range EAN-13", dated)
	}
	resolved, err := ResolveItemBySKU(tenantID, dated)
	if err != nil || resolved.ID != "TEE-RED-M" || resolved.MatchedOn != "registered_barcode" {
		t.Fatalf("scanning %s did not resolve to the SKU: %+v %v", dated, resolved, err)
	}

	EnsureReceiptBarcodes(tenantID, "GRN-T2", items) // same day, second receipt
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schemaName + `.documents WHERE doctype='ItemBarcode' AND data->>'item'='TEE-RED-M'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("want one dated barcode per SKU per day, got %d", n)
	}
	if again := ReceiptBarcodeFor(tenantID, "TEE-RED-M", "GRN-T2"); again != dated {
		t.Errorf("same-day second receipt's stickers got %q, want the day's %q", again, dated)
	}
}
