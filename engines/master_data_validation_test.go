package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestItemTaxClassificationRequired locks down Stage 30.1.2: an Item that
// cannot be sold or purchased must not be saveable in the first place. Before
// this, hsn_code/gst_rate were optional at the master layer and required at
// the transaction layer, so the app happily created products that failed at
// the till with "Tax configuration is missing".
func TestItemTaxClassificationRequired(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"

	base := func(extra map[string]interface{}) map[string]interface{} {
		p := map[string]interface{}{"code": "TEST-ITEMRULE-01", "name": "Item Rule Tee"}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}

	// Stage 26.6.11 split the rejection codes. HSN problems stay MASTER-0042
	// ("HSN code is required for this item"); rate problems moved to
	// MASTER-0044 ("Tax category is required for this item"), because the item
	// failing to say HOW it is taxed is what is actually wrong - the old code
	// showed an HSN headline over a GST-rate detail.
	for _, tc := range []struct {
		name    string
		code    string
		payload map[string]interface{}
	}{
		{"no hsn_code at all", "MASTER-0042", base(map[string]interface{}{"gst_rate": 12.0})},
		{"blank hsn_code", "MASTER-0042", base(map[string]interface{}{"hsn_code": "", "gst_rate": 12.0})},
		{"no hsn_code even when Exempt", "MASTER-0042", base(map[string]interface{}{"tax_treatment": "Exempt", "gst_rate": 0})},
		{"no gst_rate", "MASTER-0044", base(map[string]interface{}{"hsn_code": "6109"})},
		{"zero gst_rate with no treatment declared", "MASTER-0044", base(map[string]interface{}{"hsn_code": "6109", "gst_rate": 0})},
		{"blank gst_rate", "MASTER-0044", base(map[string]interface{}{"hsn_code": "6109", "gst_rate": ""})},
		{"explicitly Taxable but zero-rated", "MASTER-0044", base(map[string]interface{}{"hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 0})},
		{"unrecognized treatment", "MASTER-0044", base(map[string]interface{}{"hsn_code": "6109", "tax_treatment": "Exmept", "gst_rate": 0})},
		{"Exempt but carrying a positive rate", "MASTER-0044", base(map[string]interface{}{"hsn_code": "6109", "tax_treatment": "Exempt", "gst_rate": 5.0})},
		{"Nil-Rated but carrying a positive rate", "MASTER-0044", base(map[string]interface{}{"hsn_code": "6109", "tax_treatment": "Nil-Rated", "gst_rate": 12.0})},
	} {
		t.Run("rejected: "+tc.name, func(t *testing.T) {
			err := ValidateMasterDataRules(tenantID, "TEST-ITEMRULE-01", "Item", tc.payload)
			if err == nil {
				t.Fatalf("expected a rejection, item saved clean")
			}
			ve, ok := err.(*ValidationError)
			if !ok || ve.Code != tc.code {
				t.Fatalf("expected %s, got %#v", tc.code, err)
			}
			// The audit's actual complaint: the message must name the field.
			if ve.Message == "" {
				t.Fatalf("%s raised with no message naming the field", tc.code)
			}
		})
	}

	t.Run("a fully classified item still saves", func(t *testing.T) {
		// Unique name/family-free payload so the duplicate-name check
		// (Stage 26.4.2) can't interfere.
		p := base(map[string]interface{}{"hsn_code": "6109", "gst_rate": 12.0})
		if err := ValidateMasterDataRules(tenantID, "TEST-ITEMRULE-01", "Item", p); err != nil {
			t.Fatalf("unexpected rejection: %v", err)
		}
	})

	// Stage 26.6.11's actual point: genuinely untaxed goods become creatable,
	// but only by saying so. A 0 rate on its own is still rejected above.
	for _, treatment := range []string{TaxTreatmentExempt, TaxTreatmentNilRated, TaxTreatmentZeroRated} {
		t.Run("accepted: "+treatment+" item at 0%", func(t *testing.T) {
			p := base(map[string]interface{}{"hsn_code": "1006", "tax_treatment": treatment, "gst_rate": 0})
			if err := ValidateMasterDataRules(tenantID, "TEST-ITEMRULE-01", "Item", p); err != nil {
				t.Fatalf("a %s item at 0%% must be saveable: %v", treatment, err)
			}
		})
		t.Run("accepted: "+treatment+" item with no rate entered at all", func(t *testing.T) {
			p := base(map[string]interface{}{"hsn_code": "1006", "tax_treatment": treatment})
			if err := ValidateMasterDataRules(tenantID, "TEST-ITEMRULE-01", "Item", p); err != nil {
				t.Fatalf("a %s item with no rate must be saveable: %v", treatment, err)
			}
		})
	}

	t.Run("a malformed HSN is still caught by its own rule", func(t *testing.T) {
		p := base(map[string]interface{}{"hsn_code": "61", "gst_rate": 12.0})
		err := ValidateMasterDataRules(tenantID, "TEST-ITEMRULE-01", "Item", p)
		ve, ok := err.(*ValidationError)
		if !ok || ve.Code != "MASTER-0043" {
			t.Fatalf("expected MASTER-0043 for a 2-digit HSN, got %#v", err)
		}
	})
}

// TestNormalizeTaxTreatment covers the one function every other Stage 26.6.11
// rule leans on. The blank case matters most: it is what makes the field
// additive - every Item written before this stage has no tax_treatment at all,
// and reading those as Taxable is what keeps 30.1.2's "rate must be > 0" rule
// applying to them unchanged.
func TestNormalizeTaxTreatment(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"", TaxTreatmentTaxable},
		{"   ", TaxTreatmentTaxable},
		{"Taxable", TaxTreatmentTaxable},
		{"taxable", TaxTreatmentTaxable},
		{"Exempt", TaxTreatmentExempt},
		{" EXEMPT ", TaxTreatmentExempt},
		{"exempted", TaxTreatmentExempt},
		{"Nil-Rated", TaxTreatmentNilRated},
		{"nil rated", TaxTreatmentNilRated},
		{"nil_rated", TaxTreatmentNilRated},
		{"NILRATED", TaxTreatmentNilRated},
		{"Zero-Rated", TaxTreatmentZeroRated},
		{"zero rated", TaxTreatmentZeroRated},
		{"export", TaxTreatmentZeroRated},
	} {
		got, ok := NormalizeTaxTreatment(tc.raw)
		if !ok || got != tc.want {
			t.Errorf("NormalizeTaxTreatment(%q) = %q, %v; want %q, true", tc.raw, got, ok, tc.want)
		}
	}

	// A typo must NOT fall back to Taxable - that would charge tax on goods
	// the user was trying to mark exempt, silently.
	for _, raw := range []string{"Exmept", "none", "0%", "N/A", "Composition"} {
		if got, ok := NormalizeTaxTreatment(raw); ok {
			t.Errorf("NormalizeTaxTreatment(%q) = %q, true; want a rejection", raw, got)
		}
	}
}

// TestLottableConstraintMasterRules (Stage 42.1.7) locks down the two things
// LottableConstraint's generic metadata pass cannot express: allowed_values
// must name at least one real value, and (customer, item, attribute_key) is
// unique - with item treated as part of that key even when blank, so a
// customer-wide wildcard row and a SKU-specific override don't collide.
func TestLottableConstraintMasterRules(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'LottableConstraint' AND data->>'customer' = 'CUST-LOTCON-TEST'")
	}
	cleanup()
	defer cleanup()

	payload := map[string]interface{}{
		"customer": "CUST-LOTCON-TEST", "item": "SKU-LOTCON-01",
		"attribute_key": "country_of_origin", "allowed_values": "IN,US", "status": "Active",
	}
	if err := ValidateMasterDataRules(tenantID, "LOTCON-TEST-01", "LottableConstraint", payload); err != nil {
		t.Fatalf("expected a well-formed constraint to validate, got %v", err)
	}

	blank := map[string]interface{}{"customer": "CUST-LOTCON-TEST", "attribute_key": "grade", "allowed_values": " , "}
	if err := ValidateMasterDataRules(tenantID, "LOTCON-TEST-02", "LottableConstraint", blank); err == nil {
		t.Error("expected allowed_values with no real value to be refused")
	}

	data, _ := json.Marshal(payload)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'LottableConstraint', $2, 'Active', 'system')",
		"LOTCON-TEST-01", data); err != nil {
		t.Fatalf("seed constraint: %v", err)
	}

	dup := map[string]interface{}{
		"customer": "CUST-LOTCON-TEST", "item": "SKU-LOTCON-01",
		"attribute_key": "country_of_origin", "allowed_values": "CN",
	}
	if err := ValidateMasterDataRules(tenantID, "LOTCON-TEST-03", "LottableConstraint", dup); err == nil {
		t.Error("expected a duplicate (customer, item, attribute_key) to be refused")
	}

	other := map[string]interface{}{
		"customer": "CUST-LOTCON-TEST", "item": "SKU-LOTCON-02",
		"attribute_key": "country_of_origin", "allowed_values": "IN",
	}
	if err := ValidateMasterDataRules(tenantID, "LOTCON-TEST-04", "LottableConstraint", other); err != nil {
		t.Errorf("expected a different item to not collide: %v", err)
	}

	if err := ValidateMasterDataRules(tenantID, "LOTCON-TEST-01", "LottableConstraint", payload); err != nil {
		t.Errorf("expected editing the same row to not collide with itself: %v", err)
	}
}
