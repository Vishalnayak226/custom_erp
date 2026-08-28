package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// handlePIMTransformRules lists the active rules and, in the same response,
// the function vocabulary the engine implements - the same one-call shape
// handlePIMWorkflows uses for its condition vocabulary, so a step editor
// never renders a function dropdown from a second, later request.
func handlePIMTransformRules(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodGet) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMTransformRule", "read")
	if !ok {
		return
	}
	rules, err := engines.ListPIMTransformRules(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"rules":     rules,
		"functions": engines.ListPIMTransformFunctions(),
	})
}
