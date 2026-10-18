package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
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

// The redemption velocity cap and OTP validity window are tenant-configurable
// via the Stage 28 settings registry (security.loyalty_max_redemptions_per_day
// / security.loyalty_otp_validity_minutes, see settings_definitions.go), read
// at their use sites below. Both are lightweight velocity/fraud guards, not a
// rules engine or ML model; LoyaltyExpiry's own automatic Burn rows never count
// against the daily cap, since those aren't customer-initiated.

func checkLoyaltyRedemptionVelocity(tenantID, customerID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	return loyaltyVelocityCheck(db.DB, tenantID, schema, customerID)
}

// loyaltyVelocityCheck refuses a redemption once the customer has redeemed
// the daily limit's worth of times today. Expiry burns are not redemptions.
func loyaltyVelocityCheck(q interface {
	QueryRow(string, ...interface{}) *sql.Row
}, tenantID, schema, customerID string) error {
	var count int
	if err := q.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.loyalty_point_ledger
		WHERE customer_id = $1 AND transaction_type = 'Burn' AND reference_doctype != 'LoyaltyExpiry'
		  AND created_at::date = CURRENT_DATE`, schema), customerID).Scan(&count); err != nil {
		return err
	}
	maxPerDay := GetSettingInt(tenantID, "security.loyalty_max_redemptions_per_day")
	if count >= maxPerDay {
		return &ValidationError{Code: "CUSTOM-0601", Message: fmt.Sprintf("customer %s has already redeemed points %d times today - the daily limit is %d", customerID, count, maxPerDay)}
	}
	return nil
}

// --- Stage 73.1.4: one set of controls for every redemption ------------------

// LoyaltyRedemptionAuth names what authorised a redemption: a verified
// one-time code the customer received, and - when the value needs one - a
// manager's approved LoyaltyRedemptionRequest. Both are bound to the customer,
// the points and the reference (the till's cart number), and each is spent by
// the one redemption it authorised.
type LoyaltyRedemptionAuth struct {
	ChallengeID string `json:"challenge_id,omitempty"`
	ApprovalID  string `json:"approval_id,omitempty"`
}

// RedemptionRequirement is what a redemption of Points needs before it may
// burn, by the tenant's settings and approval rules.
type RedemptionRequirement struct {
	Points        int    `json:"points"`
	ValuePerPoint int    `json:"value_per_point"`
	Value         int    `json:"value"`
	NeedsOTP      bool   `json:"needs_otp"`
	NeedsApproval bool   `json:"needs_approval"`
	ApproverRole  string `json:"approver_role,omitempty"`
}

// LoyaltyRedemptionRequirementFor reads the controls for points: a one-time
// code when loyalty.redemption_requires_otp is on, and a manager's approval
// when the value reaches a LoyaltyRedemptionRequest approval rule (the same
// slab table every approval-gated document uses; 500 and over ships as Store
// Manager).
func LoyaltyRedemptionRequirementFor(tenantID string, points int) (RedemptionRequirement, error) {
	v := redemptionValuePerPointFor(tenantID)
	if v <= 0 {
		v = 1
	}
	req := RedemptionRequirement{Points: points, ValuePerPoint: v, Value: points * v,
		NeedsOTP: GetSettingBool(tenantID, "loyalty.redemption_requires_otp")}
	if points <= 0 {
		req.NeedsOTP = false
		return req, nil
	}
	role, err := RequiredApproverRoleForAmount(tenantID, "LoyaltyRedemptionRequest", float64(req.Value))
	if err != nil {
		return req, err
	}
	req.NeedsApproval, req.ApproverRole = role != "", role
	return req, nil
}

// authorizeLoyaltyRedemptionTx enforces the controls at the moment of the
// burn, inside its transaction and under the customer's loyalty lock: the
// daily limit, then the one-time code and the approval the value calls for,
// each spent here so it can never authorise a second redemption.
func authorizeLoyaltyRedemptionTx(tx *sql.Tx, schema, tenantID, customerID string, points int, referenceID string, auth LoyaltyRedemptionAuth) error {
	if err := loyaltyVelocityCheck(tx, tenantID, schema, customerID); err != nil {
		return err
	}
	req, err := LoyaltyRedemptionRequirementFor(tenantID, points)
	if err != nil {
		return err
	}
	if req.NeedsOTP {
		var spent string
		err := tx.QueryRow(fmt.Sprintf(`
			UPDATE %s.loyalty_redemption_otp_challenges SET consumed_at = CURRENT_TIMESTAMP
			WHERE id = $1 AND customer_id = $2 AND points = $3 AND COALESCE(reference_id, '') = $4
			  AND verified AND consumed_at IS NULL
			RETURNING id`, schema), auth.ChallengeID, customerID, points, referenceID).Scan(&spent)
		if err == sql.ErrNoRows {
			return &ValidationError{Code: "CUSTOM-0602", Message: fmt.Sprintf(
				"redeeming %d point(s) needs the one-time code sent to the customer - send it, and enter the code they read back, before completing the sale", points)}
		}
		if err != nil {
			return err
		}
	}
	if req.NeedsApproval {
		var dataStr, status string
		err := tx.QueryRow(fmt.Sprintf(`
			SELECT data, status FROM %s.documents WHERE doctype = 'LoyaltyRedemptionRequest' AND id = $1 FOR UPDATE`, schema),
			auth.ApprovalID).Scan(&dataStr, &status)
		var data map[string]interface{}
		if err == nil {
			_ = json.Unmarshal([]byte(dataStr), &data)
		}
		if err == sql.ErrNoRows || status != "Approved" || strField(data, "customer_id") != customerID ||
			int(numFromInterface(data["points"])) != points || strField(data, "reference_id") != referenceID {
			return &ValidationError{Code: "CUSTOM-0603", Message: fmt.Sprintf(
				"redeeming %d point(s) (worth %d) needs approval from a %s - raise the request and have it approved before completing the sale", points, req.Value, req.ApproverRole)}
		}
		if err != nil {
			return err
		}
		data["status"] = "Redeemed"
		marshaled, _ := json.Marshal(data)
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = $1, status = 'Redeemed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'LoyaltyRedemptionRequest' AND id = $2`, schema),
			marshaled, auth.ApprovalID); err != nil {
			return err
		}
	}
	return nil
}

// TillRedemption is where a till redemption stands: what it needs, and the
// code sent / approval raised for it so far.
type TillRedemption struct {
	RedemptionRequirement
	ChallengeID    string `json:"challenge_id,omitempty"`
	ApprovalID     string `json:"approval_id,omitempty"`
	ApprovalStatus string `json:"approval_status,omitempty"`
}

// StartTillLoyaltyRedemption sets up what a till redemption needs, before the
// sale: it sends the customer a one-time code when the tenant requires one,
// and raises a LoyaltyRedemptionRequest for a manager at the store when the
// value needs approval (with the cashier's reason). Nothing is burned - the
// sale burns the points, through RedeemLoyaltyPointsTx, once it completes.
// Asking again for the same cart, customer and points returns the request
// already raised rather than a second one.
func StartTillLoyaltyRedemption(tenantID, customerID string, points int, cartNumber, location, reasonCode, cashier, cashierRole string) (*TillRedemption, error) {
	if customerID == "" || points <= 0 || cartNumber == "" {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: "a customer, the points and the cart number are required"}
	}
	if err := checkLoyaltyRedemptionVelocity(tenantID, customerID); err != nil {
		return nil, err
	}
	balance, err := GetLoyaltyBalance(tenantID, customerID)
	if err != nil {
		return nil, err
	}
	if points > balance {
		return nil, &ValidationError{Code: "CUSTOM-0134", Message: fmt.Sprintf("insufficient loyalty points: requested %d, balance %d", points, balance)}
	}
	req, err := LoyaltyRedemptionRequirementFor(tenantID, points)
	if err != nil {
		return nil, err
	}
	out := &TillRedemption{RedemptionRequirement: req}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if req.NeedsApproval {
		var existingID, existingStatus string
		errEx := db.DB.QueryRow(fmt.Sprintf(`
			SELECT id, status FROM %s.documents
			WHERE doctype = 'LoyaltyRedemptionRequest' AND data->>'channel' = 'POS' AND data->>'reference_id' = $1
			  AND data->>'customer_id' = $2 AND (data->>'points')::int = $3 AND status IN ('Pending Approval', 'Approved')
			ORDER BY created_at DESC LIMIT 1`, schema), cartNumber, customerID, points).Scan(&existingID, &existingStatus)
		if errEx == nil {
			out.ApprovalID, out.ApprovalStatus = existingID, existingStatus
		} else {
			if err := requireActiveReasonCode(tenantID, reasonCode, "Loyalty Redemption"); err != nil {
				return nil, err
			}
			out.ApprovalID = NewDocID("LRR")
			data := map[string]interface{}{
				"id": out.ApprovalID, "code": out.ApprovalID, "customer_id": customerID, "points": points,
				"points_value": req.Value, "reference_id": cartNumber, "channel": "POS", "location": location,
				"reason_code": reasonCode, "status": "Draft",
			}
			marshaled, _ := json.Marshal(data)
			if _, err := db.DB.Exec(fmt.Sprintf(
				`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'LoyaltyRedemptionRequest', $2, 'Draft', $3)`, schema),
				out.ApprovalID, marshaled, cashier); err != nil {
				return nil, err
			}
			if err := SubmitForApproval(tenantID, "LoyaltyRedemptionRequest", out.ApprovalID, cashier, cashierRole); err != nil {
				return nil, err
			}
			out.ApprovalStatus = "Pending Approval"
		}
	}
	if req.NeedsOTP {
		out.ChallengeID, err = InitiateSecureLoyaltyRedemption(tenantID, customerID, points, cartNumber, cashier)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// VerifyTillLoyaltyOTP checks the code the customer read back. It only marks
// the code verified - the sale it was sent for spends it.
func VerifyTillLoyaltyOTP(tenantID, challengeID, otpCode string) error {
	_, _, _, _, err := verifyLoyaltyOTPChallenge(tenantID, challengeID, otpCode)
	return err
}

// TillRedemptionApprovalStatus is where a till's approval request stands.
func TillRedemptionApprovalStatus(tenantID, approvalID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var status string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'LoyaltyRedemptionRequest' AND id = $1`, schema), approvalID).Scan(&status)
	if err == sql.ErrNoRows {
		return "", &ValidationError{Code: "CUSTOM-0603", Message: fmt.Sprintf("no redemption request %s", approvalID)}
	}
	return status, err
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
	challengeID = NewDocID("LROTP")
	expiresAt := time.Now().Add(time.Duration(GetSettingInt(tenantID, "security.loyalty_otp_validity_minutes")) * time.Minute)
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
	customerID, points, referenceID, initiatedBy, err := verifyLoyaltyOTPChallenge(tenantID, challengeID, otpCode)
	if err != nil {
		return "", 0, "", err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, "", err
	}

	value := points * redemptionValuePerPointFor(tenantID)
	role, err := RequiredApproverRoleForAmount(tenantID, "LoyaltyRedemptionRequest", float64(value))
	if err != nil {
		return "", 0, "", err
	}
	if role == "" {
		// 73.1.4: through the one choke point, with the code just verified as
		// its authorisation - so the same daily limit applies here too.
		discountValue, err = redeemLoyaltyPointsAuthorized(tenantID, customerID, points, referenceID, LoyaltyRedemptionAuth{ChallengeID: challengeID})
		if err != nil {
			return "", 0, "", err
		}
		return "Redeemed", discountValue, "", nil
	}

	requestID = NewDocID("LRR")
	data := map[string]interface{}{
		"id": requestID, "code": requestID, "customer_id": customerID, "points": points,
		"points_value": value, "reference_id": referenceID, "status": "Draft",
		"channel": "API", "challenge_id": challengeID,
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

// verifyLoyaltyOTPChallenge checks a one-time code against its stored hash
// and marks the challenge verified. It burns nothing.
func verifyLoyaltyOTPChallenge(tenantID, challengeID, otpCode string) (customerID string, points int, referenceID, initiatedBy string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, "", "", err
	}

	var otpHash string
	var expired, verified bool
	// expired is computed in SQL (expires_at < NOW()) rather than scanned and
	// compared in Go - the same fix handlers_auth.go's login lockout check
	// already applies for the identical reason: expires_at is a tz-naive
	// `timestamp` column, this app server's local clock is IST (UTC+5:30),
	// and comparing time.Now() (local) against the scanned value directly
	// skews by ~5.5 hours. The existing test for this function already
	// worked around the skew with a 25-hour expiry margin instead of fixing
	// it (see loyalty_redemption_security_test.go's own comment) - BLD-020's
	// timezone-boundary pass found the root cause was never actually closed.
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT customer_id, points, COALESCE(reference_id, ''), initiated_by, otp_hash, (expires_at < NOW()), verified
		FROM %s.loyalty_redemption_otp_challenges WHERE id = $1`, schema), challengeID).
		Scan(&customerID, &points, &referenceID, &initiatedBy, &otpHash, &expired, &verified); err != nil {
		return "", 0, "", "", fmt.Errorf("redemption challenge not found: %v", err)
	}
	if verified {
		return "", 0, "", "", fmt.Errorf("this OTP challenge has already been used")
	}
	if expired {
		return "", 0, "", "", fmt.Errorf("OTP has expired - initiate a new redemption")
	}
	if hashOTP(otpCode) != otpHash {
		return "", 0, "", "", fmt.Errorf("incorrect OTP")
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.loyalty_redemption_otp_challenges SET verified = TRUE WHERE id = $1`, schema), challengeID); err != nil {
		return "", 0, "", "", err
	}
	return customerID, points, referenceID, initiatedBy, nil
}

// executeApprovedLoyaltyRedemption actually burns the points once a
// LoyaltyRedemptionRequest is Approved - called from DecideApproval, the
// same hook shape as postApprovedJournalVoucher. Logged loudly on failure
// (LogSystemError) for the same reason: an Approved-but-never-redeemed
// request is a real gap, not a best-effort nicety.
//
// Stage 73.1.4: a request raised from the till (channel POS) is an
// authorisation for that sale, not a burn - the sale burns it when it
// completes, so the points can never go both on approval and at the till.
func executeApprovedLoyaltyRedemption(tenantID, requestID string, data map[string]interface{}) {
	if strField(data, "channel") == "POS" {
		return
	}
	customerID, _ := data["customer_id"].(string)
	referenceID, _ := data["reference_id"].(string)
	points := int(numFromInterface(data["points"]))

	auth := LoyaltyRedemptionAuth{ChallengeID: strField(data, "challenge_id"), ApprovalID: requestID}
	if _, err := redeemLoyaltyPointsAuthorized(tenantID, customerID, points, referenceID, auth); err != nil {
		LogSystemError(tenantID, "", "ERROR", "executeApprovedLoyaltyRedemption", fmt.Sprintf("request %s approved but redemption failed: %v", requestID, err), "")
	}
}
