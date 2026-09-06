package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 47.1.5/47.1.6/47.1.7 - the administrator surface for role templates,
// the SoD catalog and the "why allowed / why denied" preview.
//
// Every route here is LevelAdmin in route_capabilities.go, so Super-Admin-only
// is enforced in one place (apiMiddleware) rather than re-checked in each
// handler - the exact pattern 47.1.1 established and these handlers therefore
// do not repeat.

func handleListRoleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	type templateView struct {
		Name         string            `json:"name"`
		Summary      string            `json:"summary"`
		LegacyNames  []string          `json:"legacy_names,omitempty"`
		Modules      map[string]string `json:"modules"`
		Doctypes     map[string]string `json:"doctype_overrides,omitempty"`
		Capabilities []string          `json:"capabilities,omitempty"`
		GlobalScopes []string          `json:"global_scopes,omitempty"`
	}
	out := []templateView{}
	for _, template := range engines.RoleTemplates() {
		view := templateView{
			Name: template.Name, Summary: template.Summary,
			LegacyNames: template.LegacyNames, Capabilities: template.Capabilities,
			Modules: map[string]string{}, Doctypes: map[string]string{},
		}
		for module, level := range template.Modules {
			view.Modules[module] = level.String()
		}
		for doctype, level := range template.Doctypes {
			view.Doctypes[doctype] = level.String()
		}
		for _, dimension := range template.GlobalScopes {
			view.GlobalScopes = append(view.GlobalScopes, string(dimension))
		}
		out = append(out, view)
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handleRoleTemplatePlan is the before/after diff. It changes nothing, which
// is the entire point: an owner approves what they have seen.
func handleRoleTemplatePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	plan, err := engines.PlanRoleTemplateMigration(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(plan)
}

func handleApplyRoleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Could not read the request body")
		return
	}
	// The approver is the signed-in administrator, never a value the caller
	// supplies - an approval you can type is not an approval.
	approvedBy := r.Header.Get("Resolved-Username")
	if approvedBy == "" {
		approvedBy = r.Header.Get("Resolved-User-ID")
	}
	id, err := engines.ApplyRoleTemplateMigration(r.Header.Get("Resolved-Tenant-ID"), req.Roles, approvedBy)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"migration_id": id, "approved_by": approvedBy})
}

func handleRevertRoleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		MigrationID string `json:"migration_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Could not read the request body")
		return
	}
	revertedBy := r.Header.Get("Resolved-Username")
	if revertedBy == "" {
		revertedBy = r.Header.Get("Resolved-User-ID")
	}
	if err := engines.RevertRoleTemplateMigration(r.Header.Get("Resolved-Tenant-ID"), req.MigrationID, revertedBy); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reverted", "migration_id": req.MigrationID})
}

func handleListRoleTemplateMigrations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	records, err := engines.ListRoleTemplateMigrations(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(records)
}

// handleAccessPreview answers "may this role do this, and why" in one call -
// 47.1.6's administrator preview. Without ?doctype it answers at the role
// level (capabilities, global scopes, SoD); with one, it adds that doctype's
// stored vs template grant, scope reasons and sensitive-field access.
func handleAccessPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	role := r.URL.Query().Get("role")
	if role == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Name the role to preview, e.g. ?role=Cashier")
		return
	}
	explanation, err := engines.ExplainAccess(r.Header.Get("Resolved-Tenant-ID"), role, r.URL.Query().Get("doctype"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	// The route layer's own answer for this role, so the preview reflects
	// what apiMiddleware would actually do rather than only what the
	// template declares.
	capabilities := map[string]bool{}
	for capability, allowed := range capabilityRoleAllowlist {
		capabilities[capability] = false
		for _, want := range allowed {
			if want == engines.RoleSuperAdmin && engines.IsSuperAdmin(role) {
				capabilities[capability] = true
				break
			}
			if want == role {
				capabilities[capability] = true
				break
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"access":                explanation,
		"restricted_capability": capabilities,
	})
}

func handleSoDCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	type roleFindings struct {
		Role      string               `json:"role"`
		Conflicts []engines.SoDFinding `json:"conflicts"`
	}
	// Every role that actually has grants in this tenant, not only the
	// template roles - a custom role is exactly where an unnoticed conflict
	// lives.
	roles, err := engines.RolesWithStoredGrants(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	findings := []roleFindings{}
	for _, role := range roles {
		explanation, err := engines.ExplainAccess(tenantID, role, "")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		findings = append(findings, roleFindings{Role: role, Conflicts: explanation.Conflicts})
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"catalog": engines.SoDConflicts(),
		"by_role": findings,
	})
}
