package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 49.4.6 (confusables): an identifier that mixes a look-alike alphabet
// with another is refused at the one validation choke point, naming the
// field; a name or description keeps every script, and an identifier written
// wholly in one alphabet passes.
func TestStage4946MixedScriptIdentifiersAreRefused(t *testing.T) {
	for s, bad := range map[string]bool{
		"ITEM-01":       false,
		"item_01":       false,
		"ITEМ-01":       true,  // Cyrillic Em among Latin letters
		"ΑBC":           true,  // Greek Alpha among Latin letters
		"ПР-1":          false, // wholly Cyrillic
		"αβ":            false, // wholly Greek
		"साड़ी-01":      false, // Devanagari only
		"SAREE-साड़ी":   false, // Latin and Devanagari: neither imitates the other
		"a@exаmple.com": true,  // Cyrillic a in a Latin address
		"":              false,
		"12345":         false,
	} {
		if _, got := mixedScriptRune(s); got != bad {
			t.Errorf("%q: refused=%v, want %v", s, got, bad)
		}
	}
	for field, want := range map[string]bool{
		"code": true, "item_code": true, "sku": true, "barcode": true, "gstin": true, "pan": true,
		"ifsc_code": true, "email": true, "website": true, "account_number": true, "hsn_code": true,
		"customer_id": true, "lot_no": true, "serial_number": true, "batch_no": true, "id": true,
		"name": false, "description": false, "address": false, "notes": false, "item_name": false,
		"phone": false, "mobile_number": false, "city": false, "remarks": false, "reason": false,
	} {
		if got := IsIdentifierField(field); got != want {
			t.Errorf("IsIdentifierField(%q) = %v, want %v", field, got, want)
		}
	}
	if IsIdentifierField("mobile_number") {
		t.Error("a phone field is not an identifier (its own normaliser owns it)")
	}

	db.InitDB(testConnStr())
	err := ValidateDocument("default", "Department", map[string]interface{}{"code": "D-М4946", "name": "Mixed", "status": "Active"})
	verr, ok := err.(*ValidationError)
	if !ok || verr.Code != "GLOBAL-0311" || verr.SubFor != "code" {
		t.Fatalf("a code with a Cyrillic letter among Latin ones must be refused with GLOBAL-0311 on the field, got %v", err)
	}
	// A name mixes scripts freely - Hindi, Arabic, a Cyrillic brand in a Latin
	// sentence - and a code in one alphabet passes.
	for _, doc := range []map[string]interface{}{
		{"code": "D-4946-A", "name": "Sales Москва North", "status": "Active"},
		{"code": "D-4946-B", "name": "बिक्री Sales فستان", "status": "Active"},
		{"code": "ПР-4946", "name": "Cyrillic code", "status": "Active"},
	} {
		if err := ValidateDocument("default", "Department", doc); err != nil {
			t.Errorf("%v must pass: %v", doc, err)
		}
	}
}
