package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"time"
)

// CRM/Loyalty (Stage 13.13d, scoped MVP per the CRM/Loyalty add-on
// blueprint Sec.3.4/3.5): the Loyalty Point Ledger and earn/burn only.
// Customer 360, Segmentation, Campaign Management, Voucher/Coupon Engine,
// Customer Service, and Consent & Privacy are explicitly out of scope.

// redemptionValuePerPointFor is the burn rate (default 1 point = Rs.1 on
// redemption). Stage 30.7 made it the last of the three loyalty rates to
// become tenant-configurable - the earn rate (loyalty.rupees_per_point) and
// point expiry (loyalty.point_expiry_days) already were. All three are read
// at their use sites on each call, so an admin edit applies immediately.
func redemptionValuePerPointFor(tenantID string) int {
	return GetSettingInt(tenantID, "loyalty.redemption_value_per_point")
}

// LoyaltyLedgerEntry is one earn/burn transaction.
type LoyaltyLedgerEntry struct {
	TransactionType string    `json:"transaction_type"`
	Points          int       `json:"points"`
	ReferenceID     string    `json:"reference_id"`
	CreatedAt       time.Time `json:"created_at"`
	// Stage 55.17: what kind of document it came from, and when it lapses.
	ReferenceDoctype string     `json:"reference_doctype,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

// insertLoyaltyLedgerEntryInSchema is the schema-level primitive - needed
// directly by StartLoyaltyExpiryWorker (engines/loyalty_tiering.go), which
// already has the schema from its own tenant-schema scan and no tenantID to
// re-derive it from, the same split CreateJournalVoucher/
// createJournalVoucherInSchema uses. expiresAt is nil for Burn rows (and
// for the expiry sweep's own Burn rows) - only an Earn row's points lapse.
func insertLoyaltyLedgerEntryInSchema(schema, customerID, transactionType string, points int, referenceDoctype, referenceID string, expiresAt *time.Time) error {
	_, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, schema), customerID, transactionType, points, referenceDoctype, referenceID, expiresAt)
	return err
}

func insertLoyaltyLedgerEntry(tenantID, customerID, transactionType string, points int, referenceDoctype, referenceID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	return insertLoyaltyLedgerEntryInSchema(schema, customerID, transactionType, points, referenceDoctype, referenceID, nil)
}

// RedeemLoyaltyPointsTx is RedeemLoyaltyPoints inside the caller's own
// transaction (Stage 47.3.2), so the burn commits with the sale or not at all.
//
// This removes the compensating-reversal dance FinalizePOSCheckout used to
// need: before this, the burn committed on its own, and every later failure
// path had to remember to call ReverseLoyaltyRedemption - a correctness
// obligation on five separate error branches, each of which could itself fail
// and only log. Now a rolled-back sale un-burns the points by definition.
// ReverseLoyaltyRedemption stays for the case it was really written for: a
// completed sale that is later returned.
//
// The balance check reads inside the transaction under a per-customer lock, so
// two tills redeeming the same balance concurrently cannot both pass it.
//
// Stage 73.1.1: the lock is a transaction advisory lock, not FOR UPDATE on the
// balance query. Postgres refuses FOR UPDATE on an aggregate (0A000), so every
// sale that redeemed points failed at checkout; and locking the existing rows
// would not have stopped a concurrent till inserting a new Burn anyway. The
// balance is read by a statement that starts after the lock is held, so under
// READ COMMITTED it sees the other till's committed burn.
//
// Stage 73.1.4: this is the ONE place points are burned - the till's sale, the
// /loyalty/redeem API, the OTP flow and an approved redemption request all
// come through it - so the redemption controls (daily limit, one-time code,
// manager approval above a value) are checked here, under the same lock, and
// no path can skip them. auth names what authorised this redemption.
func RedeemLoyaltyPointsTx(tx *sql.Tx, schema, tenantID, customerID string, points int, referenceID string, auth LoyaltyRedemptionAuth) (discountValue int, err error) {
	if points <= 0 {
		return 0, nil
	}
	balance, err := lockedLoyaltyBalanceTx(tx, schema, customerID)
	if err != nil {
		return 0, err
	}
	if points > balance {
		return 0, &ValidationError{Code: "CUSTOM-0134", Message: fmt.Sprintf("insufficient loyalty points: requested %d, balance %d", points, balance)}
	}
	if err := authorizeLoyaltyRedemptionTx(tx, schema, tenantID, customerID, points, referenceID, auth); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id)
		VALUES ($1, 'Burn', $2, 'POSCart', $3)`, schema), customerID, points, referenceID); err != nil {
		return 0, err
	}
	return points * redemptionValuePerPointFor(tenantID), nil
}

// lockedLoyaltyBalanceTx takes the customer's loyalty lock for the rest of the
// transaction and returns their balance as it stands once the lock is held.
// Every writer that must not race a redemption (the till's burn, a return's
// reversal or restore) goes through it.
func lockedLoyaltyBalanceTx(tx *sql.Tx, schema, customerID string) (int, error) {
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "loyalty:"+schema+":"+customerID); err != nil {
		return 0, err
	}
	var balance int
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END), 0)
		FROM %s.loyalty_point_ledger WHERE customer_id = $1 AND `+crmUsablePointsSQL, schema), customerID).Scan(&balance)
	return balance, err
}

// GetLoyaltyBalance computes a customer's current point balance as
// SUM(Earn) - SUM(Burn) from the ledger - never a stored/editable field,
// per the blueprint's explicit "Never Do This: directly edit point balance"
// rule.
func GetLoyaltyBalance(tenantID, customerID string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	// Stage 101.11.1: points still waiting for their bill's return window do
	// not count yet.
	var balance int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END), 0)
		FROM %s.loyalty_point_ledger WHERE customer_id = $1 AND `+crmUsablePointsSQL, schema), customerID).Scan(&balance)
	return balance, err
}

// 134.6 (47.10.1): most ledger rows the customer point history returns (newest first; the balance is a separate SQL SUM).
const loyaltyLedgerRowCap = 1000

// GetLoyaltyLedger lists a customer's point transaction history, most
// recent first.
func GetLoyaltyLedger(tenantID, customerID string) ([]LoyaltyLedgerEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT transaction_type, points, COALESCE(reference_id, ''), created_at, COALESCE(reference_doctype, ''), expires_at
		FROM %s.loyalty_point_ledger WHERE customer_id = $1 ORDER BY created_at DESC, id DESC LIMIT %d`, schema, loyaltyLedgerRowCap), customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []LoyaltyLedgerEntry
	for rows.Next() {
		var e LoyaltyLedgerEntry
		if err := rows.Scan(&e.TransactionType, &e.Points, &e.ReferenceID, &e.CreatedAt, &e.ReferenceDoctype, &e.ExpiresAt); err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, nil
}

// RedeemLoyaltyPoints burns points against a customer's balance (checking
// funds first - no negative balance is ever allowed, per the blueprint's
// "no negative balance" POS redemption control) and returns the rupee
// discount value it's worth, for the caller (checkout) to apply.
//
// CUSTOM-0134 (Stage 25.5, "Loyalty points insufficient") is the balance
// refusal, a 422 at its call site (handleRedeemLoyaltyPoints). Stage 73.1.4: it
// is now a thin transaction around RedeemLoyaltyPointsTx, so this API burn is
// held to exactly the controls the till's is - with no authorisation, a
// redemption that needs a one-time code or a manager's approval is refused.
func RedeemLoyaltyPoints(tenantID, customerID string, points int, referenceID string) (discountValue int, err error) {
	return redeemLoyaltyPointsAuthorized(tenantID, customerID, points, referenceID, LoyaltyRedemptionAuth{})
}

// redeemLoyaltyPointsAuthorized burns points in a transaction of its own.
func redeemLoyaltyPointsAuthorized(tenantID, customerID string, points int, referenceID string, auth LoyaltyRedemptionAuth) (int, error) {
	if points <= 0 {
		return 0, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	value, err := RedeemLoyaltyPointsTx(tx, schema, tenantID, customerID, points, referenceID, auth)
	if err != nil {
		return 0, err
	}
	return value, tx.Commit()
}

// LoyaltyRedemptionValue is what `points` are worth in rupees, without
// touching the ledger - so a caller can show the customer the discount and
// check it against the balance before anything is burned. Stage 30.2.5's POS
// flow uses this at "Redeem Points" time and only burns at checkout.
func LoyaltyRedemptionValue(tenantID string, points int) int {
	if points <= 0 {
		return 0
	}
	return points * redemptionValuePerPointFor(tenantID)
}

// ReverseLoyaltyRedemption gives back points burned for a sale that then
// failed to complete (Stage 30.2.5). The ledger is append-only by design -
// the blueprint's "never edit a point balance" rule - so the reversal is a
// compensating Earn row referencing the same cart, which restores the balance
// while leaving the original Burn visible in the customer's history.
//
// The restored points deliberately carry no expiry: they are a correction of
// something that never happened, not a fresh accrual, so re-dating their
// lifetime would quietly shorten or extend it.
func ReverseLoyaltyRedemption(tenantID, customerID string, points int, referenceID string) error {
	if points <= 0 || customerID == "" {
		return nil
	}
	return insertLoyaltyLedgerEntry(tenantID, customerID, "Earn", points, "POSCart", referenceID+":REDEMPTION-REVERSAL")
}

// EarnLoyaltyPoints credits points for a completed sale. netSaleAmount
// should already exclude any redemption discount applied to the same
// checkout (points aren't earned on the portion paid for with points).
// Stage 26.7.3: the base points are scaled by the customer's current tier's
// earn_multiplier (engines/loyalty_tiering.go), the earned lot is stamped
// with an expiry (loyaltyPointExpiryDays out), and the customer's tier is
// re-evaluated against their new lifetime spend - both best-effort
// enhancements on top of the points already earned/inserted, so a failure
// in either never undoes or blocks the earn itself.
func EarnLoyaltyPoints(tenantID, customerID string, netSaleAmount int, referenceID string) error {
	// Stage 85.C3: a store's walk-in customer stands for "nobody" - it
	// never collects points.
	if IsWalkInCustomer(tenantID, customerID) {
		return nil
	}
	rupeesPerPoint := GetSettingInt(tenantID, "loyalty.rupees_per_point")
	if rupeesPerPoint <= 0 {
		rupeesPerPoint = 100 // defensive: the setting is min-1 validated, never 0
	}
	basePoints := netSaleAmount / rupeesPerPoint
	if basePoints <= 0 {
		return nil
	}
	points := int(float64(basePoints) * loyaltyEarnMultiplierForCustomer(tenantID, customerID))
	if points <= 0 {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	expiresAt := time.Now().AddDate(0, 0, GetSettingInt(tenantID, "loyalty.point_expiry_days"))
	if err := insertLoyaltyLedgerEntryInSchema(schema, customerID, "Earn", points, "POSCart", referenceID, &expiresAt); err != nil {
		return err
	}
	// Stage 101.11.1 (D11): with pending points on, they wait out the bill's
	// return window before they can be used.
	if until := crmPendingUntil(tenantID); until != nil {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.loyalty_point_ledger SET pending_until = $3 WHERE id = (SELECT MAX(id) FROM %s.loyalty_point_ledger
			WHERE customer_id = $1 AND transaction_type = 'Earn' AND reference_doctype = 'POSCart' AND reference_id = $2)`, schema, schema), customerID, referenceID, *until); err != nil {
			LogSystemError(tenantID, "", "ERROR", "EarnLoyaltyPoints", fmt.Sprintf("points on %s could not be marked pending: %v", referenceID, err), "")
		}
	}
	// Stage 73.2.12: when accruing, the earned points' value is a liability.
	if err := postLoyaltyValue(tenantID, "POSCart", referenceID, "POSCart:"+referenceID+":LOYALTY_ACCRUE",
		int64(points)*loyaltyPointValuePaise(tenantID), true, ""); err != nil {
		LogSystemError(tenantID, "", "ERROR", "EarnLoyaltyPoints", fmt.Sprintf("points earned on %s but their liability was not booked: %v", referenceID, err), "")
	}
	if GetSettingBool(tenantID, "loyalty.recompute_tier_on_earn") {
		if _, err := RecomputeLoyaltyTier(tenantID, customerID); err != nil {
			LogSystemError(tenantID, "", "ERROR", "EarnLoyaltyPoints", fmt.Sprintf("tier recompute failed for customer %s: %v", customerID, err), "")
		}
	}
	return nil
}
