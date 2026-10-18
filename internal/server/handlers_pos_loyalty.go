package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 73.1.4 - what a till redemption needs before the sale: the customer's
// one-time code and, above the approval value, a manager's approval. Nothing
// here burns a point - the sale does, through engines.RedeemLoyaltyPointsTx,
// which checks the same controls again.

// handleStartPOSRedemption - POST /api/v1/pos/loyalty/redemption/start
// {customer_id, points, cart_number, location, reason_code}. Answers what the
// redemption needs, after sending the code and raising the approval request
// it calls for.
func handleStartPOSRedemption(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CustomerID string `json:"customer_id"`
		Points     int    `json:"points"`
		CartNumber string `json:"cart_number"`
		Location   string `json:"location"`
		ReasonCode string `json:"reason_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request")
		return
	}
	out, err := engines.StartTillLoyaltyRedemption(r.Header.Get("Resolved-Tenant-ID"), strings.TrimSpace(req.CustomerID), req.Points,
		strings.TrimSpace(req.CartNumber), strings.TrimSpace(req.Location), strings.TrimSpace(req.ReasonCode),
		r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handleVerifyPOSRedemption - POST /api/v1/pos/loyalty/redemption/verify
// {challenge_id, otp}: the code the customer read back.
func handleVerifyPOSRedemption(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChallengeID string `json:"challenge_id"`
		OTP         string `json:"otp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request")
		return
	}
	if err := engines.VerifyTillLoyaltyOTP(r.Header.Get("Resolved-Tenant-ID"), req.ChallengeID, strings.TrimSpace(req.OTP)); err != nil {
		writeAPIErrorDetail(w, r, "CUSTOM-0602", "", err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"verified": true, "challenge_id": req.ChallengeID})
}

// handlePOSRedemptionStatus - GET /api/v1/pos/loyalty/redemption/status?approval_id=
// where the till's approval request stands.
func handlePOSRedemptionStatus(w http.ResponseWriter, r *http.Request) {
	approvalID := r.URL.Query().Get("approval_id")
	status, err := engines.TillRedemptionApprovalStatus(r.Header.Get("Resolved-Tenant-ID"), approvalID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"approval_id": approvalID, "status": status})
}

// handlePOSCustomerContext - GET /api/v1/pos/customer-context?customer_id=
// (Stage 73.3.2): the customer on the bill - points and their value, tier,
// benefits and the way to the next tier, recent bills and returns, and the
// vouchers they can use now.
func handlePOSCustomerContext(w http.ResponseWriter, r *http.Request) {
	ctx, err := engines.TillCustomerContextFor(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("customer_id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(ctx)
}
