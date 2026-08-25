package server

import (
	"context"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"time"
)

// handleCreateReturnRequest is Stage 26.12.5's entry point for both a
// customer-initiated return and a courier RTO (request_type distinguishes
// them) - see engines.CreateReturnRequest's own doc comment.
func handleCreateReturnRequest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		RequestType     string `json:"request_type"`
		ReturnLocation  string `json:"return_location"`
		OriginalOrderID string `json:"original_order_id"`
		BookingID       string `json:"booking_id"`
		RequestedBy     string `json:"requested_by"`
		Items           []struct {
			SKU string `json:"sku"`
			Qty int    `json:"qty"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if req.ReturnLocation == "" || len(req.Items) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'return_location' and at least one 'items' entry are required")
		return
	}

	items := make([]engines.ReturnItemInput, len(req.Items))
	for i, it := range req.Items {
		if it.SKU == "" || it.Qty <= 0 {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Each item requires a non-empty 'sku' and a positive 'qty'")
			return
		}
		items[i] = engines.ReturnItemInput{SKU: it.SKU, Qty: it.Qty}
	}

	returnID, err := engines.CreateReturnRequest(tenantID, req.RequestType, req.ReturnLocation, req.OriginalOrderID, req.BookingID, req.RequestedBy, items)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"return_request_id": returnID})
}

func handleApproveReturnRequest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	returnID := r.PathValue("id")

	var req struct {
		ApprovedBy string `json:"approved_by"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := engines.ApproveReturnRequest(tenantID, returnID, req.ApprovedBy); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "approved"})
}

func handleRejectReturnRequest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	returnID := r.PathValue("id")

	var req struct {
		ReasonCode string `json:"reason_code"`
		RejectedBy string `json:"rejected_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}

	if err := engines.RejectReturnRequest(tenantID, returnID, req.ReasonCode, req.RejectedBy); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "rejected"})
}

func handleReceiveReturnRequest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	returnID := r.PathValue("id")

	var req struct {
		ReceivedBy string `json:"received_by"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := engines.ReceiveReturnRequest(tenantID, returnID, req.ReceivedBy); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "received"})
}

// handleApplyReturnQC is the QC disposition step - see
// engines.ApplyReturnQC's own doc comment for the disposition-to-bucket/
// refund-eligibility mapping.
func handleApplyReturnQC(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	returnID := r.PathValue("id")

	var req struct {
		Dispositions map[string]string `json:"dispositions"`
		QCBy         string            `json:"qc_by"`
		// ExchangeFor (Stage 35.9.2) is an optional originalSKU ->
		// exchangeSKU map - see engines.ApplyReturnQC's own doc comment.
		ExchangeFor map[string]string `json:"exchange_for"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if len(req.Dispositions) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'dispositions' (sku -> disposition) is required")
		return
	}

	totalRefund, refundRequestID, err := engines.ApplyReturnQC(tenantID, returnID, req.Dispositions, req.QCBy, req.ExchangeFor)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"total_refund_eligible": totalRefund,
		"refund_request_id":     refundRequestID,
	})
}

// handleScheduleReturnReversePickup is Stage 35.9.1's entry point - see
// engines.ScheduleReturnReversePickup's own doc comment for why this only
// applies to a Customer Return and how it reuses the Stage 35.5 courier
// machinery unmodified.
func handleScheduleReturnReversePickup(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	returnID := r.PathValue("id")

	var req struct {
		Provider      string `json:"provider"`
		PickupPincode string `json:"pickup_pincode"`
		PickupAddress string `json:"pickup_address"`
		PickupName    string `json:"pickup_name"`
		PickupAt      string `json:"pickup_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if req.Provider == "" || req.PickupPincode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'provider' and 'pickup_pincode' are required")
		return
	}
	pickupAt := time.Now().Add(24 * time.Hour)
	if req.PickupAt != "" {
		if parsed, errT := time.Parse(time.RFC3339, req.PickupAt); errT == nil {
			pickupAt = parsed
		}
	}

	bookingID, awb, err := engines.ScheduleReturnReversePickup(context.Background(), tenantID, returnID, req.Provider, req.PickupPincode, req.PickupAddress, req.PickupName, pickupAt)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"booking_id": bookingID, "awb": awb})
}

func handleApproveRefundRequest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	refundID := r.PathValue("id")

	var req struct {
		ApprovedBy string `json:"approved_by"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := engines.ApproveRefundRequest(tenantID, refundID, req.ApprovedBy); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "approved"})
}

func handleRejectRefundRequest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	refundID := r.PathValue("id")

	var req struct {
		ReasonCode string `json:"reason_code"`
		RejectedBy string `json:"rejected_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}

	if err := engines.RejectRefundRequest(tenantID, refundID, req.ReasonCode, req.RejectedBy); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "rejected"})
}

// handleProcessRefundRequest posts the revenue-side GL reversal - see
// engines.ProcessRefundRequest's own doc comment for why that's kept
// separate from the inventory-side post ApplyReturnQC already made.
func handleProcessRefundRequest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	refundID := r.PathValue("id")

	var req struct {
		ProcessedBy  string `json:"processed_by"`
		RefundMethod string `json:"refund_method"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := engines.ProcessRefundRequest(tenantID, refundID, req.ProcessedBy, req.RefundMethod); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "processed"})
}
