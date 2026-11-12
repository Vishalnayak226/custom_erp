package server

import "testing"

// Stage 73.D.20: the one object-level read rule. A doctype the scope registry
// does not know falls back to the legacy location rule (Super Admin exempt),
// supplier scoping applies to any row carrying a supplier code, and a supplier
// account with no vendor sees nothing.
func TestStage73D20DocumentReadRuleVisible(t *testing.T) {
	const doctype = "T73D20Unregistered"
	cases := []struct {
		name string
		rule documentReadRule
		data map[string]interface{}
		want bool
	}{
		{"same store", documentReadRule{doctype: doctype, role: "Store Manager", location: "HO"}, map[string]interface{}{"location": "HO"}, true},
		{"other store", documentReadRule{doctype: doctype, role: "Store Manager", location: "HO"}, map[string]interface{}{"location": "JAY"}, false},
		{"other store via location_code", documentReadRule{doctype: doctype, role: "Store Manager", location: "HO"}, map[string]interface{}{"location_code": "JAY"}, false},
		{"Super Admin sees every store", documentReadRule{doctype: doctype, role: "Super Admin", location: "HO"}, map[string]interface{}{"location": "JAY"}, true},
		{"no store on the row", documentReadRule{doctype: doctype, role: "Store Manager", location: "HO"}, map[string]interface{}{"name": "x"}, true},
		{"own supplier", documentReadRule{doctype: doctype, role: supplierRole, supplierCode: "S1"}, map[string]interface{}{supplierScopeField: "S1"}, true},
		{"other supplier", documentReadRule{doctype: doctype, role: supplierRole, supplierCode: "S1"}, map[string]interface{}{supplierScopeField: "S2"}, false},
		{"supplier with no vendor", documentReadRule{doctype: doctype, role: supplierRole, noSupplier: true}, map[string]interface{}{"name": "x"}, false},
	}
	for _, c := range cases {
		if got := c.rule.visible(c.data); got != c.want {
			t.Errorf("%s: visible = %v, want %v", c.name, got, c.want)
		}
	}
}
