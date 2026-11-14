package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 168.12 / 168.8: what attribute rules apply to a category (its own and
// inherited ones), and the operator's import of the workbook's category rules.

// GET /api/v1/pim/category-rules?category=<id> lists the effective rules.
func handlePIMCategoryRules(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := pimDataGate(w, r, "read", "CategoryAttributeRule", "ProductAttributeDef")
	if !ok {
		return
	}
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category == "" {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "category is required")
		return
	}
	rules, err := engines.EffectiveCategoryRules(tenantID, category)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"category": category, "rules": rules})
}

// POST /api/v1/platform/tenants/{tenant}/attribute-library/rules {"apply": bool, "industry": ""}
// previews (apply false) or adds the workbook's category rules for one client.
func handleApplyCategoryRules(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return
	}
	var req struct {
		Apply    bool   `json:"apply"`
		Industry string `json:"industry"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request")
		return
	}
	res, err := engines.ImportCategoryRules(target, controlPanelActor(r), req.Industry, req.Apply)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
