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
		ShippingAddress string `json:"shipping_address"`
		PaymentStatus   string `json:"payment_status"`
		Lines           []struct {
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
		PaymentStatus:   req.PaymentStatus,
		CustomerPhone:   req.CustomerPhone,
		Lines:           lines,
	})
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"order_id": orderID})
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
