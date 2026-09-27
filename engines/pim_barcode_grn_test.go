package engines

// Stage 51.6 - GenerateEANBarcode existed only behind a standalone, manual
// PIM action before this Stage; EnsureItemBarcodes is its first automatic
// caller (wired into GRN receipt posting in
// internal/server/handlers_core_doc_engine.go). Uses its own throwaway
// tenant schema (uniqueLifecycleTenant/ProvisionTenantSchema, same pattern
// as TestIndustryLockAndOverride) so the Item rows this test writes never
// touch the shared "default" dev tenant.

import (
	"custom_erp/db"
	"testing"
)

func TestEnsureItemBarcodesGeneratesOnlyWhenMissing(t *testing.T) {
	db.InitDB(testConnStr())

	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	// Item A: no barcode yet - should get one generated.
	if _, err := db.DB.Exec(
		`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		 ('Item', 'ITEM-A', '{"code":"ITEM-A","name":"No Barcode Item"}', 'Active', 'admin')`,
	); err != nil {
		t.Fatalf("seed Item A: %v", err)
	}
	// Item B: already has a barcode - must be left untouched.
	if _, err := db.DB.Exec(
		`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		 ('Item', 'ITEM-B', '{"code":"ITEM-B","name":"Has Barcode","barcode":"1234567890123"}', 'Active', 'admin')`,
	); err != nil {
		t.Fatalf("seed Item B: %v", err)
	}

	items := []interface{}{
		map[string]interface{}{"sku": "ITEM-A", "qty": 5.0},
		map[string]interface{}{"sku": "ITEM-A", "qty": 3.0}, // duplicate SKU in the same receipt - must not generate twice
		map[string]interface{}{"sku": "ITEM-B", "qty": 1.0},
		map[string]interface{}{"sku": "ITEM-NONEXISTENT", "qty": 1.0}, // must not error/panic
	}
	EnsureItemBarcodes(tenantID, items)

	var barcodeA string
	if err := db.DB.QueryRow(`SELECT data->>'barcode' FROM ` + schemaName + `.documents WHERE doctype='Item' AND id='ITEM-A'`).Scan(&barcodeA); err != nil {
		t.Fatalf("read back Item A: %v", err)
	}
	if len(barcodeA) != 13 {
		t.Errorf("Item A barcode = %q, want a 13-digit EAN-13", barcodeA)
	}

	var barcodeB string
	if err := db.DB.QueryRow(`SELECT data->>'barcode' FROM ` + schemaName + `.documents WHERE doctype='Item' AND id='ITEM-B'`).Scan(&barcodeB); err != nil {
		t.Fatalf("read back Item B: %v", err)
	}
	if barcodeB != "1234567890123" {
		t.Errorf("Item B's existing barcode was overwritten: %q", barcodeB)
	}

	// Calling it again for the same items must not reassign Item A's barcode
	// - idempotent, since a caller passing the same SKU twice (or across two
	// otherwise-unrelated calls) must not mint a second code for it.
	EnsureItemBarcodes(tenantID, items)
	var barcodeA2 string
	if err := db.DB.QueryRow(`SELECT data->>'barcode' FROM ` + schemaName + `.documents WHERE doctype='Item' AND id='ITEM-A'`).Scan(&barcodeA2); err != nil {
		t.Fatalf("read back Item A (2nd pass): %v", err)
	}
	if barcodeA2 != barcodeA {
		t.Errorf("Item A's barcode changed on a second call: %q -> %q", barcodeA, barcodeA2)
	}
}
