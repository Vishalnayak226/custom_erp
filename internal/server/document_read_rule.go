package server

import (
	"fmt"
	"net/http"
	"strings"

	"custom_erp/db"
	"custom_erp/engines"
)

// selfScopeWriteAllowed is the write half of self scope (Stage 73.D.7). A
// session confined to its own records of a doctype (a self dimension in
// ScopeConstraints - Employee Self-Service, Cashier; not a role holding the
// global self scope) writes only its own: a new record is filed under its own
// employee code (one naming another employee is refused, a blank one is
// filled in), and an update must target a record already its own. Same shape
// as the supplier write scope. target is the record being written, "" for a
// create.
func selfScopeWriteAllowed(schema, doctype, target string, scope engines.SessionScope, payload map[string]interface{}) bool {
	constraints, known := engines.ScopeConstraints(doctype, scope)
	if !known {
		return true
	}
	for _, c := range constraints {
		if c.Dimension != engines.ScopeSelf || len(c.Fields) == 0 {
			continue
		}
		field := c.Fields[0]
		if c.Value == "" {
			return false // no linked Employee: no record of this kind is theirs
		}
		given := ""
		if v, ok := payload[field]; ok && v != nil {
			given = strings.TrimSpace(fmt.Sprintf("%v", v))
		}
		if given != "" && given != c.Value {
			return false
		}
		if target != "" {
			var stored string
			if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'%s', '') FROM %s.documents WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL`, field, schema),
				target, doctype).Scan(&stored); err == nil && stored != c.Value {
				return false
			}
		}
		if given == "" {
			payload[field] = c.Value
		}
	}
	return true
}

// documentReadRule is the one object-level read rule (Stage 73.D.20): may this
// session see this one document. It used to exist three times - inline in
// handleGenericDoc's single-document GET, in purchaseReturnInScope (which
// skipped the legacy location rule and supplier scoping) and here for
// attachments - and every caller now builds it from the same pieces: the
// session's role, store and supplier, and the doctype's session scope
// (engines.ResolveSessionScope). The doctype-level half (role_permissions,
// module gate) stays with each route, which knows what it is answering.
type documentReadRule struct {
	doctype      string
	role         string
	location     string
	supplierCode string
	// noSupplier: a Supplier account with no Vendor linked sees nothing.
	noSupplier bool
	scope      engines.SessionScope
}

// sessionReadRule resolves who is asking, for one doctype. It writes no
// response, so a route that has already checked permissions (or answers in
// its own words) can use it.
func sessionReadRule(r *http.Request, tenantID, doctype string) (documentReadRule, error) {
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	rule := documentReadRule{doctype: doctype, role: role, location: r.Header.Get("Resolved-Location")}
	if role == supplierRole {
		code, err := engines.SupplierCodeForUser(tenantID, userID)
		if err != nil {
			return rule, err
		}
		rule.supplierCode = code
		rule.noSupplier = code == ""
	}
	scope, err := engines.ResolveSessionScope(tenantID, doctype,
		engines.SessionScope{Role: role, UserID: userID, LocationCode: rule.location})
	if err != nil {
		return rule, err
	}
	rule.scope = scope
	return rule, nil
}

// visible is the object-level check: the doctype's scope dimensions
// (engines.ScopeConstraints / DocumentMatchesScope), or the legacy location
// rule for a doctype the scope registry does not know, then the supplier
// scope - a row that carries a supplier_code must match the session's.
func (rule documentReadRule) visible(data map[string]interface{}) bool {
	if rule.noSupplier {
		return false
	}
	if constraints, known := engines.ScopeConstraints(rule.doctype, rule.scope); known {
		if engines.DocumentMatchesScope(constraints, data) != "" {
			return false
		}
	} else {
		docLoc, hasLoc := data["location"]
		if !hasLoc {
			docLoc, hasLoc = data["location_code"]
		}
		if hasLoc && fmt.Sprintf("%v", docLoc) != rule.location && !engines.IsSuperAdmin(rule.role) {
			return false
		}
	}
	if rule.supplierCode != "" {
		if docSup, ok := data[supplierScopeField]; ok && fmt.Sprintf("%v", docSup) != rule.supplierCode {
			return false
		}
	}
	return true
}
