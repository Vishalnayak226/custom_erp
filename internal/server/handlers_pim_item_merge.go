package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// handlePIMItemMerge previews (dry_run) or performs a product merge. Only an
// admin or a role allowed to delete Items may merge; extension tokens never.
func handlePIMItemMerge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keep      string `json:"keep"`
		Duplicate string `json:"duplicate"`
		DryRun    bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload JSON")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if !requireAdminGate(w, r, engines.MayMergeItems(tenantID, role)) {
		return
	}
	fail := func(err error) {
		if verr, ok := err.(*engines.ValidationError); ok && verr.Code != "" {
			writeAPIErrorDetail(w, r, verr.Code, verr.SubFor, verr.Message)
			return
		}
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
	}
	if req.DryRun {
		p, err := engines.PreviewItemMerge(tenantID, req.Keep, req.Duplicate)
		if err != nil {
			fail(err)
			return
		}
		_ = json.NewEncoder(w).Encode(p)
		return
	}
	res, err := engines.MergeItems(tenantID, req.Keep, req.Duplicate, r.Header.Get("Resolved-User-ID"), role)
	if err != nil {
		fail(err)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
