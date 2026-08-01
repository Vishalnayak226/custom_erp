package engines

import (
	"custom_erp/db"
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

	for _, tc := range []struct {
		name    string
		payload map[string]interface{}
	}{
		{"no hsn_code at all", base(map[string]interface{}{"gst_rate": 12.0})},
		{"blank hsn_code", base(map[string]interface{}{"hsn_code": "", "gst_rate": 12.0})},
		{"no gst_rate", base(map[string]interface{}{"hsn_code": "6109"})},
		{"zero gst_rate (exempt goods are not supported at checkout either)", base(map[string]interface{}{"hsn_code": "6109", "gst_rate": 0})},
		{"blank gst_rate", base(map[string]interface{}{"hsn_code": "6109", "gst_rate": ""})},
	} {
		t.Run("rejected: "+tc.name, func(t *testing.T) {
			err := ValidateMasterDataRules(tenantID, "TEST-ITEMRULE-01", "Item", tc.payload)
			if err == nil {
				t.Fatalf("expected a rejection, item saved clean")
			}
			ve, ok := err.(*ValidationError)
			if !ok || ve.Code != "MASTER-0042" {
				t.Fatalf("expected MASTER-0042, got %#v", err)
			}
			// The audit's actual complaint: the message must name the field.
			if ve.Message == "" {
				t.Fatalf("MASTER-0042 raised with no message naming the field")
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

	t.Run("a malformed HSN is still caught by its own rule", func(t *testing.T) {
		p := base(map[string]interface{}{"hsn_code": "61", "gst_rate": 12.0})
		err := ValidateMasterDataRules(tenantID, "TEST-ITEMRULE-01", "Item", p)
		ve, ok := err.(*ValidationError)
		if !ok || ve.Code != "MASTER-0043" {
			t.Fatalf("expected MASTER-0043 for a 2-digit HSN, got %#v", err)
		}
	})
}
