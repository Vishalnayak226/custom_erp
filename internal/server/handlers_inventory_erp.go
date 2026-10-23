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
	tenantID, userID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role")
	// Stage 130.14: "post with the current stock" - a store manager takes the
	// before quantities again when the stock moved while it waited.
	var req struct {
		AcceptCurrentStock bool `json:"accept_current_stock"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.AcceptCurrentStock {
		if !engines.StoreAppManagerRole(role) {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return
		}
		if err := engines.RefreshAdjustmentSnapshot(tenantID, r.PathValue("id"), userID); err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
	}
	if err := engines.PostInventoryAdjustment(tenantID, r.PathValue("id"), userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Posted"})
}

// handlePreviewInventoryAdjustment (Stage 130.14) - what an adjustment would
// do, before it is saved: before / after quantities, values and GL legs.
func handlePreviewInventoryAdjustment(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.InventoryAdjustmentDoctype) {
		return
	}
	var req engines.InventoryAdjustmentInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	out, err := engines.PreviewInventoryAdjustment(r.Header.Get("Resolved-Tenant-ID"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handleReverseInventoryAdjustment (Stage 130.14) raises the linked reversal.
func handleReverseInventoryAdjustment(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.InventoryAdjustmentDoctype) {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	id, status, err := engines.ReverseInventoryAdjustment(tenantID, r.PathValue("id"), req.Reason, r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"))
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

// Stage 130.15 (74.2.4): the internal Material Issue - a department's
// request, the store's issue, and the return of what was not used.
func handleCreateMaterialRequest(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.MaterialIssueDoctype) {
		return
	}
	var req engines.MaterialRequestInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	id, status, err := engines.CreateMaterialRequest(tenantID, req, r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"))
	if err != nil && id == "" {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	out := map[string]interface{}{"id": id, "number": engines.DocumentNumber(tenantID, engines.MaterialIssueDoctype, id), "status": status}
	if err != nil {
		out["warning"] = err.Error()
	}
	_ = json.NewEncoder(w).Encode(out)
}

func handleIssueMaterial(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.MaterialIssueDoctype) {
		return
	}
	if err := engines.IssueMaterial(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-User-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Issued"})
}

func handleReturnMaterial(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.MaterialReturnDoctype) {
		return
	}
	var req struct {
		Lines []engines.MaterialReturnLine `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	id, err := engines.ReturnMaterial(tenantID, r.PathValue("id"), req.Lines, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "number": engines.DocumentNumber(tenantID, engines.MaterialReturnDoctype, id)})
}

// Stage 130.16 (74.3.1): the Delivery Challan - create, dispatch, receive a
// return, cancel before dispatch, and ask to write off what will not return.
func handleCreateDeliveryChallan(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.DeliveryChallanDoctype) {
		return
	}
	var req engines.ChallanInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	id, status, err := engines.CreateDeliveryChallan(tenantID, req, r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"))
	if err != nil && id == "" {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	out := map[string]interface{}{"id": id, "number": engines.DocumentNumber(tenantID, engines.DeliveryChallanDoctype, id), "status": status}
	if err != nil {
		out["warning"] = err.Error()
	}
	_ = json.NewEncoder(w).Encode(out)
}

func handleDispatchDeliveryChallan(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, engines.DeliveryChallanDoctype, "update", "You do not have permission to dispatch delivery challans") {
		return
	}
	var req engines.ChallanDispatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if err := engines.DispatchDeliveryChallan(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-User-ID"), req); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Dispatched"})
}

func handleReturnDeliveryChallan(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, engines.DeliveryChallanDoctype, "update", "You do not have permission to receive challan returns") {
		return
	}
	var req struct {
		ReturnKey string                      `json:"return_key"`
		Lines     []engines.ChallanReturnLine `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	status, err := engines.ReceiveChallanReturn(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-User-ID"), req.ReturnKey, req.Lines)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func handleCancelDeliveryChallan(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, engines.DeliveryChallanDoctype, "update", "You do not have permission to cancel delivery challans") {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if err := engines.CancelDeliveryChallan(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-User-ID"), req.Reason); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Cancelled"})
}

func handleRequestChallanWriteOff(w http.ResponseWriter, r *http.Request) {
	if !requireCreate(w, r, engines.ChallanWriteOffDoctype) {
		return
	}
	var req engines.ChallanWriteOffInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	id, err := engines.RequestChallanWriteOff(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "Pending Approval"})
}
