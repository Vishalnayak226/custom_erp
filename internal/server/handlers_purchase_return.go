package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 57.15: Purchase Return. Creating and editing a return goes through
// the generic doc API (/api/v1/doc/PurchaseReturn, validated by
// engines.validatePurchaseReturnRules); these two routes add what that API
// cannot express - what a GRN can still return, and Post.

// purchaseReturnInScope applies the same object-level scope rule the generic
// doc API applies to a single record, so a location-scoped user can neither
// read another location's GRN through the context call nor post another
// location's return.
func purchaseReturnInScope(r *http.Request, tenantID, doctype string, data map[string]interface{}) (bool, error) {
	session := engines.SessionScope{
		Role: r.Header.Get("Resolved-Role"), UserID: r.Header.Get("Resolved-User-ID"),
		LocationCode: r.Header.Get("Resolved-Location"),
	}
	session, err := engines.ResolveSessionScope(tenantID, doctype, session)
	if err != nil {
		return false, err
	}
	constraints, known := engines.ScopeConstraints(doctype, session)
	if !known {
		return true, nil
	}
	return engines.DocumentMatchesScope(constraints, data) == "", nil
}

// handlePurchaseReturnContext: GET /api/v1/procurement/purchase-returns/context
// ?grn_id=&return_id= - whether posting needs approval, and (with grn_id) what
// that GRN can still return, excluding return_id's own lines when editing.
func handlePurchaseReturnContext(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "PurchaseReturn", "read"); err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	gated, err := engines.IsApprovalGated(tenantID, "PurchaseReturn")
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	out := map[string]interface{}{"approval_required": gated}
	if grnID := r.URL.Query().Get("grn_id"); grnID != "" {
		returnable, err := engines.GetPurchaseReturnable(tenantID, grnID, r.URL.Query().Get("return_id"))
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		ok, err := purchaseReturnInScope(r, tenantID, "GRN", map[string]interface{}{"location": returnable.Location})
		if err != nil {
			writeEngineError(w, r, err, http.StatusInternalServerError)
			return
		}
		if !ok {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return
		}
		out["returnable"] = returnable
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handlePostPurchaseReturn: POST /api/v1/procurement/purchase-returns/{id}/post.
func handlePostPurchaseReturn(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "PurchaseReturn", "update"); err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	returnID := r.PathValue("id")
	data, err := engines.GetPurchaseReturn(tenantID, returnID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "purchase return "+returnID+" does not exist")
		return
	}
	ok, err := purchaseReturnInScope(r, tenantID, "PurchaseReturn", data)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	if !ok {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	result, err := engines.PostPurchaseReturn(tenantID, returnID, userID, r.Header.Get("Resolved-Location"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
