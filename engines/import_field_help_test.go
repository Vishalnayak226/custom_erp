package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 47.13.2: a field with help text carries it in the meta and into the
// import dictionary's definition column; a field without keeps today's text.
func TestStage47132FieldHelpInMetaAndDictionary(t *testing.T) {
	db.InitDB(testConnStr())

	for doctype, fieldnames := range map[string][]string{
		"Item":          {"hsn_code", "tracking_mode", "mrp", "family", "barcode"},
		"Vendor":        {"gstin", "bank_ifsc"},
		"Customer":      {"price_list_code", "credit_limit", "loyalty_tier", "merged_into"},
		"PurchaseOrder": {"items", "gst_mode", "interstate_override", "payment_term"},
	} {
		fields, err := GetDocTypeMeta("default", doctype)
		if err != nil {
			t.Fatal(err)
		}
		specs, err := ImportTemplateDictionary("default", doctype)
		if err != nil {
			t.Fatal(err)
		}
		byField := map[string]FieldMeta{}
		for _, f := range fields {
			byField[f.Fieldname] = f
		}
		byColumn := map[string]ImportColumnSpec{}
		for _, s := range specs {
			byColumn[s.Column] = s
		}
		for _, name := range fieldnames {
			f, ok := byField[name]
			if !ok {
				t.Fatalf("%s.%s is not a field", doctype, name)
			}
			if f.Help == "" || len(f.Help) > 100 {
				t.Errorf("%s.%s: help = %q, want a few words", doctype, name, f.Help)
			}
			if def := byColumn[name].Definition; !strings.Contains(def, f.Help) {
				t.Errorf("%s.%s: definition %q does not carry the help %q", doctype, name, def, f.Help)
			}
		}
	}

	// A field with no help keeps the generated sentence.
	fields, _ := GetDocTypeMeta("default", "Item")
	for _, f := range fields {
		if f.Fieldname == "sticker_template" && f.Help != "" {
			t.Fatalf("sticker_template has help %q - pick another field for the fallback check", f.Help)
		}
	}
	specs, _ := ImportTemplateDictionary("default", "Item")
	for _, s := range specs {
		if s.Column == "sticker_template" && s.Definition != s.Label+" - which StickerTemplate this record refers to." {
			t.Errorf("fallback definition changed: %q", s.Definition)
		}
	}
}
