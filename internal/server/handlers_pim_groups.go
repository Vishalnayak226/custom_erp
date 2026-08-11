package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// handlePIMProductGroupMembers resolves both static and dynamic groups through
// one read endpoint. Group creation/editing remains on the generic document
// API, preserving the existing metadata/RBAC/audit machinery.
func handlePIMProductGroupMembers(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	allowed, err := checkPermission(tenantID, role, "PIMProductGroup", "read")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	resolved, err := engines.ResolvePIMProductGroup(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(resolved)
}
