package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 35.3 - the order-mutation HTTP surface: item-level hold/unhold, order
// edit, switch facility, set priority and split.
//
// Each is a POST to the order (or line) it acts on, matching the shape the
// three 26.12.1 routes already use (POST /api/v1/orders/{id}/hold and
// friends), so the console's action bar hits one consistent family of URLs.

// handleHoldOrderLine is 35.3.1: stop one line without stopping the order.
func handleHoldOrderLine(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if req.ReasonCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'reason_code' is required")
		return
	}
	if err := engines.HoldOrderLine(tenantID, r.PathValue("lineId"), req.ReasonCode, r.Header.Get("Resolved-User-ID")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "held"})
}

// handleReleaseOrderLineHold is 35.3.1's other half.
func handleReleaseOrderLineHold(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := engines.ReleaseOrderLineHold(tenantID, r.PathValue("lineId"), r.Header.Get("Resolved-User-ID")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "released"})
}

// handleEditOrder is 35.3.2. Pointer fields so an omitted key means "leave it
// alone" rather than "blank it" - a partial edit is the normal case here.
func handleEditOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		CustomerName    *string           `json:"customer_name"`
		CustomerPhone   *string           `json:"customer_phone"`
		ShippingAddress *string           `json:"shipping_address"`
		BillingAddress  *string           `json:"billing_address"`
		PaymentStatus   *string           `json:"payment_status"`
		CustomFields    map[string]string `json:"custom_fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	err := engines.EditSalesOrder(tenantID, r.PathValue("id"), engines.OrderEdit{
		CustomerName:    req.CustomerName,
		CustomerPhone:   req.CustomerPhone,
		ShippingAddress: req.ShippingAddress,
		BillingAddress:  req.BillingAddress,
		PaymentStatus:   req.PaymentStatus,
		CustomFields:    req.CustomFields,
	}, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleSwitchOrderFacility is 35.3.3. An empty location re-runs the
// allocation engine instead of forcing a specific node - that is the console's
// "Reallocate" button.
func handleSwitchOrderFacility(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		LocationCode string `json:"location_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	moved, err := engines.SwitchOrderFacility(tenantID, r.PathValue("id"), req.LocationCode, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "moved", "lines_moved": moved})
}

// handleSetOrderPriority is 35.3.4.
func handleSetOrderPriority(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Priority string `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if err := engines.SetOrderPriority(tenantID, r.PathValue("id"), req.Priority, r.Header.Get("Resolved-User-ID")); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated", "priority": req.Priority})
}

// handleSplitOrder is 35.3.5.
func handleSplitOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		LineIDs []string `json:"line_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	groupID, err := engines.SplitOrder(tenantID, r.PathValue("id"), req.LineIDs, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "split", "fulfillment_group": groupID})
}

// handlePickQueue exposes 35.3.4's priority-ordered picking worklist - the
// thing that makes the Expedite flag operational rather than decorative.
func handlePickQueue(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	tasks, err := engines.PickTasksInPriorityOrder(tenantID, r.URL.Query().Get("location_code"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"tasks": tasks})
}
