package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 37.11 - dashboard layout HTTP surface. Mirrors
// handlers_oms_console.go's handleOMSSavedViews/handleOMSDeleteSavedView
// shape exactly: DashboardLayout documents are engine-written (not through
// the generic doc-create API), so list/save/delete each need their own
// route. DashboardDigest needs none of this - it is a plain registered
// doctype the generic doc API already handles.

// handleDashboardLayouts lists (GET) or saves (POST) the caller's own
// dashboard layouts, plus any role-shared default for their role.
func handleDashboardLayouts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	switch r.Method {
	case http.MethodGet:
		layouts, err := engines.ListDashboardLayouts(tenantID, userID, role)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"layouts": layouts})
	case http.MethodPost:
		var req struct {
			Name  string                     `json:"name"`
			Role  string                     `json:"role"`
			Tiles []engines.DashboardTileSpec `json:"tiles"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
			return
		}
		layoutID, err := engines.SaveDashboardLayout(tenantID, userID, req.Name, req.Role, req.Tiles)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": layoutID, "status": "saved"})
	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleDeleteDashboardLayout removes one of the caller's own layouts.
func handleDeleteDashboardLayout(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodDelete {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := engines.DeleteDashboardLayout(tenantID, r.Header.Get("Resolved-User-ID"), r.PathValue("id")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}
