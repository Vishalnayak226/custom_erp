package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 26.7: Voucher redemption + loyalty tier rules admin. A new file
// (not handlers_pim_pos_finance.go, which had the pre-existing loyalty
// endpoint) purely to avoid colliding with a concurrent session's in-flight
// edits there this same day.

func handleValidateVoucher(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Code       string `json:"code"`
		CustomerID string `json:"customer_id"`
		CartAmount int    `json:"cart_amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'code' is required")
		return
	}
	v, discount, err := engines.ValidateVoucher(tenantID, req.Code, req.CustomerID, req.CartAmount)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"voucher": v, "discount_amount": discount})
}

func handleRedeemVoucher(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Code        string `json:"code"`
		CustomerID  string `json:"customer_id"`
		ReferenceID string `json:"reference_id"`
		CartAmount  int    `json:"cart_amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'code' is required")
		return
	}
	discount, err := engines.RedeemVoucher(tenantID, req.Code, req.CustomerID, req.ReferenceID, req.CartAmount)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"discount_amount": discount})
}

func handleInitiateSecureLoyaltyRedemption(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		CustomerID  string `json:"customer_id"`
		Points      int    `json:"points"`
		ReferenceID string `json:"reference_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == "" || req.Points <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'customer_id' and a positive 'points' are required")
		return
	}
	challengeID, err := engines.InitiateSecureLoyaltyRedemption(tenantID, req.CustomerID, req.Points, req.ReferenceID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"challenge_id": challengeID})
}

func handleVerifySecureLoyaltyRedemption(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		ChallengeID string `json:"challenge_id"`
		OTPCode     string `json:"otp_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChallengeID == "" || req.OTPCode == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'challenge_id' and 'otp_code' are required")
		return
	}
	result, discount, requestID, err := engines.VerifyAndRedeemLoyaltyOTP(tenantID, req.ChallengeID, req.OTPCode)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": result, "discount_amount": discount, "request_id": requestID})
}

func handleLoyaltyTierRules(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	switch r.Method {
	case http.MethodGet:
		rules, err := engines.GetLoyaltyTierRules(tenantID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(rules)
	case http.MethodPost:
		var req struct {
			Tier           string  `json:"tier"`
			MinSpend       float64 `json:"min_spend"`
			EarnMultiplier float64 `json:"earn_multiplier"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Tier == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'tier' is required")
			return
		}
		if err := engines.UpsertLoyaltyTierRule(tenantID, req.Tier, req.MinSpend, req.EarnMultiplier); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"tier": req.Tier, "status": "saved"})
	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
