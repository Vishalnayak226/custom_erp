package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 55.14 guided HR journeys (engines/hr_journeys.go).

// POST /api/v1/hr/onboarding/{id}/steps {index, done} - tick or untick one
// step; who and when are stamped by the server. Needs update permission on
// OnboardingChecklist, like any write to it, and the checklist must be in the
// session's scope (the engine checks that - stricter than the generic write,
// which does not scope writes).
func handleOnboardingStep(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	allowed, err := checkPermission(tenantID, role, "OnboardingChecklist", "update")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var req struct {
		Index *int `json:"index"`
		Done  bool `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Index == nil {
		writeAPIError(w, r, "HR-0533", "")
		return
	}
	actor := r.Header.Get("Resolved-Username")
	if actor == "" {
		actor = r.Header.Get("Resolved-User-ID")
	}
	session := engines.SessionScope{
		Role: role, UserID: r.Header.Get("Resolved-User-ID"), LocationCode: r.Header.Get("Resolved-Location"),
	}
	progress, err := engines.SetOnboardingStep(tenantID, r.PathValue("id"), *req.Index, req.Done, actor, session)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(progress)
}

// scopePendingApprovals is the approvals inbox (GET /api/v1/approval/pending)
// under the document API's own rules: an HR record (a self-scoped doctype:
// Grievance, Appraisal, ExpenseClaim...) shows only to its employee unless the
// role holds HR's self-scope exemption, and the fields a role may not read
// are removed. Without it every pending Grievance - no location, so it passed
// the inbox's location filter - reached every signed-in role with its
// description. Only the self dimension is applied: the inbox's location
// filter is unchanged, so no approver loses a document it routes to them.
func scopePendingApprovals(r *http.Request, docs []map[string]interface{}) ([]map[string]interface{}, error) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	base := engines.SessionScope{Role: role, UserID: r.Header.Get("Resolved-User-ID"), LocationCode: r.Header.Get("Resolved-Location")}
	type doctypeRule struct {
		self   []engines.ScopeConstraint
		hidden []string
	}
	rules := map[string]*doctypeRule{}
	out := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		doctype, _ := doc["doctype"].(string)
		rule, ok := rules[doctype]
		if !ok {
			session, err := engines.ResolveSessionScope(tenantID, doctype, base)
			if err != nil {
				return nil, err
			}
			rule = &doctypeRule{}
			if constraints, known := engines.ScopeConstraints(doctype, session); known {
				for _, c := range constraints {
					if c.Dimension == engines.ScopeSelf {
						rule.self = append(rule.self, c)
					}
				}
			}
			if rule.hidden, err = engines.HiddenFieldsForRole(tenantID, role, doctype); err != nil {
				return nil, err
			}
			rules[doctype] = rule
		}
		if len(rule.self) > 0 && engines.DocumentMatchesScope(rule.self, doc) != "" {
			continue
		}
		// 73.D.2: a record that passed the self check is the session's own,
		// and its own fields (a grievance's content) stay readable.
		ownFields := map[string]bool{}
		if len(rule.self) > 0 {
			for _, f := range engines.OwnRecordReadableFields(doctype) {
				ownFields[f] = true
			}
		}
		for _, field := range rule.hidden {
			if !ownFields[field] {
				delete(doc, field)
			}
		}
		out = append(out, doc)
	}
	return out, nil
}

// GET /api/v1/hr/grievance-access - who can read a grievance, and what this
// session can do with one, derived from the enforced policy.
func handleGrievanceAccess(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	access, err := engines.GetGrievanceAccess(tenantID, role)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	canRead, err := checkPermission(tenantID, role, "Grievance", "read")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"content_readers": access.ContentReaders, "case_list_roles": access.CaseListRoles,
		"reviewer_role": access.ReviewerRole, "you": access.You, "you_can_read": canRead,
	})
}
