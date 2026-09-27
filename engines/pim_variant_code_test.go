package engines

// Stage 51.5 - GenerateVariantCode (engines/numbering.go) existed with no
// caller at all before this Stage; PrepareItemVariantCode is its first real
// caller. Both are pure functions over their arguments (no DB access), so
// these run as plain unit tests, no tenant schema/Postgres needed.

import "testing"

func TestGenerateVariantCodeIsDeterministic(t *testing.T) {
	attrs := map[string]string{"metal_type": "Gold", "purity_karat": "18k", "stone_type": "Diamond"}
	want := "PARENT-Gold-18k-Diamond" // keys sorted: metal_type, purity_karat, stone_type
	for i := 0; i < 20; i++ {
		got := GenerateVariantCode("t1", "PARENT", "", attrs)
		if got != want {
			t.Fatalf("iteration %d: GenerateVariantCode = %q, want %q (map iteration order must be sorted, not randomized)", i, got, want)
		}
	}
}

func TestGenerateVariantCodeCustomPattern(t *testing.T) {
	attrs := map[string]string{"metal_type": "Gold", "purity_karat": "18k"}
	got := GenerateVariantCode("t1", "PARENT", "{Parent}/{metal_type}/{purity_karat}", attrs)
	want := "PARENT/Gold/18k"
	if got != want {
		t.Fatalf("GenerateVariantCode with pattern = %q, want %q", got, want)
	}
}

func TestPrepareItemVariantCodeFromFamily(t *testing.T) {
	payload := map[string]interface{}{
		"family":       "Design/HQ/26-27/000001",
		"metal_type":   "Gold",
		"purity_karat": "18k",
		"stone_type":   "Diamond",
	}
	if err := PrepareItemVariantCode("t1", true, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantCode := "Design/HQ/26-27/000001-Gold-18k-Diamond"
	if payload["code"] != wantCode {
		t.Errorf("code = %v, want %v", payload["code"], wantCode)
	}
	if payload["parent_product_code"] != "Design/HQ/26-27/000001" {
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
	wantCode := "EI001-Purple-Gold-NA" // sorted: color, polish, size
	if payload["code"] != wantCode {
		t.Errorf("code = %v, want %v", payload["code"], wantCode)
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
	wantCode := "RING001-Silver-7"
	if payload["code"] != wantCode {
		t.Errorf("code = %v, want %v", payload["code"], wantCode)
	}
}

func TestPrepareItemVariantCodeNoOpCases(t *testing.T) {
	t.Run("edit is untouched", func(t *testing.T) {
		payload := map[string]interface{}{"family": "DESIGN1", "metal_type": "Gold"}
		if err := PrepareItemVariantCode("t1", false, payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, set := payload["code"]; set {
			t.Errorf("code should not be set on an edit, got %v", payload["code"])
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
		if _, set := payload["code"]; set {
			t.Errorf("code should not be set for a standalone item, got %v", payload["code"])
		}
	})
}
