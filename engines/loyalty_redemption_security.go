package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// Stage 26.7.5: fraud/staff-restriction rules + OTP redemption on loyalty
// burn. An additional, opt-in front door (InitiateSecureLoyaltyRedemption/
// VerifyAndRedeemLoyaltyOTP) alongside the existing immediate
// RedeemLoyaltyPoints/handleRedeemLoyaltyPoints path - stores that want ID
// verification and amount-based approval on burns use this one; the
// existing simple endpoint is untouched and still behaves exactly as
// before for stores that don't need the extra control.

// maxLoyaltyRedemptionsPerCustomerPerDay is a lightweight velocity/fraud
// guard - not a rules engine or ML model, just a hard daily cap on
// customer-initiated redemption attempts (LoyaltyExpiry's own automatic
// Burn rows don't count, since those aren't customer-initiated).
const maxLoyaltyRedemptionsPerCustomerPerDay = 5

// loyaltyRedemptionOTPValidityMinutes is how long a generated OTP stays
// valid before VerifyAndRedeemLoyaltyOTP rejects it as expired.
const loyaltyRedemptionOTPValidityMinutes = 5

func checkLoyaltyRedemptionVelocity(tenantID, customerID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var count int
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.loyalty_point_ledger
		WHERE customer_id = $1 AND transaction_type = 'Burn' AND reference_doctype != 'LoyaltyExpiry'
		  AND created_at::date = CURRENT_DATE`, schema), customerID).Scan(&count); err != nil {
		return err
	}
	if count >= maxLoyaltyRedemptionsPerCustomerPerDay {
		return fmt.Errorf("customer %s has already redeemed points %d times today - the daily limit is %d", customerID, count, maxLoyaltyRedemptionsPerCustomerPerDay)
	}
	return nil
}

func generateNumericOTP(digits int) (string, error) {
	max := big.NewInt(1)
	for i := 0; i < digits; i++ {
		max.Mul(max, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(fmt.Sprintf("%%0%dd", digits), n.Int64()), nil
}

func hashOTP(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// InitiateSecureLoyaltyRedemption starts the OTP-gated redemption path:
// checks the velocity guard and balance up front (so a customer never
// receives an OTP for a redemption that couldn't succeed anyway), generates
// a 6-digit code, stores its hash (never the plaintext) with a short
// expiry, and dispatches it to the customer via the existing Stage
// 26.12.10 notification mechanism (DispatchNotification) - no new SMS/
// WhatsApp provider dependency.
func InitiateSecureLoyaltyRedemption(tenantID, customerID string, points int, referenceID, initiatedBy string) (challengeID string, err error) {
	if points <= 0 {
		return "", fmt.Errorf("points must be positive")
	}
	if err := checkLoyaltyRedemptionVelocity(tenantID, customerID); err != nil {
		return "", err
	}
	balance, err := GetLoyaltyBalance(tenantID, customerID)
	if err != nil {
		return "", err
	}
	if points > balance {
		return "", &ValidationError{Code: "CUSTOM-0134", Message: fmt.Sprintf("insufficient loyalty points: requested %d, balance %d", points, balance)}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	code, err := generateNumericOTP(6)
	if err != nil {
		return "", err
	}
	challengeID = fmt.Sprintf("LROTP-%d", time.Now().UnixNano())
	expiresAt := time.Now().Add(loyaltyRedemptionOTPValidityMinutes * time.Minute)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.loyalty_redemption_otp_challenges (id, customer_id, points, reference_id, initiated_by, otp_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, schema),
		challengeID, customerID, points, referenceID, initiatedBy, hashOTP(code), expiresAt); err != nil {
		return "", err
	}

	// "Loyalty Redemption OTP" (Title Case), matching every other
	// DispatchNotification event string in this codebase (e.g. "Order
	// Placed", "Return Approved") - also the exact NotificationTemplate.event
	// Select option this migration adds.
	DispatchNotification(tenantID, "Loyalty Redemption OTP", customerID, map[string]string{
		"otp_code": code, "points": fmt.Sprintf("%d", points),
	})
	return challengeID, nil
}

// VerifyAndRedeemLoyaltyOTP verifies the OTP against its stored hash, then
// either redeems immediately (below the staff-restriction threshold) or
// creates a Draft LoyaltyRedemptionRequest for maker-checker approval (at/
// above it) - RequiredApproverRoleForAmount decides which, the same
// amount-slab routing every other approval-gated doctype already uses, so
// the threshold itself is tenant-adjustable via the existing approval_rules
// admin screen with no code change.
func VerifyAndRedeemLoyaltyOTP(tenantID, challengeID, otpCode string) (result string, discountValue int, requestID string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, "", err
	}

	var customerID, referenceID, initiatedBy, otpHash string
	var points int
	var expiresAt time.Time
	var verified bool
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT customer_id, points, COALESCE(reference_id, ''), initiated_by, otp_hash, expires_at, verified
		FROM %s.loyalty_redemption_otp_challenges WHERE id = $1`, schema), challengeID).
		Scan(&customerID, &points, &referenceID, &initiatedBy, &otpHash, &expiresAt, &verified); err != nil {
		return "", 0, "", fmt.Errorf("redemption challenge not found: %v", err)
	}
	if verified {
		return "", 0, "", fmt.Errorf("this OTP challenge has already been used")
	}
	if time.Now().After(expiresAt) {
		return "", 0, "", fmt.Errorf("OTP has expired - initiate a new redemption")
	}
	if hashOTP(otpCode) != otpHash {
		return "", 0, "", fmt.Errorf("incorrect OTP")
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.loyalty_redemption_otp_challenges SET verified = TRUE WHERE id = $1`, schema), challengeID); err != nil {
		return "", 0, "", err
	}

	value := points * redemptionValuePerPoint
	role, err := RequiredApproverRoleForAmount(tenantID, "LoyaltyRedemptionRequest", float64(value))
	if err != nil {
		return "", 0, "", err
	}
	if role == "" {
		discountValue, err = RedeemLoyaltyPoints(tenantID, customerID, points, referenceID)
		if err != nil {
			return "", 0, "", err
		}
		return "Redeemed", discountValue, "", nil
	}

	requestID = fmt.Sprintf("LRR-%d", time.Now().UnixNano())
	data := map[string]interface{}{
		"id": requestID, "code": requestID, "customer_id": customerID, "points": points,
		"points_value": value, "reference_id": referenceID, "status": "Draft",
	}
	marshaled, err := json.Marshal(data)
	if err != nil {
		return "", 0, "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'LoyaltyRedemptionRequest', $2, 'Draft', $3)`, schema),
		requestID, marshaled, initiatedBy); err != nil {
		return "", 0, "", err
	}
	return "PendingApproval", 0, requestID, nil
}

// executeApprovedLoyaltyRedemption actually burns the points once a
// LoyaltyRedemptionRequest is Approved - called from DecideApproval, the
// same hook shape as postApprovedJournalVoucher. Logged loudly on failure
// (LogSystemError) for the same reason: an Approved-but-never-redeemed
// request is a real gap, not a best-effort nicety.
func executeApprovedLoyaltyRedemption(tenantID, requestID string, data map[string]interface{}) {
	customerID, _ := data["customer_id"].(string)
	referenceID, _ := data["reference_id"].(string)
	points := int(numFromInterface(data["points"]))

	if _, err := RedeemLoyaltyPoints(tenantID, customerID, points, referenceID); err != nil {
		LogSystemError(tenantID, "", "ERROR", "executeApprovedLoyaltyRedemption", fmt.Sprintf("request %s approved but redemption failed: %v", requestID, err), "")
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "executeApprovedLoyaltyRedemption", fmt.Sprintf("request %s redeemed but status update failed: %v", requestID, err), "")
		return
	}
	data["status"] = "Redeemed"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Redeemed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'LoyaltyRedemptionRequest' AND id = $2`, schema),
		marshaled, requestID)
}
