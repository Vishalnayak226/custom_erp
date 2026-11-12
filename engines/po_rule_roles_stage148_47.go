package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strings"
)

// Stage 148.47: a PO routing rule names the role that approves. A mistyped
// name sent every PO to Super Admin only, so the save now refuses a role the
// client does not have, the form offers the real ones, and the list marks a
// saved rule whose role has gone. Rows already saved keep working (the
// approval engine is untouched); a rule switched Inactive is not checked, so
// an old bad row can still be turned off.

const poRuleRoleUnknownCode = "PURCHA-5100"

// TenantRoleNames is every role the client has: held by a user, granted any
// permission, or registered on the Roles screen, plus the built-in two.
func TenantRoleNames(tenantID string) []string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || db.DB == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n != "" && !seen[strings.ToLower(n)] {
			seen[strings.ToLower(n)] = true
			out = append(out, n)
		}
	}
	for _, q := range []string{
		`SELECT role FROM %[1]s.users`, `SELECT role FROM %[1]s.role_permissions`, `SELECT name FROM %[1]s.roles`,
	} {
		rows, err := db.DB.Query(fmt.Sprintf(q, schema))
		if err != nil {
			continue
		}
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				add(n)
			}
		}
		rows.Close()
	}
	add(RoleSuperAdmin)
	add(RoleAdmin)
	sort.Strings(out)
	return out
}

func roleKnownIn(known []string, role string) bool {
	for _, k := range known {
		if strings.EqualFold(k, role) || CanonicalRole(k) == CanonicalRole(role) {
			return true
		}
	}
	return false
}

var poRuleRoleFields = []string{"required_role", "escalate_role"}

// validatePORoutingRuleRoles is the save check (ValidateMasterDataRules, so the
// form, the API and a CSV upload all pass through it).
func validatePORoutingRuleRoles(tenantID string, payload map[string]interface{}) error {
	delete(payload, "role_check") // a mark the list adds, never stored
	if strings.EqualFold(strField(payload, "status"), "Inactive") {
		return nil
	}
	var known []string
	for _, f := range poRuleRoleFields {
		role := strings.TrimSpace(strField(payload, f))
		if role == "" {
			continue
		}
		if known == nil {
			known = TenantRoleNames(tenantID)
		}
		if !roleKnownIn(known, role) {
			return &ValidationError{Code: poRuleRoleUnknownCode, SubFor: f, Message: fmt.Sprintf("%q is not a role here. Pick one from the list.", role)}
		}
	}
	return nil
}

// OfferRoleChoices turns the role boxes of a PO routing rule into a list of
// the client's real roles (read by the doctype meta route).
func OfferRoleChoices(tenantID, doctype string, fields []FieldMeta) {
	if doctype != "PORoutingRule" {
		return
	}
	choices := strings.Join(TenantRoleNames(tenantID), ",")
	for i := range fields {
		for _, f := range poRuleRoleFields {
			if fields[i].Fieldname == f {
				fields[i].Fieldtype = "Select"
				fields[i].Options = choices
			}
		}
	}
}

// MarkUnknownRoles sets role_check on a listed rule whose role no longer exists.
func MarkUnknownRoles(tenantID, doctype string, docs []map[string]interface{}) {
	if doctype != "PORoutingRule" || len(docs) == 0 {
		return
	}
	known := TenantRoleNames(tenantID)
	for _, d := range docs {
		for _, f := range poRuleRoleFields {
			if role := strings.TrimSpace(strField(d, f)); role != "" && !roleKnownIn(known, role) {
				d["role_check"] = "Unknown role"
			}
		}
	}
}
