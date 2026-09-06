package server

import (
	"context"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
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
		// IdempotencyKey (Stage 47.4.3) makes a repeated click, a lost
		// response or two open tabs produce ONE return rather than several.
		// It defaults to the original order id below, which is the safest
		// available identity for a client that sends none: without it, a
		// caller that retries would create a second request against the same
		// bill, and A-04 is precisely that failure.
		IdempotencyKey string `json:"idempotency_key"`
		// ExceptionType/ExceptionReason (Stage 47.4.5) request one of the two
		// named exception paths - "No Receipt" or "Goodwill". Both are gated on
		// the returns.exception capability at the route, so a cashier cannot
		// reach either; the reason is mandatory, and the authoriser is the
		// caller's own resolved identity, never a name from the request body.
		ExceptionType   string `json:"exception_type"`
		ExceptionReason string `json:"exception_reason"`
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

	// The requester is the CALLER, never the request body - the same rule
	// every other identity in this codebase follows. Before Stage 47.4 a
	// client could name anyone as requested_by, which put a stranger's name on
	// the evidence for a return they never raised.
	requestedBy := r.Header.Get("Resolved-User-ID")
	if requestedBy == "" {
		requestedBy = req.RequestedBy
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = req.RequestType + ":" + req.OriginalOrderID + ":" + req.BookingID
	}

	var exception *engines.ReturnException
	if strings.TrimSpace(req.ExceptionType) != "" {
		// A No Receipt or Goodwill return is a supervisor decision, and the
		// capability is what makes that structural rather than a matter of
		// which screen someone opened. A Cashier is refused here even though
		// the route itself is open to them for ordinary returns.
		if !engines.RoleHasCapability(r.Header.Get("Resolved-Role"), "returns.exception") {
			writeAPIErrorDetail(w, r, "GLOBAL-0011", "",
				"A No Receipt or Goodwill return needs a supervisor. Ask one to authorise it.")
			return
		}
		// The route's own capability check (returns.exception) has already run
		// by the time this handler is entered - see route_capabilities.go. The
		// authoriser is taken from the session so the evidence names whoever
		// actually held the capability, not whoever the client typed.
		exception = &engines.ReturnException{
			Type:         strings.TrimSpace(req.ExceptionType),
			Reason:       req.ExceptionReason,
			AuthorisedBy: requestedBy,
		}
	}

	result, err := engines.CreateReturnRequestCommand(tenantID, req.RequestType, req.ReturnLocation,
		req.OriginalOrderID, req.BookingID, requestedBy, idempotencyKey,
		r.Header.Get("Resolved-Correlation-ID"), items, exception)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"return_request_id": result.ReturnRequestID,
		"status":            result.Status,
		"replayed":          result.Replayed,
		"exception_type":    result.ExceptionType,
	})
}

// handleReturnEligibility (Stage 47.4.6) answers "what can still be returned
// against this bill, and why" - the question the Returns screen has to be able
// to answer before a clerk touches anything, and the one the old POS return
// form could not answer at all (it asked the clerk to type the SKU, the
// quantity AND the price, with no reference to what was actually sold).
func handleReturnEligibility(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	orderID := strings.TrimSpace(r.URL.Query().Get("original_order_id"))
	if orderID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameter 'original_order_id' is required")
		return
	}
	eligibility, err := engines.ResolveReturnEligibility(tenantID, orderID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(eligibility)
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
