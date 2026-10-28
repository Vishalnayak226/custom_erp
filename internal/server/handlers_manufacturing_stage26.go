package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
)

// Stage 26.9 (Manufacturing/MRP Maturity Sprint) handlers. Kept in their own
// file, separate from handlers_operations.go's existing Issue Material/
// Complete handlers, to avoid colliding with any concurrent session's
// in-flight edits to that shared file - same precedent Stage 26.5 set with
// handlers_wms_enterprise.go.

func handlePartialCompleteProductionOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		OrderID string  `json:"order_id"`
		Qty     float64 `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" || req.Qty <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'order_id' and a positive 'qty' are required")
		return
	}
	if err := engines.ReportPartialCompletion(tenantID, req.OrderID, req.Qty); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "partial_completion_posted"})
}

func handlePostProductionScrap(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		OrderID string  `json:"order_id"`
		Sku     string  `json:"sku"`
		Qty     float64 `json:"qty"`
		Reason  string  `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" || req.Sku == "" || req.Qty <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'order_id', 'sku', and a positive 'qty' are required")
		return
	}
	if err := engines.PostScrap(tenantID, req.OrderID, req.Sku, req.Qty, req.Reason, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "scrap_posted"})
}

func handleSendProductionToRework(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		OrderID string  `json:"order_id"`
		Qty     float64 `json:"qty"`
		Reason  string  `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" || req.Qty <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'order_id' and a positive 'qty' are required")
		return
	}
	if err := engines.SendToRework(tenantID, req.OrderID, req.Qty, req.Reason, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "sent_to_rework"})
}

func handleConfirmProductionOperation(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		OrderID string `json:"order_id"`
		Seq     int    `json:"seq"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'order_id' is required")
		return
	}
	warning, err := engines.ConfirmOperation(tenantID, req.OrderID, req.Seq, userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "operation_confirmed", "capacity_warning": warning})
}

// handleAcknowledgeBOMVariance accepts a production order's BOM change
// (MFG-0276) so it can be finished. Stage 49.5.6c: the one way to set it - it
// needs Production Order update permission and a reason, and is audited.
func handleAcknowledgeBOMVariance(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	allowed, err := checkPermission(tenantID, r.Header.Get("Resolved-Role"), "ProductionOrder", "update")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to check permissions")
		return
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	var req struct {
		OrderID string `json:"order_id"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'order_id' is required")
		return
	}
	ack, err := engines.AcknowledgeBOMVariance(tenantID, req.OrderID, userID, req.Reason)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "bom_variance_acknowledged", "acknowledgement": ack})
}

func handleRecordActualProductionCost(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		OrderID    string  `json:"order_id"`
		ActualCost float64 `json:"actual_cost"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'order_id' is required")
		return
	}
	if err := engines.RecordActualProductionCost(tenantID, req.OrderID, req.ActualCost, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "actual_cost_recorded"})
}

// handleMRPSuggestions (26.9.5) is a read-only GET, matching the existing
// replenishment-suggestions endpoint's own query-param shape.
func handleMRPSuggestions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	location := r.URL.Query().Get("location")
	if location == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query param 'location' is required")
		return
	}
	leadTimeDays, _ := strconv.Atoi(r.URL.Query().Get("lead_time_days"))
	if leadTimeDays <= 0 {
		leadTimeDays = engines.GetSettingInt(tenantID, "manufacturing.default_lead_time_days")
	}
	safetyStock, _ := strconv.Atoi(r.URL.Query().Get("safety_stock"))

	suggestions, err := engines.GetMRPSuggestions(tenantID, location, leadTimeDays, safetyStock)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(suggestions)
}

// handleGetProductionSchedule (26.9.10) is a read-only GET, same shape as
// handleMRPSuggestions above - a scheduling suggestion, not a document.
func handleGetProductionSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	schedule, err := engines.GetProductionSchedule(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(schedule)
}

// handleSendSubcontractOrder/handleReceiveSubcontractOrder (26.9.11) are the
// two state-changing actions a plain generic-doc SubcontractOrder can't
// express on its own - same "flat doctype + bespoke action handlers" shape
// as PurchaseRequisition's submit/convert actions.
func handleSendSubcontractOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	// Stage 130.16 (74.3.3): sent through a delivery challan, with its transport.
	var req struct {
		ID string `json:"id"`
		engines.ChallanDispatch
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'id' is required")
		return
	}
	if !requireDocPermission(w, r, "SubcontractOrder", "update", "You do not have permission to send job work") {
		return
	}
	orderStatus, err := engines.SendJobWork(tenantID, req.ID, userID, r.Header.Get("Resolved-Role"), req.ChallanDispatch)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "sent_to_subcontractor", "order_status": orderStatus})
}

// handleReceiveJobWorkLines (Stage 130.16, 74.3.3) takes a partial return
// against the order's lines.
func handleReceiveJobWorkLines(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    string                       `json:"id"`
		Key   string                       `json:"key"`
		Lines []engines.JobWorkReceiveLine `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'id' is required")
		return
	}
	if !requireDocPermission(w, r, "SubcontractOrder", "update", "You do not have permission to receive job work") {
		return
	}
	status, err := engines.ReceiveJobWork(r.Header.Get("Resolved-Tenant-ID"), req.ID, r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"), req.Key, req.Lines)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}

// handleJobWorkCharge (Stage 130.16, 74.3.3) books the job worker's bill.
func handleJobWorkCharge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string  `json:"id"`
		BillNo string  `json:"bill_no"`
		Amount float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'id' is required")
		return
	}
	if !requireDocPermission(w, r, "SubcontractOrder", "update", "You do not have permission to book job work charges") {
		return
	}
	if err := engines.PostJobWorkCharge(r.Header.Get("Resolved-Tenant-ID"), req.ID, r.Header.Get("Resolved-User-ID"), req.BillNo, req.Amount); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "booked"})
}

func handleReceiveSubcontractOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		ID  string  `json:"id"`
		Qty float64 `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Qty <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'id' and a positive 'qty' are required")
		return
	}
	if err := engines.ReceiveFromSubcontractor(tenantID, req.ID, userID, req.Qty); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "received_from_subcontractor"})
}

// handleActiveBOMForItem (26.9.2) is a frontend-driven advisory lookup - it
// suggests which BOM a new Production Order for an item should default to
// (is_default + effective-dating), without a server-side hook into the
// generic doc-create path.
func handleActiveBOMForItem(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	item := r.URL.Query().Get("item")
	if item == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query param 'item' is required")
		return
	}
	bomID, err := engines.GetActiveBOMForItem(tenantID, item, r.URL.Query().Get("date"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"bom_id": bomID})
}
