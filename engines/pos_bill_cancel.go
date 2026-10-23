package engines

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 130.9 (72.D c, user decision 2026-10-22): cancel a posted bill. A Store
// Manager or an administrator may cancel a bill on the day it was made. Every
// line comes back as sellable through the ordinary return - raised, received
// and inspected at the counter in one go, as a till exchange is (73.2.6) - so
// stock, revenue, tax, points and gift-card use are reversed by the code that
// already reverses them, and the money goes back to the tenders it was paid
// with. A bill from an earlier day, or one already partly returned, is
// returned instead. Each step is the return's own: one that fails leaves the
// return where it stopped, on the Returns screen.

// RefundMethodOriginalPayment is the refund method a cancelled bill uses: back
// to each tender the bill was paid with (73.1.6).
const RefundMethodOriginalPayment = "Original Payment"

// BillCancelResult says what cancelling a bill did.
type BillCancelResult struct {
	BillNo          string  `json:"bill_no"`
	ReturnRequestID string  `json:"return_request_id"`
	RefundRequestID string  `json:"refund_request_id"`
	Refund          float64 `json:"refund"`
	Replayed        bool    `json:"replayed,omitempty"`
}

// CancelPOSBill cancels today's bill billNo at location.
func CancelPOSBill(tenantID, role, user, billNo, location, reason string) (*BillCancelResult, error) {
	billNo, location, reason = strings.TrimSpace(billNo), strings.TrimSpace(location), strings.TrimSpace(reason)
	if role != RoleStoreManager && !HasFullAccess(role) {
		return nil, &ValidationError{Code: "GLOBAL-0011", Message: "only a Store Manager or an administrator can cancel a bill"}
	}
	if billNo == "" || location == "" || reason == "" {
		return nil, &ValidationError{Code: "POSOFF-1301", SubFor: "reason", Message: "say which bill, at which store, and why it is being cancelled"}
	}
	elig, err := ResolveReturnEligibility(tenantID, billNo)
	if err != nil {
		return nil, err
	}
	if !elig.Found {
		return nil, &ValidationError{Code: "POSOFF-1301", SubFor: billNo, Message: fmt.Sprintf("no posted bill %s was found", billNo)}
	}
	if done, err := cancelledBill(tenantID, billNo); err != nil || done != nil {
		return done, err
	}
	if elig.SaleDate != time.Now().Format("2006-01-02") {
		return nil, &ValidationError{Code: "POSOFF-1301", SubFor: billNo, Message: fmt.Sprintf("bill %s is from %s - only today's bills can be cancelled; take a return for it instead", billNo, elig.SaleDate)}
	}
	var lines []ReturnItemInput
	for _, l := range elig.Lines {
		if l.AlreadyReturned > 0 {
			return nil, &ValidationError{Code: "POSOFF-1301", SubFor: billNo, Message: fmt.Sprintf("part of bill %s was already returned - return the rest instead of cancelling", billNo)}
		}
		if l.RemainingQty > 0 {
			lines = append(lines, ReturnItemInput{SKU: l.SKU, Qty: l.RemainingQty})
		}
	}
	if len(lines) == 0 {
		return nil, &ValidationError{Code: "POSOFF-1301", SubFor: billNo, Message: fmt.Sprintf("bill %s has nothing left to cancel", billNo)}
	}

	cmd, err := CreateReturnRequestCommand(tenantID, "Customer Return", location, billNo, "", user, "bill-cancel:"+billNo, "", lines, nil)
	if err != nil {
		return nil, err
	}
	returnID := cmd.ReturnRequestID
	// The manager's own decision at the counter, like an exchange: the
	// second-person rule (55.5a) does not apply to it.
	if err := approveReturnRequest(tenantID, returnID, user, false); err != nil {
		return nil, err
	}
	if err := ReceiveReturnRequest(tenantID, returnID, user); err != nil {
		return nil, err
	}
	dispositions := map[string]string{}
	for _, l := range lines {
		dispositions[l.SKU] = "Sellable"
	}
	refund, refundID, err := ApplyReturnQC(tenantID, returnID, dispositions, user)
	if err != nil {
		return nil, err
	}
	if refundID != "" {
		if err := approveRefundRequest(tenantID, refundID, user, false); err != nil {
			return nil, err
		}
		if err := ProcessRefundRequest(tenantID, refundID, user, RefundMethodOriginalPayment); err != nil {
			return nil, err
		}
	}
	out := &BillCancelResult{BillNo: billNo, ReturnRequestID: returnID, RefundRequestID: refundID, Refund: round2(refund)}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	stamp, _ := json.Marshal(map[string]interface{}{
		"bill_cancelled": true, "cancelled_at": time.Now().Format(time.RFC3339), "cancelled_by": user,
		"cancel_reason": reason, "cancel_return_id": returnID, "cancel_refund_id": refundID, "cancel_refund": out.Refund,
	})
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'POSCart' AND id = $1`, schema), billNo, string(stamp)); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, user, "POS_BILL_CANCELLED", "SUCCESS", fmt.Sprintf("Bill %s cancelled (%s): return %s, refund %.2f", billNo, reason, returnID, out.Refund))
	return out, nil
}

// cancelledBill is the result of a bill already cancelled, for a repeat.
func cancelledBill(tenantID, billNo string) (*BillCancelResult, error) {
	data, _, err := fetchDocData(tenantID, "POSCart", billNo)
	if err != nil || data == nil || !truthy(data["bill_cancelled"]) {
		return nil, nil
	}
	return &BillCancelResult{BillNo: billNo, ReturnRequestID: strField(data, "cancel_return_id"), RefundRequestID: strField(data, "cancel_refund_id"),
		Refund: numFromInterface(data["cancel_refund"]), Replayed: true}, nil
}
