package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// handleCreateSalesOrder is Stage 26.12.1's Order Engine entry point - the
// only way a SalesOrder+SalesOrderLine set gets created (creating an order
// atomically validates, sources a node, and reserves stock per line, which
// the single-document generic POST /api/v1/doc/{doctype} endpoint can't do,
// hence a dedicated route rather than relying on the generic doc engine).
func handleCreateSalesOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Channel         string `json:"channel"`
		ChannelOrderID  string `json:"channel_order_id"`
		CustomerName    string `json:"customer_name"`
		CustomerPhone   string `json:"customer_phone"`
		CustomerRef     string `json:"customer_reference"` // Stage 73.2.9
		ShippingAddress string `json:"shipping_address"`
		// Stage 76.3: the manual order form sends the address in its parts
		// (shipping_address is Address Line 1).
		ShippingLine2   string `json:"shipping_address_line2"`
		ShippingCity    string `json:"shipping_city"`
		ShippingState   string `json:"shipping_state"`
		ShippingPincode string `json:"shipping_pincode"`
		PaymentStatus   string `json:"payment_status"`
		OwnerID         string `json:"owner_id"` // Stage 59.2: the 3PL client (optional)
		// Stage 88.3.1 (D12): the OMS manual-order form sets it - an order keyed in
		// by staff skips Pending. API callers leave it off and wait at Pending.
		Manual bool `json:"manual"`
		Lines  []struct {
			SKU       string  `json:"sku"`
			Qty       int     `json:"qty"`
			UnitPrice float64 `json:"unit_price"`
		} `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if req.ShippingAddress == "" || len(req.Lines) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'shipping_address' and at least one 'lines' entry are required")
		return
	}

	// FA-20261005-06: say when a repeated channel reference returns the order it
	// already made, so the screen does not announce a second "created".
	existing := ""
	if req.Channel != "" && req.ChannelOrderID != "" {
		existing, _ = engines.FindSalesOrderByChannelRef(tenantID, req.Channel, req.ChannelOrderID)
	}

	lines := make([]engines.SalesOrderLineInput, len(req.Lines))
	for i, l := range req.Lines {
		if l.SKU == "" || l.Qty <= 0 {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Each line requires a non-empty 'sku' and a positive 'qty'")
			return
		}
		lines[i] = engines.SalesOrderLineInput{SKU: l.SKU, Qty: l.Qty, UnitPrice: l.UnitPrice}
	}

	orderID, err := engines.CreateSalesOrder(tenantID, engines.SalesOrderInput{
		Channel:         req.Channel,
		ChannelOrderID:  req.ChannelOrderID,
		CustomerName:    req.CustomerName,
		ShippingAddress: req.ShippingAddress,
		ShippingLine2:   req.ShippingLine2,
		ShippingCity:    req.ShippingCity,
		ShippingState:   req.ShippingState,
		ShippingPincode: req.ShippingPincode,
		PaymentStatus:   req.PaymentStatus,
		CustomerPhone:   req.CustomerPhone,
		CustomerRef:     req.CustomerRef,
		OwnerID:         req.OwnerID, // Stage 59.2
		SkipPending:     req.Manual,
		AcceptedBy:      r.Header.Get("Resolved-User-ID"),
		Lines:           lines,
	})
	if err != nil {
		// writeEngineError keeps a coded refusal's own code (Stage 57.8's
		// ASSET-0273) and is the generic 422 for everything else, as before.
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"order_id": orderID, "order_number": engines.DocumentNumber(tenantID, "SalesOrder", orderID), // Stage 76.11
		"existing": existing != "" && existing == orderID})
}

// handlePlaceOrderHold is the Hold engine's manual entry point (a CS agent
// placing an order on hold) - see engines.PlaceOrderHold's own doc comment.
func handlePlaceOrderHold(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	orderID := r.PathValue("id")

	var req struct {
		ReasonCode string `json:"reason_code"`
		Owner      string `json:"owner"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}

	if err := engines.PlaceOrderHold(tenantID, orderID, req.ReasonCode, req.Owner); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "on_hold"})
}

// handleReleaseOrderHold re-runs the order's validate chain and either
// reserves stock and clears the hold, or updates the hold reason and stays
// On Hold - see engines.ReleaseOrderHold's own doc comment.
func handleReleaseOrderHold(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	orderID := r.PathValue("id")

	if err := engines.ReleaseOrderHold(tenantID, orderID); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// ReleaseOrderHold doesn't error just because the order stayed On Hold
	// (a still-failing validate chain is an expected outcome, not a
	// failure) - report the actual resulting state instead of a blanket
	// "released" that would be misleading if the order didn't clear.
	orderStatus, holdReason, err := engines.GetOrderStatus(tenantID, orderID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"order_status": orderStatus,
		"hold_reason":  holdReason,
	})
}

// handleReleaseOrderToFulfillment is the Reserved order's next step
// (FA-20261005-03): it creates the pick tasks. Idempotent - a repeat returns
// the existing tasks with created=false, so the UI can say so honestly.
func handleReleaseOrderToFulfillment(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	orderID := r.PathValue("id")
	taskIDs, created, err := engines.ReleaseOrderToFulfillment(tenantID, orderID, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"order_id": orderID, "order_status": engines.OrderStatusReleased, "task_ids": taskIDs, "created": created,
	})
}

// handleCancelOrder enforces the stage-gated cancellation matrix - see
// engines.CancelOrder's own doc comment.
func handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	orderID := r.PathValue("id")

	var req struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}

	if err := engines.CancelOrder(tenantID, orderID, req.ReasonCode); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}
