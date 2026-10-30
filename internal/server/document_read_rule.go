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
func selfScopeWriteAllowed(schema, doctype, target string, scope engines.SessionScope, payload map[string]interface{}) selfScopeVerdict {
	constraints, known := engines.ScopeConstraints(doctype, scope)
	if !known {
		return selfScopeOK
	}
	for _, c := range constraints {
		if c.Dimension != engines.ScopeSelf || len(c.Fields) == 0 {
			continue
		}
		field := c.Fields[0]
		if target != "" {
			// Stage 49.4.7: a stored record that is not theirs (a colleague's,
			// or any record when no Employee is linked) is a record they cannot
			// reach - the caller answers it as "not found".
			var stored string
			if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'%s', '') FROM %s.documents WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL`, field, schema),
				target, doctype).Scan(&stored); err == nil && (c.Value == "" || stored != c.Value) {
				return selfScopeOtherRecord
			}
		}
		if c.Value == "" {
			return selfScopeRefused // no linked Employee: no record of this kind is theirs
		}
		given := ""
		if v, ok := payload[field]; ok && v != nil {
			given = strings.TrimSpace(fmt.Sprintf("%v", v))
		}
		if given != "" && given != c.Value {
			return selfScopeRefused
		}
		if given == "" {
			payload[field] = c.Value
		}
	}
	return selfScopeOK
}

// selfScopeVerdict is what selfScopeWriteAllowed decided. selfScopeRefused
// is a new record filed under someone else's code (403: nothing to
// disclose); selfScopeOtherRecord is a stored record that is not theirs
// (404, Stage 49.4.7's disclosure rule, like a read).
type selfScopeVerdict int

const (
	selfScopeOK selfScopeVerdict = iota
	selfScopeRefused
	selfScopeOtherRecord
)

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
		if hasLoc && fmt.Sprintf("%v", docLoc) != rule.location && !engines.HasFullAccess(rule.role) {
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
