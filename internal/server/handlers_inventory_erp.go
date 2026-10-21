package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 88.5: the Inventory module's own routes - its dashboard and the MRP
// Change document, behind the inventory module gate (as Consumption is); each
// write is checked against its doctype's create right.

// handleInventoryDashboard is Inventory ▸ Dashboard. Stock value is cost, so a
// role that may not see cost gets the units without it.
func handleInventoryDashboard(w http.ResponseWriter, r *http.Request) {
	out, err := engines.InventoryDashboard(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("location"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !engines.RoleMaySeeCost(r.Header.Get("Resolved-Role")) {
		delete(out, "total_value")
		if rows, ok := out["by_location"].([]map[string]interface{}); ok {
			for _, row := range rows {
				delete(row, "value")
			}
		}
		out["value_hidden"] = true
	}
	_ = json.NewEncoder(w).Encode(out)
}

// requireCreate answers GLOBAL-0011 unless the role may create the doctype.
func requireCreate(w http.ResponseWriter, r *http.Request, doctype string) bool {
	allowed, err := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), doctype, "create")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check permissions")
		return false
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return false
	}
	return true
}

// handleCreateMRPChange is 88.5.5: a new MRP from an effective date.
func handleCreateMRPChange(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.MRPChangeDoctype) {
		return
	}
	var req engines.MRPChangeInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	id, status, err := engines.CreateMRPChange(r.Header.Get("Resolved-Tenant-ID"), req, r.Header.Get("Resolved-User-ID"))
	if err != nil && id == "" {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	out := map[string]interface{}{"id": id, "number": engines.DocumentNumber(r.Header.Get("Resolved-Tenant-ID"), engines.MRPChangeDoctype, id), "status": status}
	if err != nil {
		out["warning"] = err.Error()
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handleCancelMRPChange withdraws a change that has not started yet.
func handleCancelMRPChange(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.MRPChangeDoctype) {
		return
	}
	if err := engines.CancelMRPChange(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Cancelled"})
}

// handleCreateInventoryAdjustment is 88.5.4 (D13): it posts at once, or waits
// as Pending Approval when an approval rule covers adjustments.
func handleCreateInventoryAdjustment(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.InventoryAdjustmentDoctype) {
		return
	}
	var req engines.InventoryAdjustmentInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	id, status, err := engines.CreateInventoryAdjustment(tenantID, req, r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"))
	if err != nil && id == "" {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	out := map[string]interface{}{"id": id, "number": engines.DocumentNumber(tenantID, engines.InventoryAdjustmentDoctype, id), "status": status}
	if err != nil {
		out["warning"] = err.Error()
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handlePostInventoryAdjustment posts an Approved adjustment again after its
// first post was refused (the stock had gone since it was raised).
func handlePostInventoryAdjustment(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.InventoryAdjustmentDoctype) {
		return
	}
	if err := engines.PostInventoryAdjustment(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Posted"})
}
