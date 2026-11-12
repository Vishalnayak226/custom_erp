package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// TestMirroredFields (Stage 30.5.6) covers PurchaseOrder's vendor/vendor_id
// pair: two mandatory fields registered independently at different points in
// this project's history, both still read by different consumers, which the
// generic form used to render as two indistinguishable required boxes.
func TestMirroredFields(t *testing.T) {
	t.Run("primary fills the mirror", func(t *testing.T) {
		p := map[string]interface{}{"vendor": "VEND01"}
		PrepareMirroredFields("PurchaseOrder", p)
		if p["vendor_id"] != "VEND01" {
			t.Errorf("expected vendor_id filled from vendor, got %v", p["vendor_id"])
		}
	})

	// The API contract this widens rather than narrows: callers that have only
	// ever sent vendor_id must keep working.
	t.Run("mirror fills the primary", func(t *testing.T) {
		p := map[string]interface{}{"vendor_id": "VEND02"}
		PrepareMirroredFields("PurchaseOrder", p)
		if p["vendor"] != "VEND02" {
			t.Errorf("expected vendor filled from vendor_id, got %v", p["vendor"])
		}
	})

	t.Run("primary wins when both are set and differ", func(t *testing.T) {
		p := map[string]interface{}{"vendor": "VEND01", "vendor_id": "STALE"}
		PrepareMirroredFields("PurchaseOrder", p)
		if p["vendor_id"] != "VEND01" {
			t.Errorf("expected the primary to win, got %v", p["vendor_id"])
		}
	})

	t.Run("blank on both sides stays blank", func(t *testing.T) {
		// Must not invent a value - ValidateDocument's mandatory check is what
		// should reject this, with a message naming the field.
		p := map[string]interface{}{"vendor": "", "vendor_id": "  "}
		PrepareMirroredFields("PurchaseOrder", p)
		if strings.TrimSpace(payloadString(p, "vendor")) != "" {
			t.Errorf("expected vendor left blank, got %q", p["vendor"])
		}
	})

	t.Run("no-op for a doctype with no registered pair", func(t *testing.T) {
		p := map[string]interface{}{"name": "Blue"}
		PrepareMirroredFields("Color", p)
		if len(p) != 1 {
			t.Errorf("expected the payload untouched, got %v", p)
		}
	})

	// The flag the generic form reads must come from the same registry that
	// does the copying - that is the whole point of deriving it.
	t.Run("meta marks the mirror and not the primary", func(t *testing.T) {
		db.InitDB(testConnStr())
		fields, err := GetDocTypeMeta("default", "PurchaseOrder")
		if err != nil {
			t.Fatalf("get meta: %v", err)
		}
		var sawPrimary, sawMirror bool
		for _, f := range fields {
			switch f.Fieldname {
			case "vendor":
				sawPrimary = true
				if f.Mirrored {
					t.Error("vendor is the primary and must stay on the form")
				}
			case "vendor_id":
				sawMirror = true
				if !f.Mirrored {
					t.Error("vendor_id should be flagged mirrored so the form drops it")
				}
			}
		}
		if !sawPrimary || !sawMirror {
			t.Fatal("PurchaseOrder should declare both vendor and vendor_id")
		}
	})
}

// TestJSONFieldValidation (Stage 30.5.3) covers the format check the two new
// structured fieldtypes brought with them. Before this, these were plain Data
// fields with no check of any kind, so a malformed string saved happily and
// only failed much later inside the MRP explosion.
func TestJSONFieldValidation(t *testing.T) {
	componentsSpec := `[{"key":"sku","label":"Component SKU","type":"link","link":"Item","required":true},
	                    {"key":"qty","label":"Qty per Unit","type":"number","required":true},
	                    {"key":"scrap_percent","label":"Scrap %","type":"number"}]`

	table := FieldMeta{Fieldname: "components", Label: "Components", Fieldtype: "JSONTable", Options: componentsSpec}

	t.Run("a well-formed array passes", func(t *testing.T) {
		if err := validateJSONTableValue(table, `[{"sku":"SKU1","qty":2},{"sku":"SKU2","qty":1,"scrap_percent":5}]`); err != nil {
			t.Fatalf("expected valid components to pass, got %v", err)
		}
	})

	t.Run("malformed JSON is rejected", func(t *testing.T) {
		err := validateJSONTableValue(table, `[{"sku"`)
		if err == nil {
			t.Fatal("expected malformed JSON to be rejected")
		}
		if verr, ok := err.(*ValidationError); !ok || verr.Code != "GLOBAL-0002" {
			t.Errorf("expected GLOBAL-0002, got %v", err)
		}
	})

	t.Run("a JSON object where an array is required is rejected", func(t *testing.T) {
		if err := validateJSONTableValue(table, `{"sku":"SKU1"}`); err == nil {
			t.Fatal("expected an object to be rejected for a JSONTable field")
		}
	})

	t.Run("a row missing a required column is rejected by line number", func(t *testing.T) {
		err := validateJSONTableValue(table, `[{"sku":"SKU1","qty":2},{"sku":"SKU2"}]`)
		if err == nil {
			t.Fatal("expected the row missing qty to be rejected")
		}
		verr, ok := err.(*ValidationError)
		if !ok {
			t.Fatalf("expected *ValidationError, got %T", err)
		}
		if !strings.Contains(verr.Message, "line 2") {
			t.Errorf("the message should name the offending line, got %q", verr.Message)
		}
		if !strings.Contains(verr.Message, "Qty per Unit") {
			t.Errorf("the message should name the missing column by its label, got %q", verr.Message)
		}
	})

	t.Run("an optional column may be absent", func(t *testing.T) {
		if err := validateJSONTableValue(table, `[{"sku":"SKU1","qty":2}]`); err != nil {
			t.Fatalf("scrap_percent is optional, got %v", err)
		}
	})

	// A mistyped spec is a configuration bug. Refusing every save on that
	// doctype would lock the tenant out of it, so the value is accepted.
	t.Run("a malformed column spec does not block saves", func(t *testing.T) {
		broken := FieldMeta{Fieldname: "components", Label: "Components", Fieldtype: "JSONTable", Options: `not json`}
		if err := validateJSONTableValue(broken, `[{"anything":1}]`); err != nil {
			t.Fatalf("expected a broken spec to fail open, got %v", err)
		}
	})

	t.Run("the new fieldtypes are accepted by SaveFieldDefinition's guard", func(t *testing.T) {
		for _, ft := range []string{"JSONTable", "JSONMap"} {
			if !validFieldTypes[ft] {
				t.Errorf("%s must be a valid fieldtype or the migration's rows become unsaveable", ft)
			}
		}
	})
}
