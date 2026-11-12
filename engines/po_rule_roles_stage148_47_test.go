package engines

import (
	"errors"
	"testing"
)

// Stage 148.47: a PO routing rule names a role that exists.
func TestStage14847PORoutingRuleRoleMustExist(t *testing.T) {
	tenantID, _, _ := poRulesTenant(t) // users hold Cashier, Store Manager, Accountant
	code := func(err error) string {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return ve.Code
		}
		return ""
	}
	cases := []struct {
		name string
		rule map[string]interface{}
		want string
	}{
		{"known role", map[string]interface{}{"required_role": "Accountant"}, ""},
		{"known role, other case", map[string]interface{}{"required_role": "accountant"}, ""},
		{"typo", map[string]interface{}{"required_role": "Acountant"}, "PURCHA-5100"},
		{"typo in the escalation role", map[string]interface{}{"required_role": "Accountant", "escalate_role": "Stor Manager"}, "PURCHA-5100"},
		{"escalation blank", map[string]interface{}{"required_role": "Store Manager", "escalate_role": ""}, ""},
		{"an old bad row switched off", map[string]interface{}{"required_role": "Acountant", "status": "Inactive"}, ""},
	}
	for _, c := range cases {
		c.rule["rule_name"] = "R1"
		err := ValidateDocument(tenantID, "PORoutingRule", c.rule)
		if got := code(err); got != c.want {
			t.Errorf("%s: code %q (%v), want %q", c.name, got, err, c.want)
		}
	}
}

func TestStage14847RoleChoicesAndListMark(t *testing.T) {
	tenantID, _, _ := poRulesTenant(t)
	fields := []FieldMeta{{Fieldname: "required_role", Fieldtype: "Data"}, {Fieldname: "rule_name", Fieldtype: "Data"}}
	OfferRoleChoices(tenantID, "PORoutingRule", fields)
	if fields[0].Fieldtype != "Select" || fields[0].Options == "" || fields[1].Fieldtype != "Data" {
		t.Fatalf("role field not offered as a list: %+v", fields)
	}
	docs := []map[string]interface{}{{"required_role": "Accountant"}, {"required_role": "Gone Role"}}
	MarkUnknownRoles(tenantID, "PORoutingRule", docs)
	if docs[0]["role_check"] != nil || docs[1]["role_check"] != "Unknown role" {
		t.Fatalf("list marks wrong: %v", docs)
	}
	other := []FieldMeta{{Fieldname: "required_role", Fieldtype: "Data"}}
	OfferRoleChoices(tenantID, "Vendor", other)
	if other[0].Fieldtype != "Data" {
		t.Fatal("another doctype changed")
	}
}
