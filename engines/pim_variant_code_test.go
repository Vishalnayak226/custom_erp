package engines

// Stage 51.5 / 117 - PrepareItemVariantCode records a combination's variant
// values from its design and attribute fields; its Combination ID is the next
// Item number (PrepareMasterNumber), so no code is built here. "t1" has no
// tenant schema, so the duplicate lookup is skipped and these run as plain
// unit tests.

import "testing"

func TestPrepareItemVariantCodeFromFamily(t *testing.T) {
	payload := map[string]interface{}{
		"family":       "D00001",
		"metal_type":   "Gold",
		"purity_karat": "18k",
		"stone_type":   "Diamond",
	}
	if err := PrepareItemVariantCode("t1", true, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, set := payload["code"]; set {
		t.Errorf("code must be left for the Item series (Stage 117), got %v", payload["code"])
	}
	if payload["parent_product_code"] != "D00001" {
		t.Errorf("parent_product_code was not bridged from family: %v", payload["parent_product_code"])
	}
	wantOptions := "metal_type:Gold;purity_karat:18k;stone_type:Diamond"
	if payload["variant_option_values"] != wantOptions {
		t.Errorf("variant_option_values = %v, want %v", payload["variant_option_values"], wantOptions)
	}
}

// TestPrepareItemVariantCodeFashionJewelleryAttributes covers the
// color/polish attributes added after a real client migration (fashion/
// imitation jewellery, no karat/weight data at all) - metal_type/
// purity_karat alone don't cover this business model's variant dimensions.
func TestPrepareItemVariantCodeFashionJewelleryAttributes(t *testing.T) {
	payload := map[string]interface{}{
		"parent_product_code": "EI001",
		"color":               "Purple",
		"polish":              "Gold",
		"size":                "NA",
	}
	if err := PrepareItemVariantCode("t1", true, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "color:Purple;polish:Gold;size:NA"
	if payload["variant_option_values"] != want {
		t.Errorf("variant_option_values = %v, want %v", payload["variant_option_values"], want)
	}
}

func TestPrepareItemVariantCodeExplicitOptionsWinOverFields(t *testing.T) {
	payload := map[string]interface{}{
		"parent_product_code":   "RING001",
		"metal_type":            "Gold",
		"variant_option_values": "metal_type:Silver;size:7",
	}
	if err := PrepareItemVariantCode("t1", true, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The explicit variant_option_values already named metal_type - that
	// value wins over the plain metal_type field, it is not overwritten.
	want := "metal_type:Silver;size:7"
	if payload["variant_option_values"] != want {
		t.Errorf("variant_option_values = %v, want %v", payload["variant_option_values"], want)
	}
}

func TestPrepareItemVariantCodeNoOpCases(t *testing.T) {
	t.Run("edit is untouched", func(t *testing.T) {
		payload := map[string]interface{}{"family": "DESIGN1", "metal_type": "Gold"}
		if err := PrepareItemVariantCode("t1", false, payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, set := payload["variant_option_values"]; set {
			t.Errorf("variant values should not be set on an edit, got %v", payload["variant_option_values"])
		}
	})

	t.Run("explicit code is respected", func(t *testing.T) {
		payload := map[string]interface{}{"family": "DESIGN1", "code": "MY-OWN-CODE", "metal_type": "Gold"}
		if err := PrepareItemVariantCode("t1", true, payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if payload["code"] != "MY-OWN-CODE" {
			t.Errorf("code was overwritten: %v", payload["code"])
		}
	})

	t.Run("standalone item with no family/parent is untouched", func(t *testing.T) {
		payload := map[string]interface{}{"name": "Plain Item"}
		if err := PrepareItemVariantCode("t1", true, payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, set := payload["variant_option_values"]; set {
			t.Errorf("a standalone item has no variant values, got %v", payload["variant_option_values"])
		}
	})
}
